package artifact

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Mizune/mosaic/pkg/transcript"
)

func TestExtractMermaid(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"none", "plain text\n```go\nfmt.Println()\n```\n", nil},
		{"single", "intro\n```mermaid\ngraph TD\n  A-->B\n```\noutro", []string{"graph TD\n  A-->B"}},
		{"multiple", "```mermaid\nA\n```\ntext\n```mermaid\nB\n```", []string{"A", "B"}},
		{"tilde fence", "~~~mermaid\nsequenceDiagram\n~~~", []string{"sequenceDiagram"}},
		{"case insensitive", "```Mermaid\npie\n```", []string{"pie"}},
		{"info string with attrs", "```mermaid title=x\nflowchart LR\n```", []string{"flowchart LR"}},
		{"indented fence", "  ```mermaid\n  graph LR\n  ```", []string{"  graph LR"}},
		{"longer closing run", "````mermaid\ngraph TD\n`````", []string{"graph TD"}},
		{"shorter run does not close", "````mermaid\ngraph TD\n```\nstill\n````", []string{"graph TD\n```\nstill"}},
		{"backticks inside other fence", "```md\n```mermaid\nnot a diagram\n```\n", nil},
		{"unterminated", "```mermaid\ngraph TD\n  A-->B\n", nil},
		{"inline code is not a fence", "```mermaid `x`\ngraph TD\n```", nil},
		{"empty body kept for caller to drop", "```mermaid\n```", []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractMermaid(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractMermaid_SkipsHugeBlocks(t *testing.T) {
	huge := "```mermaid\n" + strings.Repeat("A-->B\n", maxTextBlockBytes/6+1) + "```"
	if got := ExtractMermaid(huge); got != nil {
		t.Fatalf("expected huge block to be skipped, got %d diagrams", len(got))
	}
}

func TestKindForPath(t *testing.T) {
	cases := map[string]struct {
		kind Kind
		ok   bool
	}{
		"a.png":        {KindImage, true},
		"b.JPG":        {KindImage, true},
		"c.jpeg":       {KindImage, true},
		"d.gif":        {KindImage, true},
		"e.webp":       {KindImage, true},
		"f.svg":        {KindImage, true},
		"g.html":       {KindHTML, true},
		"h.htm":        {KindHTML, true},
		"i.go":         {"", false},
		"j.md":         {"", false},
		"noext":        {"", false},
		"dir.png/x.go": {"", false},
	}
	for p, want := range cases {
		kind, ok := KindForPath(p)
		if kind != want.kind || ok != want.ok {
			t.Errorf("KindForPath(%q) = (%q, %v), want (%q, %v)", p, kind, ok, want.kind, want.ok)
		}
	}
}

func TestTitleFor(t *testing.T) {
	if got := TitleFor("\n\n  graph TD  \n A-->B"); got != "graph TD" {
		t.Fatalf("TitleFor = %q", got)
	}
	long := strings.Repeat("x", 80)
	got := TitleFor(long)
	if len([]rune(got)) != maxTitleRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("TitleFor(long) = %q (%d runes)", got, len([]rune(got)))
	}
	if got := TitleFor("   \n"); got != "mermaid" {
		t.Fatalf("TitleFor(blank) = %q", got)
	}
}

func TestContentHash(t *testing.T) {
	a, b := ContentHash("graph TD"), ContentHash("graph TD")
	if a != b || len(a) != hashLen {
		t.Fatalf("hash not stable or wrong length: %q %q", a, b)
	}
	if ContentHash("graph TD") == ContentHash("graph LR") {
		t.Fatal("different inputs must hash differently")
	}
}

func toolUse(name, inputJSON string) transcript.ContentBlock {
	return transcript.ContentBlock{Type: "tool_use", ID: "t1", Name: name, Input: json.RawMessage(inputJSON)}
}

func TestDetectWrittenFile(t *testing.T) {
	cases := []struct {
		name     string
		block    transcript.ContentBlock
		cwd, wt  string
		wantPath string
		wantKind Kind
		ok       bool
	}{
		{"write png absolute", toolUse("Write", `{"file_path":"/tmp/out/diagram.png","content":"x"}`), "/cwd", "/wt", "/tmp/out/diagram.png", KindImage, true},
		{"edit html", toolUse("Edit", `{"file_path":"/tmp/report.html","old_string":"a","new_string":"b"}`), "", "", "/tmp/report.html", KindHTML, true},
		{"multiedit svg", toolUse("MultiEdit", `{"file_path":"/tmp/a.svg"}`), "", "", "/tmp/a.svg", KindImage, true},
		{"relative resolves against cwd", toolUse("Write", `{"file_path":"docs/arch.svg"}`), "/cwd", "/wt", "/cwd/docs/arch.svg", KindImage, true},
		{"relative falls back to worktree", toolUse("Write", `{"file_path":"shot.png"}`), "", "/wt", "/wt/shot.png", KindImage, true},
		{"cleans dot segments", toolUse("Write", `{"file_path":"/tmp/./x/../y.png"}`), "", "", "/tmp/y.png", KindImage, true},
		{"non-visual extension", toolUse("Write", `{"file_path":"/tmp/main.go"}`), "", "", "", "", false},
		{"other tool", toolUse("Read", `{"file_path":"/tmp/a.png"}`), "", "", "", "", false},
		{"malformed input", toolUse("Write", `{"file_path":`), "", "", "", "", false},
		{"missing file_path", toolUse("Write", `{"content":"x"}`), "", "", "", "", false},
		{"text block", transcript.ContentBlock{Type: "text", Text: "a.png"}, "", "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := DetectWrittenFile(tc.block, tc.cwd, tc.wt)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if d.Path != filepath.Clean(tc.wantPath) || d.Kind != tc.wantKind {
				t.Fatalf("got (%q, %q), want (%q, %q)", d.Path, d.Kind, tc.wantPath, tc.wantKind)
			}
			if d.Title != filepath.Base(tc.wantPath) || d.Hash != ContentHash(d.Path) {
				t.Fatalf("title/hash mismatch: %+v", d)
			}
		})
	}
}

