package viewer

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Time{}

// tinyPNG returns a 3x2 PNG; the argument only keeps the helper
// signature stable for callers that pass a value.
func tinyPNG(_ time.Time) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestPrepareInline_PNGPassthrough(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(src, tinyPNG(t0), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{CacheRoot: filepath.Join(dir, "cache")}
	img, err := PrepareInline(context.Background(), Target{SessionID: "s1", Kind: KindImage, Path: src, Hash: "h1"}, nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 3 || img.Height != 2 || !bytes.Equal(img.PNG, tinyPNG(t0)) {
		t.Fatalf("png should be used as-is: %dx%d", img.Width, img.Height)
	}
	if _, err := os.Stat(filepath.Join(opts.CacheRoot, "s1", "h1.png")); err == nil {
		t.Fatal("a png file needs no cache copy")
	}
}

func TestPrepareInline_JPEGConvertsAndCaches(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.jpg")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 5, 4)), nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	opts := Options{CacheRoot: filepath.Join(dir, "cache")}
	img, err := PrepareInline(context.Background(), Target{SessionID: "s1", Kind: KindImage, Path: src, Hash: "j1"}, nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 5 || img.Height != 4 {
		t.Fatalf("size = %dx%d", img.Width, img.Height)
	}
	cached := filepath.Join(opts.CacheRoot, "s1", "j1.png")
	if _, err := os.Stat(cached); err != nil {
		t.Fatalf("converted image should be cached: %v", err)
	}
	// Second call is served from the cache even if the source vanished.
	os.Remove(src)
	if _, err := PrepareInline(context.Background(), Target{SessionID: "s1", Kind: KindImage, Path: src, Hash: "j1"}, nil, opts, nil); err != nil {
		t.Fatalf("cached read failed: %v", err)
	}
}

func TestPrepareInline_MissingAndUnsupported(t *testing.T) {
	opts := Options{CacheRoot: t.TempDir()}
	_, err := PrepareInline(context.Background(), Target{SessionID: "s1", Kind: KindImage, Path: "/nope/x.png", Hash: "m"}, nil, opts, nil)
	if !errors.Is(err, ErrArtifactMissing) {
		t.Fatalf("missing png: %v", err)
	}
	_, err = PrepareInline(context.Background(), Target{SessionID: "s1", Kind: KindMermaid, Content: "graph TD", Hash: "mm"}, nil, opts, nil)
	if !errors.Is(err, ErrNoRasterizer) {
		t.Fatalf("mermaid without a browser: %v", err)
	}
	_, err = PrepareInline(context.Background(), Target{SessionID: "../s1", Kind: KindMermaid, Content: "x", Hash: "mm"}, nil, opts, nil)
	if err == nil {
		t.Fatal("unsafe session id must be rejected")
	}
	_, err = PrepareInline(context.Background(), Target{SessionID: "s1", Kind: KindURL, Content: "javascript:alert(1)", Hash: "u"}, nil, opts, &Rasterizer{chrome: "/fake", run: &scriptedRunner{}})
	if !errors.Is(err, ErrUnsupportedURL) {
		t.Fatalf("javascript url: %v", err)
	}
}

func TestPrepareInline_MermaidRasterizesPage(t *testing.T) {
	dir := t.TempDir()
	opts := Options{CacheRoot: filepath.Join(dir, "cache"), MermaidJSURL: "https://example.invalid/mermaid.min.js"}
	run := &scriptedRunner{writeFile: true}
	r := &Rasterizer{chrome: "/fake/chrome", run: run, profileDir: filepath.Join(dir, "profile"), timeout: time.Second}
	target := Target{SessionID: "s1", Kind: KindMermaid, Title: "graph", Content: "graph TD\nA-->B", Hash: "g1"}

	img, err := PrepareInline(context.Background(), target, nil, opts, r)
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 3 || len(run.calls) != 1 {
		t.Fatalf("expected one rasterization, got %d calls, %dx%d", len(run.calls), img.Width, img.Height)
	}
	page := filepath.Join(opts.CacheRoot, "s1", "g1.html")
	if _, err := os.Stat(page); err != nil {
		t.Fatalf("mermaid page should be written first: %v", err)
	}
	url := run.calls[0][len(run.calls[0])-1]
	if url != FileURL(page) {
		t.Fatalf("rasterizer should load the generated page, got %q", url)
	}
	// Cached: no second browser run.
	if _, err := PrepareInline(context.Background(), target, nil, opts, r); err != nil || len(run.calls) != 1 {
		t.Fatalf("second call should hit the cache: err=%v calls=%d", err, len(run.calls))
	}
}
