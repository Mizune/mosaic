package viewer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mizune/mosaic/internal/config"
)

func TestTargetURLMissingImageReturnsErrArtifactMissing(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "diagram.png")
	_, err := TargetURL(Target{Kind: KindImage, Path: gone}, nil, Options{})
	if !errors.Is(err, ErrArtifactMissing) {
		t.Fatalf("err = %v, want ErrArtifactMissing", err)
	}
	_, err = TargetURL(Target{Kind: KindHTML, Path: ""}, nil, Options{})
	if !errors.Is(err, ErrArtifactMissing) {
		t.Fatalf("empty path err = %v, want ErrArtifactMissing", err)
	}
}

func TestTargetURLExistingFileBecomesFileURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report #1.html")
	if err := os.WriteFile(path, []byte("<p>hi</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := TargetURL(Target{Kind: KindHTML, Path: path}, nil, Options{})
	if err != nil {
		t.Fatalf("TargetURL: %v", err)
	}
	if want := FileURL(path); got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
}

func TestTargetURLRejectsJavascriptScheme(t *testing.T) {
	_, err := TargetURL(Target{Kind: KindURL, Content: "javascript:alert(1)"}, nil, Options{})
	if !errors.Is(err, ErrUnsupportedURL) {
		t.Fatalf("err = %v, want ErrUnsupportedURL", err)
	}
	_, err = TargetURL(Target{Kind: KindURL, Content: "/just/a/path"}, nil, Options{})
	if !errors.Is(err, ErrUnsupportedURL) {
		t.Fatalf("schemeless err = %v, want ErrUnsupportedURL", err)
	}
}

func TestTargetURLAcceptsWebAndFileURLs(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:3000/",
		"https://example.com/report?x=1#top",
		"file:///tmp/page.html",
	} {
		got, err := TargetURL(Target{Kind: KindURL, Content: raw}, nil, Options{})
		if err != nil || got != raw {
			t.Errorf("TargetURL(%q) = %q, %v; want verbatim", raw, got, err)
		}
	}
	// Path is consulted when Content is empty so either store column works.
	got, err := TargetURL(Target{Kind: KindURL, Path: "https://example.com/"}, nil, Options{})
	if err != nil || got != "https://example.com/" {
		t.Errorf("Path fallback = %q, %v", got, err)
	}
}

func TestTargetURLMermaidWritesPageUnderCacheRoot(t *testing.T) {
	root := t.TempDir()
	target := Target{SessionID: "01HSESSION", Kind: KindMermaid, Content: "graph TD\nA-->B", Hash: "h1"}
	got, err := TargetURL(target, nil, Options{CacheRoot: root})
	if err != nil {
		t.Fatalf("TargetURL: %v", err)
	}
	wantPath := filepath.Join(root, "01HSESSION", "h1.html")
	if got != FileURL(wantPath) {
		t.Errorf("url = %q, want %q", got, FileURL(wantPath))
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("page not written: %v", err)
	}
	// An explicit PageWriter wins over opts.CacheRoot.
	other := t.TempDir()
	got, err = TargetURL(target, &PageWriter{Root: other}, Options{CacheRoot: root})
	if err != nil {
		t.Fatalf("TargetURL with writer: %v", err)
	}
	if !strings.HasPrefix(got, FileURL(other)) {
		t.Errorf("url = %q, want under %q", got, FileURL(other))
	}
}

func TestTargetURLUnknownKind(t *testing.T) {
	if _, err := TargetURL(Target{Kind: Kind("video")}, nil, Options{}); err == nil {
		t.Fatal("expected an error for an unknown kind")
	}
}

func TestOptionsFrom(t *testing.T) {
	cfg := config.ViewerSection{
		Placement:    "down",
		Focus:        true,
		MermaidJSURL: "file:///opt/mermaid.min.js",
	}
	opts := OptionsFrom(cfg)
	if opts.Placement != "down" || !opts.Focus || opts.MermaidJSURL != cfg.MermaidJSURL {
		t.Errorf("OptionsFrom = %+v, want fields copied from %+v", opts, cfg)
	}
	if !filepath.IsAbs(opts.CacheRoot) {
		t.Errorf("CacheRoot = %q, want an absolute path", opts.CacheRoot)
	}
	want1 := filepath.Join("mosaic", "artifacts")
	if !strings.HasSuffix(opts.CacheRoot, want1) && filepath.Base(opts.CacheRoot) != "mosaic-artifacts" {
		t.Errorf("CacheRoot = %q, want …/%s or …/mosaic-artifacts", opts.CacheRoot, want1)
	}
}
