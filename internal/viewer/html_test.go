package viewer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Mizune/mosaic/internal/config"
)

var update = flag.Bool("update", false, "rewrite golden files under testdata/")

// goldenSource carries markup that would break out of the <pre> if the
// page were built by string concatenation.
const goldenSource = "graph TD\n  A[\"</pre><script>alert(1)</script>\"] --> B\n  B --> C{ok?}\n"

const cdnScript = "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"

func TestWriteMermaidGolden(t *testing.T) {
	root := t.TempDir()
	w := &PageWriter{Root: root}
	target := Target{
		SessionID: "01HSESSION",
		Kind:      KindMermaid,
		Title:     "graph TD",
		Content:   goldenSource,
		Hash:      "abc123",
	}
	path, err := w.WriteMermaid(target, cdnScript)
	if err != nil {
		t.Fatalf("WriteMermaid: %v", err)
	}
	if want := filepath.Join(root, "01HSESSION", "abc123.html"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	golden := filepath.Join("testdata", "mermaid_page.golden.html")
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("page differs from %s (run with -update after eyeballing)\n--- got ---\n%s", golden, got)
	}

	// Belt and braces beyond the golden: the hostile source must be
	// entity-escaped and must never appear raw.
	page := string(got)
	if strings.Contains(page, "<script>alert(1)</script>") {
		t.Error("raw <script> from the diagram source leaked into the page")
	}
	const escaped = "&lt;/pre&gt;&lt;script&gt;alert(1)&lt;/script&gt;"
	if strings.Count(page, escaped) != 2 {
		t.Errorf("escaped source should appear in the diagram and the <details> fallback: %q", page)
	}
	if !strings.Contains(page, "<title>graph TD</title>") {
		t.Error("title missing")
	}
}

func TestWriteMermaidScriptSrc(t *testing.T) {
	cases := []struct {
		name, scriptURL, wantTag string
	}{
		{"https", cdnScript, `<script src="https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>`},
		{"file", "file:///tmp/mermaid.min.js", `<script src="file:///tmp/mermaid.min.js"></script>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &PageWriter{Root: t.TempDir()}
			path, err := w.WriteMermaid(Target{SessionID: "s", Content: "graph LR\nA-->B", Hash: "h"}, tc.scriptURL)
			if err != nil {
				t.Fatalf("WriteMermaid: %v", err)
			}
			page, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(page), tc.wantTag) {
				t.Errorf("page lacks %s\n%s", tc.wantTag, page)
			}
			if strings.Contains(string(page), "ZgotmplZ") {
				t.Error("html/template neutered the script URL; ScriptURL must be template.URL")
			}
			if strings.Contains(string(page), `type="module"`) {
				t.Error("mermaid must load as a classic script, not an ES module")
			}
		})
	}
}

func TestWriteMermaidDefaultsScriptAndHash(t *testing.T) {
	w := &PageWriter{Root: t.TempDir()}
	src := "sequenceDiagram\n  A->>B: hi\n"
	path, err := w.WriteMermaid(Target{SessionID: "s", Content: src}, "")
	if err != nil {
		t.Fatalf("WriteMermaid: %v", err)
	}
	sum := sha256.Sum256([]byte(src))
	if want := hex.EncodeToString(sum[:])[:16] + ".html"; filepath.Base(path) != want {
		t.Errorf("derived file name = %q, want %q", filepath.Base(path), want)
	}
	page, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `src="`+config.DefaultMermaidJSURL+`"`) {
		t.Errorf("empty scriptURL should fall back to the default build:\n%s", page)
	}
	if !strings.Contains(string(page), "<title>mermaid</title>") {
		t.Error("empty title should fall back to \"mermaid\"")
	}
}

func TestWriteMermaidPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	root := t.TempDir()
	w := &PageWriter{Root: root}
	path, err := w.WriteMermaid(Target{SessionID: "s", Content: "graph TD\nA", Hash: "h"}, cdnScript)
	if err != nil {
		t.Fatalf("WriteMermaid: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %o, want 700", perm)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 600", perm)
	}
}

func TestWriteMermaidRejectsUnsafeNames(t *testing.T) {
	w := &PageWriter{Root: t.TempDir()}
	cases := []struct {
		name   string
		target Target
	}{
		{"empty session", Target{Content: "graph TD\nA"}},
		{"session traversal", Target{SessionID: "../escape", Content: "graph TD\nA"}},
		{"hash with separator", Target{SessionID: "s", Content: "graph TD\nA", Hash: "a/b"}},
		{"dot-dot hash", Target{SessionID: "s", Content: "graph TD\nA", Hash: ".."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := w.WriteMermaid(tc.target, cdnScript); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	var nilWriter *PageWriter
	if _, err := nilWriter.WriteMermaid(Target{SessionID: "s"}, cdnScript); err == nil {
		t.Error("nil PageWriter should error, not panic")
	}
}

func TestFileURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/tmp/a b/c#d.html", "file:///tmp/a%20b/c%23d.html"},
		{"/tmp/q?.html", "file:///tmp/q%3F.html"},
		{"/Users/me/Library/Caches/mosaic/artifacts/01H/abc.html",
			"file:///Users/me/Library/Caches/mosaic/artifacts/01H/abc.html"},
	}
	for _, tc := range cases {
		if got := FileURL(tc.in); got != tc.want {
			t.Errorf("FileURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
