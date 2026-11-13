package viewer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scriptedRunner records invocations and optionally writes the
// screenshot file the rasterizer expects.
type scriptedRunner struct {
	calls     [][]string
	writeFile bool
	err       error
}

func (s *scriptedRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	s.calls = append(s.calls, append([]string{name}, args...))
	if s.err != nil {
		return "", s.err
	}
	if s.writeFile {
		for _, a := range args {
			if strings.HasPrefix(a, "--screenshot=") {
				_ = os.WriteFile(strings.TrimPrefix(a, "--screenshot="), tinyPNG(t0), 0o644)
			}
		}
	}
	return "", nil
}

func TestFindChrome(t *testing.T) {
	exists := func(p string) bool { return p == chromeBundles[1] }
	lookPath := func(string) (string, error) { return "", errors.New("nope") }
	if got := FindChrome(lookPath, exists); got != chromeBundles[1] {
		t.Fatalf("bundle lookup = %q", got)
	}
	none := func(string) bool { return false }
	onPath := func(n string) (string, error) {
		if n == "chromium" {
			return "/usr/bin/chromium", nil
		}
		return "", errors.New("nope")
	}
	if got := FindChrome(onPath, none); got != "/usr/bin/chromium" {
		t.Fatalf("PATH lookup = %q", got)
	}
	if got := FindChrome(lookPath, none); got != "" {
		t.Fatalf("expected no browser, got %q", got)
	}
}

func TestRasterizer_Screenshot(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "shot.png")
	run := &scriptedRunner{writeFile: true}
	r := &Rasterizer{chrome: "/fake/chrome", run: run, profileDir: filepath.Join(dir, "profile"), timeout: time.Second}
	if err := r.Screenshot(context.Background(), "file:///tmp/page.html", out, 1400, 900); err != nil {
		t.Fatal(err)
	}
	if len(run.calls) != 1 || run.calls[0][0] != "/fake/chrome" {
		t.Fatalf("calls = %v", run.calls)
	}
	args := run.calls[0][1:]
	for _, want := range []string{"--headless=new", "--screenshot=" + out, "--window-size=1400,900", "--user-data-dir=" + filepath.Join(dir, "profile")} {
		found := false
		for _, a := range args {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing arg %q in %v", want, args)
		}
	}
	if args[len(args)-1] != "file:///tmp/page.html" {
		t.Fatalf("url must be the last argument: %v", args)
	}
	info, err := os.Stat(out)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("screenshot should exist with 0600, got %v %v", info, err)
	}
}

func TestRasterizer_Failures(t *testing.T) {
	ctx := context.Background()
	var nilR *Rasterizer
	if err := nilR.Screenshot(ctx, "u", "o", 1, 1); !errors.Is(err, ErrNoRasterizer) {
		t.Fatalf("nil rasterizer: %v", err)
	}
	if err := (&Rasterizer{}).Screenshot(ctx, "u", "o", 1, 1); !errors.Is(err, ErrNoRasterizer) {
		t.Fatalf("empty chrome path: %v", err)
	}
	r := &Rasterizer{chrome: "/fake/chrome", run: &scriptedRunner{}, timeout: time.Second}
	if err := r.Screenshot(ctx, "u", filepath.Join(t.TempDir(), "o.png"), 1, 1); err == nil {
		t.Fatal("a run that writes no file must fail")
	}
	r = &Rasterizer{chrome: "/fake/chrome", run: &scriptedRunner{err: errors.New("boom")}, timeout: time.Second}
	if err := r.Screenshot(ctx, "u", "o", 1, 1); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("runner error must surface, got %v", err)
	}
}
