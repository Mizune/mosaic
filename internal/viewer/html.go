package viewer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Mizune/mosaic/internal/config"
)

// PageWriter renders Mermaid artifacts into self-contained HTML pages at
// Root/<session id>/<hash>.html so a browser can show them from a file
// URL. Directories are created 0700 and pages 0600 because a page embeds
// the agent's diagram source.
type PageWriter struct {
	Root string
}

// mermaidPage is the data handed to mermaidTemplate. ScriptURL is a
// template.URL on purpose: html/template rewrites an untyped file://
// value inside a src attribute to #ZgotmplZ, which would break every
// locally hosted mermaid build.
type mermaidPage struct {
	Title     string
	Source    string
	ScriptURL template.URL
}

// mermaidTemplate loads mermaid with a classic <script> tag rather than
// an ES module because WebKit refuses module imports on file:// pages.
// The raw source also sits in a <details> block so the diagram text is
// still readable when the script cannot be fetched (offline, CDN outage).
var mermaidTemplate = template.Must(template.New("mermaid").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
  :root { color-scheme: dark; }
  html, body { margin: 0; min-height: 100%; background: #1e1e2e; color: #cdd6f4; }
  body { padding: 24px; font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; }
  header { margin: 0 0 16px; opacity: .7; }
  pre.mermaid { display: flex; justify-content: center; margin: 0; }
  details { margin-top: 24px; }
  summary { cursor: pointer; opacity: .7; }
  details pre { padding: 12px; border-radius: 6px; background: #181825; overflow: auto; font: 13px/1.4 ui-monospace, SFMono-Regular, Menlo, monospace; }
</style>
</head>
<body>
<header>{{.Title}}</header>
<pre class="mermaid">{{.Source}}</pre>
<details><summary>source</summary><pre>{{.Source}}</pre></details>
<script src="{{.ScriptURL}}"></script>
<script>mermaid.initialize({startOnLoad:true,theme:'dark'});</script>
</body>
</html>
`))

// hashLen mirrors internal/artifact.ContentHash so a page named from a
// derived hash matches the one a detected artifact would have produced.
const hashLen = 16

// WriteMermaid renders t's Mermaid source into Root/<SessionID>/<Hash>.html
// and returns the absolute path. scriptURL is the mermaid build the page
// loads; empty means config.DefaultMermaidJSURL. When t.Hash is empty the
// file is named after the SHA-256 of t.Content. The source is always
// passed through html/template escaping, so a diagram containing markup
// cannot break out of its <pre>.
func (w *PageWriter) WriteMermaid(t Target, scriptURL string) (string, error) {
	if w == nil || w.Root == "" {
		return "", errors.New("viewer: page writer has no root directory")
	}
	if err := checkPathComponent("session id", t.SessionID); err != nil {
		return "", err
	}
	hash := t.Hash
	if hash == "" {
		hash = contentHash(t.Content)
	}
	if err := checkPathComponent("hash", hash); err != nil {
		return "", err
	}
	if scriptURL == "" {
		scriptURL = config.DefaultMermaidJSURL
	}
	title := t.Title
	if title == "" {
		title = "mermaid"
	}

	var buf bytes.Buffer
	page := mermaidPage{Title: title, Source: t.Content, ScriptURL: template.URL(scriptURL)}
	if err := mermaidTemplate.Execute(&buf, page); err != nil {
		return "", fmt.Errorf("viewer: render mermaid page: %w", err)
	}

	dir := filepath.Join(w.Root, t.SessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("viewer: create page dir: %w", err)
	}
	path := filepath.Join(dir, hash+".html")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("viewer: write page: %w", err)
	}
	return path, nil
}

// checkPathComponent rejects values that could escape the cache
// directory when joined onto Root. Session IDs are ULIDs and hashes are
// hex, so anything else is a bug upstream rather than user input.
func checkPathComponent(what, v string) error {
	if v == "" {
		return fmt.Errorf("viewer: %s is required", what)
	}
	if v == "." || v == ".." || v != filepath.Base(v) || strings.ContainsAny(v, `/\`) {
		return fmt.Errorf("viewer: %s %q is not a safe file name", what, v)
	}
	return nil
}

// contentHash derives the page name for a Target without a Hash.
func contentHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:hashLen]
}

// FileURL converts an absolute path into a file:// URL. It goes through
// url.URL so spaces, '#' and '?' are percent-encoded while path
// separators are kept — url.PathEscape would escape the slashes too.
func FileURL(abs string) string {
	return (&url.URL{Scheme: "file", Path: abs}).String()
}
