// Package viewer opens agent-produced artifacts — Mermaid diagrams,
// images, HTML files and plain URLs — in a browser. Inside cmux it
// drives a browser pane through the cmux CLI and reuses that pane for
// later artifacts; anywhere else it hands the URL to the operating
// system's default browser.
//
// The package depends only on the standard library and internal/config.
// Callers describe what to show with a [Target] rather than a store row,
// so the viewer never needs to know how artifacts are persisted.
//
// Every external process is best effort in the style of the TUI's OS
// notifications: a failure comes back to the caller as an error and must
// never take the TUI down. Commands always receive their arguments as a
// slice; nothing is ever interpolated into a shell string.
package viewer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Mizune/mosaic/internal/config"
)

// Kind classifies a Target and therefore how the viewer presents it.
// The values mirror internal/artifact.Kind, so a store row converts
// with a plain string conversion.
type Kind string

const (
	// KindMermaid is a Mermaid diagram whose source is in Target.Content.
	KindMermaid Kind = "mermaid"
	// KindImage is an image file at Target.Path.
	KindImage Kind = "image"
	// KindHTML is an HTML document at Target.Path.
	KindHTML Kind = "html"
	// KindURL is an http(s) or file URL in Target.Content.
	KindURL Kind = "url"
)

// Target is what the TUI asks the viewer to show. It is deliberately
// independent of the store row so this package has no store dependency.
type Target struct {
	// SessionID scopes generated pages to one session's cache directory
	// so killing the session removes them in one go.
	SessionID string
	// Kind selects the presentation strategy.
	Kind Kind
	// Title is the human label; it becomes the <title> of generated pages.
	Title string
	// Content holds the Mermaid source for KindMermaid and the URL for
	// KindURL.
	Content string
	// Path is the absolute file path for KindImage and KindHTML. For
	// KindURL it is consulted when Content is empty.
	Path string
	// Hash is the artifact's content hash and names the generated page.
	// When empty the viewer derives one from Content.
	Hash string
}

// Options tunes a Launcher and the pages written for it.
type Options struct {
	// Placement is where cmux opens the browser pane: "dock" (default),
	// "right" or "down". Unknown values behave like "dock".
	Placement string
	// Focus moves keyboard focus to the browser pane when it opens.
	Focus bool
	// MermaidJSURL is the script generated Mermaid pages load. Empty
	// means config.DefaultMermaidJSURL.
	MermaidJSURL string
	// CacheRoot is the directory generated pages are written under.
	CacheRoot string
	// Inline is the [viewer] inline mode: InlineAuto, InlineOn or InlineOff.
	Inline string
	// ChromePath overrides the browser binary used to rasterize
	// artifacts for the inline preview; empty means auto-detect.
	ChromePath string
}

// OptionsFrom converts the [viewer] config section into Options. The
// cache root is <user cache dir>/mosaic/artifacts, falling back to a
// mosaic-artifacts directory under the system temp dir when the user
// cache dir cannot be determined.
func OptionsFrom(cfg config.ViewerSection) Options {
	return Options{
		Placement:    cfg.Placement,
		Focus:        cfg.Focus,
		MermaidJSURL: cfg.MermaidJSURL,
		CacheRoot:    defaultCacheRoot(),
		Inline:       cfg.Inline,
		ChromePath:   cfg.ChromePath,
	}
}

func defaultCacheRoot() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		return filepath.Join(os.TempDir(), "mosaic-artifacts")
	}
	return filepath.Join(base, "mosaic", "artifacts")
}

// Launcher opens a URL in some browser. Implementations are safe for
// concurrent use; the TUI calls Open from a goroutine with a short
// deadline on ctx.
type Launcher interface {
	// Name identifies the backend ("cmux" or "system") for toasts.
	Name() string
	// Open shows url. It returns once the request has been handed to
	// the browser, not once the page has rendered.
	Open(ctx context.Context, url string) error
}

// New picks the launcher for the current environment: Cmux when mosaic
// runs inside cmux (CMUX_SOCKET_PATH is set and the cmux CLI is on
// PATH), otherwise System.
func New(opts Options) Launcher {
	if os.Getenv("CMUX_SOCKET_PATH") != "" {
		if _, err := exec.LookPath("cmux"); err == nil {
			return newCmux(execRunner{}, opts)
		}
	}
	return System{}
}

// ErrArtifactMissing reports that a file artifact has been deleted or
// moved since it was recorded. The picker shows it instead of dropping
// the row.
var ErrArtifactMissing = errors.New("viewer: artifact file no longer exists")

// ErrUnsupportedURL reports a KindURL target whose scheme is not http,
// https or file.
var ErrUnsupportedURL = errors.New("viewer: unsupported url scheme")

// TargetURL resolves a Target to the URL a Launcher should open.
//
//   - KindMermaid writes a self-contained HTML page through pages (a
//     PageWriter rooted at opts.CacheRoot when pages is nil) and returns
//     its file URL.
//   - KindImage and KindHTML check that the file still exists and return
//     its file URL; a vanished file yields ErrArtifactMissing.
//   - KindURL accepts http, https and file URLs verbatim and rejects any
//     other scheme with ErrUnsupportedURL.
func TargetURL(t Target, pages *PageWriter, opts Options) (string, error) {
	switch t.Kind {
	case KindMermaid:
		if pages == nil {
			pages = &PageWriter{Root: opts.CacheRoot}
		}
		path, err := pages.WriteMermaid(t, opts.MermaidJSURL)
		if err != nil {
			return "", err
		}
		return FileURL(path), nil
	case KindImage, KindHTML:
		return fileTargetURL(t.Path)
	case KindURL:
		raw := t.Content
		if raw == "" {
			raw = t.Path
		}
		return validateURL(raw)
	default:
		return "", fmt.Errorf("viewer: unsupported artifact kind %q", t.Kind)
	}
}

// fileTargetURL turns an on-disk artifact into a file URL after
// confirming it still exists.
func fileTargetURL(path string) (string, error) {
	if path == "" {
		return "", ErrArtifactMissing
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("viewer: resolve artifact path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrArtifactMissing
		}
		return "", fmt.Errorf("viewer: stat artifact: %w", err)
	}
	return FileURL(abs), nil
}

// validateURL admits only schemes a browser pane can show safely.
func validateURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsupportedURL, err)
	}
	switch u.Scheme {
	case "http", "https", "file":
		return raw, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedURL, u.Scheme)
	}
}
