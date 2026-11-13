package viewer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Rasterizer turns a page or image URL into a PNG with a headless
// Chrome or Chromium. It is what lets Mermaid diagrams, HTML pages and
// SVGs reach a terminal that can only show raster images.
type Rasterizer struct {
	chrome     string
	run        runner
	profileDir string
	timeout    time.Duration
}

// rasterTimeout bounds one screenshot. Chrome needs ~2 s for a Mermaid
// page; a stuck renderer must not pin the picker's goroutine forever.
const rasterTimeout = 30 * time.Second

// ErrNoRasterizer reports that no browser binary was found.
var ErrNoRasterizer = errors.New("viewer: no Chrome or Chromium found to rasterize this artifact (set [viewer] chrome_path)")

// chromeBundles are checked first, in order, on macOS.
var chromeBundles = []string{
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
}

// chromeNames are looked up on PATH when no bundle matched.
var chromeNames = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"}

// FindChrome returns the first browser binary found, or "".
func FindChrome(lookPath func(string) (string, error), exists func(string) bool) string {
	for _, p := range chromeBundles {
		if exists(p) {
			return p
		}
	}
	for _, name := range chromeNames {
		if p, err := lookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// NewRasterizer picks the browser binary (chromePath wins over
// auto-detection) and keeps its throwaway profile under cacheRoot so it
// never collides with the user's running browser.
func NewRasterizer(chromePath, cacheRoot string) *Rasterizer {
	if chromePath == "" {
		chromePath = FindChrome(exec.LookPath, fileExists)
	}
	return &Rasterizer{
		chrome:     chromePath,
		run:        execRunner{},
		profileDir: filepath.Join(cacheRoot, "chrome-profile"),
		timeout:    rasterTimeout,
	}
}

// Available reports whether a browser binary was found.
func (r *Rasterizer) Available() bool { return r != nil && r.chrome != "" }

// screenshotArgs builds the headless invocation. --virtual-time-budget
// lets the page finish async work (mermaid rendering) before capture.
func (r *Rasterizer) screenshotArgs(url, out string, width, height int) []string {
	return []string{
		"--headless=new",
		"--disable-gpu",
		"--hide-scrollbars",
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=" + r.profileDir,
		"--virtual-time-budget=8000",
		fmt.Sprintf("--window-size=%d,%d", width, height),
		"--screenshot=" + out,
		url,
	}
}

// Screenshot renders url into the PNG at out.
func (r *Rasterizer) Screenshot(ctx context.Context, url, out string, width, height int) error {
	if !r.Available() {
		return ErrNoRasterizer
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.run.Run(ctx, r.chrome, r.screenshotArgs(url, out, width, height)...); err != nil {
		return fmt.Errorf("viewer: rasterize: %w", err)
	}
	if !fileExists(out) {
		return errors.New("viewer: rasterizer exited without writing an image")
	}
	_ = os.Chmod(out, 0o600)
	return nil
}