func TestDetectFromEntry(t *testing.T) {
	ts := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	entry := transcript.Entry{
		Type:      transcript.EntryAssistant,
		Timestamp: ts,
		CWD:       "/cwd",
		Message: &transcript.Message{Role: "assistant", Content: transcript.ContentList{
			{Type: "text", Text: "Here:\n```mermaid\ngraph TD\nA-->B\n```\nand again\n```mermaid\ngraph TD\nA-->B\n```"},
			toolUse("Write", `{"file_path":"out/shot.png"}`),
			toolUse("Write", `{"file_path":"out/shot.png"}`),
			toolUse("Write", `{"file_path":"main.go"}`),
			{Type: "text", Text: "```mermaid\n\n```"},
		}},
	}
	got := DetectFromEntry(entry, "/wt")
	if len(got) != 2 {
		t.Fatalf("expected 2 artifacts (dedup within entry), got %d: %+v", len(got), got)
	}
	if got[0].Kind != KindMermaid || got[0].Content != "graph TD\nA-->B" || got[0].Title != "graph TD" {
		t.Fatalf("mermaid artifact wrong: %+v", got[0])
	}
	if got[1].Kind != KindImage || got[1].Path != "/cwd/out/shot.png" {
		t.Fatalf("image artifact wrong: %+v", got[1])
	}
	for _, d := range got {
		if !d.At.Equal(ts) {
			t.Fatalf("At should be the entry timestamp, got %v", d.At)
		}
	}
}

func TestDetectFromEntry_IgnoresNonAssistant(t *testing.T) {
	user := transcript.Entry{Type: transcript.EntryUser, Message: &transcript.Message{Content: transcript.ContentList{
		{Type: "text", Text: "```mermaid\ngraph TD\n```"},
	}}}
	if got := DetectFromEntry(user, ""); got != nil {
		t.Fatalf("user entries must be ignored, got %+v", got)
	}
	if got := DetectFromEntry(transcript.Entry{Type: transcript.EntryAssistant}, ""); got != nil {
		t.Fatalf("nil message must be ignored, got %+v", got)
	}
}

func TestDetectFromEntry_DefaultsTimestamp(t *testing.T) {
	entry := transcript.Entry{Type: transcript.EntryAssistant, Message: &transcript.Message{Content: transcript.ContentList{
		{Type: "text", Text: "```mermaid\npie\n```"},
	}}}
	got := DetectFromEntry(entry, "")
	if len(got) != 1 || got[0].At.IsZero() {
		t.Fatalf("expected a non-zero At, got %+v", got)
	}
}
