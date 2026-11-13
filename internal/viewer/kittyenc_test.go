package viewer

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

func TestEncodeKitty_SingleChunk(t *testing.T) {
	img := InlineImage{PNG: []byte("tiny-png-bytes"), Width: 4, Height: 2}
	p, err := EncodeKitty(img, 7, 10, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b_Ga=T,U=1,q=2,f=100,i=7,c=10,r=5,m=0;" + base64.StdEncoding.EncodeToString(img.PNG) + "\x1b\\"
	if p.Transmit != want {
		t.Fatalf("transmit = %q\nwant %q", p.Transmit, want)
	}
	if p.ID != 7 || p.Cols != 10 || p.Rows != 5 {
		t.Fatalf("placement meta = %+v", p)
	}
	if len(p.Lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(p.Lines))
	}
	for i, line := range p.Lines {
		if ansi.StringWidth(line) != 10 {
			t.Fatalf("line %d width = %d, want 10", i, ansi.StringWidth(line))
		}
		if !strings.HasPrefix(line, "\x1b[38;5;7m") || !strings.HasSuffix(line, "\x1b[39m") {
			t.Fatalf("line %d must carry the id in a 256-colour SGR: %q", i, line)
		}
		if strings.Count(line, string(kitty.Placeholder)) != 10 {
			t.Fatalf("line %d should hold 10 placeholders", i)
		}
		if !strings.Contains(line, string(kitty.Placeholder)+string(kitty.Diacritic(i))+string(kitty.Diacritic(0))) {
			t.Fatalf("line %d should start at row diacritic %d / col 0", i, i)
		}
	}
	if p.Delete != "\x1b_Ga=d,d=I,i=7,q=2\x1b\\" {
		t.Fatalf("delete = %q", p.Delete)
	}
}

func TestEncodeKitty_ChunksAndTmuxWrapping(t *testing.T) {
	img := InlineImage{PNG: make([]byte, 9000), Width: 1, Height: 1}
	p, err := EncodeKitty(img, 255, 3, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Each passthrough is DCS "tmux;" + (inner with doubled ESC) + ST, so
	// split on the DCS introducer: the inner ST is doubled and never
	// matches the outer terminator on its own.
	parts := strings.Split(p.Transmit, "\x1bPtmux;")
	if parts[0] != "" {
		t.Fatalf("transmit must start with a passthrough, got %q", parts[0])
	}
	seqs := parts[1:]
	if len(seqs) != 3 { // 12000 base64 chars → 4096 + 4096 + 3808
		t.Fatalf("expected 3 passthrough-wrapped chunks, got %d", len(seqs))
	}
	var joined strings.Builder
	for i, s := range seqs {
		if !strings.HasPrefix(s, "\x1b\x1b_G") || !strings.HasSuffix(s, "\x1b\\") {
			t.Fatalf("chunk %d not wrapped for tmux: %q", i, s[:12])
		}
		inner := strings.ReplaceAll(strings.TrimSuffix(s, "\x1b\\"), "\x1b\x1b", "\x1b")
		if !strings.HasSuffix(inner, "\x1b\\") {
			t.Fatalf("chunk %d inner sequence must end with ST", i)
		}
		body := strings.TrimSuffix(strings.TrimPrefix(inner, "\x1b_G"), "\x1b\\")
		opts, payload, _ := strings.Cut(body, ";")
		switch i {
		case 0:
			if opts != "a=T,U=1,q=2,f=100,i=255,c=3,r=2,m=1" {
				t.Fatalf("first chunk opts = %q", opts)
			}
		case 1:
			if opts != "q=2,m=1" {
				t.Fatalf("middle chunk opts = %q", opts)
			}
		case 2:
			if opts != "q=2,m=0" {
				t.Fatalf("last chunk opts = %q", opts)
			}
		}
		if len(payload) > kittyChunk {
			t.Fatalf("chunk %d payload too long: %d", i, len(payload))
		}
		joined.WriteString(payload)
	}
	if joined.String() != base64.StdEncoding.EncodeToString(img.PNG) {
		t.Fatal("chunks do not reassemble into the base64 payload")
	}
	if !strings.HasPrefix(p.Delete, "\x1bPtmux;") {
		t.Fatalf("delete must be wrapped too: %q", p.Delete)
	}
}

func TestEncodeKitty_Validation(t *testing.T) {
	img := InlineImage{PNG: []byte("x")}
	if _, err := EncodeKitty(img, 0, 1, 1, 0); err == nil {
		t.Fatal("id 0 must be rejected")
	}
	if _, err := EncodeKitty(img, 256, 1, 1, 0); err == nil {
		t.Fatal("id 256 must be rejected")
	}
	if _, err := EncodeKitty(InlineImage{}, 1, 1, 1, 0); err == nil {
		t.Fatal("empty image must be rejected")
	}
	p, err := EncodeKitty(img, 1, 0, 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Cols != 1 || p.Rows != maxPlaceholderSpan {
		t.Fatalf("cols/rows should clamp, got %d x %d", p.Cols, p.Rows)
	}
}

func TestNextKittyID(t *testing.T) {
	if NextKittyID(0) != 1 || NextKittyID(1) != 2 || NextKittyID(254) != 255 || NextKittyID(255) != 1 || NextKittyID(-5) != 1 {
		t.Fatal("ids must cycle through 1..255")
	}
}

// unwrapTmux strips one passthrough layer: DCS tmux; ... ST with ESC
// un-doubled, exactly what one tmux server does before forwarding.
func unwrapTmux(t *testing.T, s string) string {
	t.Helper()
	if !strings.HasPrefix(s, "\x1bPtmux;") || !strings.HasSuffix(s, "\x1b\\") {
		t.Fatalf("not a passthrough: %q", s[:min(len(s), 12)])
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "\x1bPtmux;"), "\x1b\\")
	return strings.ReplaceAll(inner, "\x1b\x1b", "\x1b")
}

func TestEncodeKitty_NestedTmuxWrapping(t *testing.T) {
	img := InlineImage{PNG: []byte("png"), Width: 1, Height: 1}
	p, err := EncodeKitty(img, 3, 2, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Two servers: the first strip must yield a still-wrapped sequence,
	// the second the raw APC.
	once := unwrapTmux(t, p.Transmit)
	raw := unwrapTmux(t, once)
	if !strings.HasPrefix(raw, "\x1b_Ga=T,U=1,q=2,f=100,i=3,c=2,r=2,m=0;") || !strings.HasSuffix(raw, "\x1b\\") {
		t.Fatalf("after two unwraps expected the raw sequence, got %q", raw)
	}
	del := unwrapTmux(t, unwrapTmux(t, p.Delete))
	if del != "\x1b_Ga=d,d=I,i=3,q=2\x1b\\" {
		t.Fatalf("delete = %q", del)
	}
	// Depth 0 leaves the sequence untouched.
	p0, _ := EncodeKitty(img, 3, 2, 2, 0)
	if !strings.HasPrefix(p0.Transmit, "\x1b_G") {
		t.Fatal("depth 0 must not wrap")
	}
}
