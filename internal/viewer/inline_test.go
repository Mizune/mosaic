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
