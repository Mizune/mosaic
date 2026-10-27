package viewer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetectInline(t *testing.T) {
	ctx := context.Background()
	tmuxOn := func(context.Context, string) (string, error) { return "on", nil }
	tmuxAll := func(context.Context, string) (string, error) { return "all", nil }
	tmuxOff := func(context.Context, string) (string, error) { return "off", nil }
	tmuxErr := func(context.Context, string) (string, error) { return "", errors.New("no server") }

	cases := []struct {
		name       string
		mode       string
		env        map[string]string
		tmux       func(context.Context, string) (string, error)
		want       bool
		wantTmux   bool
		wantReason string
	}{
		{"off", "off", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x"}, tmuxOn, false, false, "off"},
		{"auto plain terminal", "auto", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, tmuxOn, false, false, "Kitty graphics"},
		{"auto ghostty", "auto", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x"}, tmuxOn, true, false, ""},
		{"auto kitty", "auto", map[string]string{"KITTY_WINDOW_ID": "1"}, tmuxOn, true, false, ""},
		{"auto wezterm", "auto", map[string]string{"TERM_PROGRAM": "WezTerm"}, tmuxOn, true, false, ""},
		{"auto TERM", "auto", map[string]string{"TERM": "xterm-kitty"}, tmuxOn, true, false, ""},
		{"ghostty inside tmux on", "auto", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x", "TMUX": "/tmp/t,1,0"}, tmuxOn, true, true, ""},
		{"ghostty inside tmux all", "auto", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x", "TMUX": "/tmp/t,1,0"}, tmuxAll, true, true, ""},
		{"ghostty inside tmux off", "auto", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x", "TMUX": "/tmp/t,1,0"}, tmuxOff, false, true, "allow-passthrough"},
		{"ghostty inside tmux probe fails", "auto", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x", "TMUX": "/tmp/t,1,0"}, tmuxErr, false, true, "allow-passthrough"},
		{"on skips terminal check", "on", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, tmuxOn, true, false, ""},
		{"on still needs passthrough", "ON", map[string]string{"TMUX": "/tmp/t,1,0"}, tmuxOff, false, true, "allow-passthrough"},
		{"unknown mode behaves like auto", "", map[string]string{"TERM": "xterm-ghostty"}, tmuxOn, true, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectInline(ctx, tc.mode, Environment{Getenv: envOf(tc.env), TmuxOption: tc.tmux})
			if got.Supported != tc.want || got.InTmux != tc.wantTmux {
				t.Fatalf("got %+v, want supported=%v inTmux=%v", got, tc.want, tc.wantTmux)
			}
			if tc.wantReason != "" && !strings.Contains(got.Reason, tc.wantReason) {
				t.Fatalf("reason %q should mention %q", got.Reason, tc.wantReason)
			}
			if tc.want && got.Reason != "" {
				t.Fatalf("supported result must carry no reason, got %q", got.Reason)
			}
		})
	}
}

func TestDetectInline_NestedTmux(t *testing.T) {
	ctx := context.Background()
	on := func(context.Context, string) (string, error) { return "on", nil }
	base := map[string]string{"GHOSTTY_RESOURCES_DIR": "/x", "TMUX": "/tmp/tmux-501/mosaic,4242,3"}
	withOuter := func(outer string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		m["MOSAIC_OUTER_TMUX"] = outer
		return m
	}
	cases := []struct {
		name      string
		env       map[string]string
		at        func(context.Context, string, string) (string, error)
		want      bool
		wantDepth int
		reason    string
	}{
		{"single layer", base, nil, true, 1, ""},
		{"outer on", withOuter("/tmp/tmux-501/default,100,0"), func(_ context.Context, socket, name string) (string, error) {
			if socket != "/tmp/tmux-501/default" || name != "allow-passthrough" {
				return "", errors.New("wrong probe " + socket + " " + name)
			}
			return "on", nil
		}, true, 2, ""},
		{"outer all", withOuter("/tmp/tmux-501/default,100,0"), func(context.Context, string, string) (string, error) { return "all", nil }, true, 2, ""},
		{"outer off", withOuter("/tmp/tmux-501/default,100,0"), func(context.Context, string, string) (string, error) { return "off", nil }, false, 2, "/tmp/tmux-501/default"},
		{"outer probe fails", withOuter("/tmp/tmux-501/default,100,0"), func(context.Context, string, string) (string, error) { return "", errors.New("no server") }, false, 2, "allow-passthrough"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectInline(ctx, "auto", Environment{Getenv: envOf(tc.env), TmuxOption: on, TmuxOptionAt: tc.at})
			if got.Supported != tc.want || got.TmuxDepth != tc.wantDepth || !got.InTmux {
				t.Fatalf("got %+v, want supported=%v depth=%d", got, tc.want, tc.wantDepth)
			}
			if tc.reason != "" && !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("reason %q should mention %q", got.Reason, tc.reason)
			}
		})
	}
	// A stale MOSAIC_OUTER_TMUX outside any tmux is ignored.
	got := DetectInline(ctx, "auto", Environment{Getenv: envOf(map[string]string{"GHOSTTY_RESOURCES_DIR": "/x", "MOSAIC_OUTER_TMUX": "/tmp/x,1,0"}), TmuxOption: on})
	if !got.Supported || got.TmuxDepth != 0 {
		t.Fatalf("outside tmux the outer hint must be ignored: %+v", got)
	}
}
