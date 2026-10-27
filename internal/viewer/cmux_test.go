package viewer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// fakeRunner records every command and answers from a table keyed by
// the cmux subcommand (the first argument).
type fakeRunner struct {
	calls   [][]string
	results map[string]fakeResult
}

type fakeResult struct {
	out string
	err error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if len(args) == 0 {
		return "", nil
	}
	r := f.results[args[0]]
	return r.out, r.err
}

const testURL = "file:///tmp/mosaic/artifacts/01H/abc.html"

func TestCmuxNewPaneArgs(t *testing.T) {
	dock := []string{"new-pane", "--type", "browser", "--url", testURL, "--focus", "false", "--placement", "dock"}
	cases := []struct {
		name string
		opts Options
		want []string
	}{
		{"dock by default", Options{}, dock},
		{"dock explicit", Options{Placement: "dock"}, dock},
		{"unknown placement falls back to dock", Options{Placement: "left"}, dock},
		{"right splits the workspace", Options{Placement: "right"},
			[]string{"new-pane", "--type", "browser", "--url", testURL, "--focus", "false", "--placement", "workspace", "--direction", "right"}},
		{"down splits the workspace", Options{Placement: "down"},
			[]string{"new-pane", "--type", "browser", "--url", testURL, "--focus", "false", "--placement", "workspace", "--direction", "down"}},
		{"focus true", Options{Focus: true},
			[]string{"new-pane", "--type", "browser", "--url", testURL, "--focus", "true", "--placement", "dock"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newCmux(&fakeRunner{}, tc.opts).newPaneArgs(testURL)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("newPaneArgs = %q\nwant        %q", got, tc.want)
			}
		})
	}
}

func TestCmuxOpenCreatesPaneThenReusesSurface(t *testing.T) {
	run := &fakeRunner{results: map[string]fakeResult{
		"new-pane": {out: "surface:4\n"},
	}}
	c := newCmux(run, Options{})
	ctx := context.Background()

	if err := c.Open(ctx, testURL); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if c.surface != "surface:4" {
		t.Fatalf("surface after first Open = %q, want surface:4", c.surface)
	}
	second := "file:///tmp/mosaic/artifacts/01H/def.html"
	if err := c.Open(ctx, second); err != nil {
		t.Fatalf("second Open: %v", err)
	}

	if len(run.calls) != 2 {
		t.Fatalf("calls = %d, want 2: %q", len(run.calls), run.calls)
	}
	for _, call := range run.calls {
		if call[0] != "cmux" {
			t.Errorf("command = %q, want cmux", call[0])
		}
	}
	wantFirst := append([]string{"cmux"}, c.newPaneArgs(testURL)...)
	if !reflect.DeepEqual(run.calls[0], wantFirst) {
		t.Errorf("first call = %q\nwant       %q", run.calls[0], wantFirst)
	}
	wantSecond := []string{"cmux", "browser", "--surface", "surface:4", "goto", second}
	if !reflect.DeepEqual(run.calls[1], wantSecond) {
		t.Errorf("second call = %q\nwant        %q", run.calls[1], wantSecond)
	}
}

func TestCmuxOpenFallsBackToNewPaneWhenGotoFails(t *testing.T) {
	run := &fakeRunner{results: map[string]fakeResult{
		"browser":  {err: errors.New("surface not found")},
		"new-pane": {out: "pane:2 surface:7"},
	}}
	c := newCmux(run, Options{})
	c.surface = "surface:4"

	if err := c.Open(context.Background(), testURL); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(run.calls) != 2 || run.calls[0][1] != "browser" || run.calls[1][1] != "new-pane" {
		t.Fatalf("calls = %q, want goto then new-pane", run.calls)
	}
	if c.surface != "surface:7" {
		t.Errorf("surface = %q, want the fresh pane's surface:7", c.surface)
	}
}

func TestCmuxOpenWithoutSurfaceRefOpensNewPaneEachTime(t *testing.T) {
	run := &fakeRunner{results: map[string]fakeResult{"new-pane": {out: "ok\n"}}}
	c := newCmux(run, Options{})
	for i := 0; i < 2; i++ {
		if err := c.Open(context.Background(), testURL); err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
	}
	if len(run.calls) != 2 || run.calls[0][1] != "new-pane" || run.calls[1][1] != "new-pane" {
		t.Fatalf("calls = %q, want two new-pane calls", run.calls)
	}
	if c.surface != "" {
		t.Errorf("surface = %q, want empty when cmux prints no ref", c.surface)
	}
}

func TestCmuxOpenReportsNewPaneFailure(t *testing.T) {
	boom := errors.New("socket closed")
	run := &fakeRunner{results: map[string]fakeResult{"new-pane": {err: boom}}}
	c := newCmux(run, Options{})
	err := c.Open(context.Background(), testURL)
	if !errors.Is(err, boom) {
		t.Fatalf("Open error = %v, want to wrap %v", err, boom)
	}
	if c.surface != "" {
		t.Errorf("surface = %q, want empty after a failed new-pane", c.surface)
	}
}

func TestParseSurfaceRef(t *testing.T) {
	cases := []struct{ in, want string }{
		{"surface:4\n", "surface:4"},
		{"pane:2 surface:12", "surface:12"},
		{"workspace:1/pane:3/surface:9", "surface:9"},
		{"ok", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := parseSurfaceRef(tc.in); got != tc.want {
			t.Errorf("parseSurfaceRef(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewPicksSystemWithoutCmuxEnv(t *testing.T) {
	t.Setenv("CMUX_SOCKET_PATH", "")
	l := New(Options{})
	if l.Name() != "system" {
		t.Fatalf("New().Name() = %q, want system", l.Name())
	}
	if _, ok := l.(System); !ok {
		t.Fatalf("New() = %T, want System", l)
	}
}

func TestNewPicksCmuxInsideCmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cmux shim is a shell script")
	}
	dir := t.TempDir()
	shim := filepath.Join(dir, "cmux")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CMUX_SOCKET_PATH", filepath.Join(dir, "cmux.sock"))

	l := New(Options{Placement: "right", Focus: true})
	c, ok := l.(*Cmux)
	if !ok {
		t.Fatalf("New() = %T, want *Cmux", l)
	}
	if c.Name() != "cmux" || c.placement != "right" || !c.focus {
		t.Errorf("Cmux = {name %q placement %q focus %v}, want cmux/right/true", c.Name(), c.placement, c.focus)
	}
}

func TestNewPicksSystemWhenCmuxBinaryMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CMUX_SOCKET_PATH", "/tmp/cmux.sock")
	if l := New(Options{}); l.Name() != "system" {
		t.Fatalf("New().Name() = %q, want system when cmux is not on PATH", l.Name())
	}
}
