package viewer

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// KittyPlacement is everything the TUI needs to show one image through
// the Kitty graphics protocol with Unicode placeholders:
//
//   - Transmit uploads the PNG and creates a virtual placement of
//     Cols x Rows cells (the terminal fits the image into that box,
//     keeping its aspect ratio). It must reach the terminal once.
//   - Lines are Rows strings of Cols placeholder cells each. They are
//     ordinary text, so the layout code, the Bubble Tea renderer and
//     tmux all treat them like any other characters; the terminal draws
//     the image over them wherever they appear.
//   - Delete frees the image and its placement once it is no longer
//     shown.
type KittyPlacement struct {
	ID       int
	Cols     int
	Rows     int
	Transmit string
	Lines    []string
	Delete   string
}

// kittyChunk is the maximum base64 payload per escape sequence allowed
// by the protocol.
const kittyChunk = 4096

// maxPlaceholderSpan bounds Cols and Rows: the row/column diacritic
// table has a few more entries, but nothing on screen needs them.
const maxPlaceholderSpan = 256

// NextKittyID returns the image id to use after prev. Ids stay in
// 1..255 so the placeholder cells can carry them in a 256-colour SGR,
// which tmux forwards untouched (a 24-bit colour may be quantised).
func NextKittyID(prev int) int {
	if prev < 1 || prev >= 255 {
		return 1
	}
	return prev + 1
}

// EncodeKitty builds the placement for img under image id (1..255)
// in a box of cols x rows cells. tmuxDepth is the number of tmux
// servers between the process and the terminal; every escape sequence
// is wrapped in that many passthrough layers so each server forwards
// it instead of swallowing it.
func EncodeKitty(img InlineImage, id, cols, rows, tmuxDepth int) (KittyPlacement, error) {
	if id < 1 || id > 255 {
		return KittyPlacement{}, fmt.Errorf("viewer: kitty image id %d out of range 1..255", id)
	}
	if len(img.PNG) == 0 {
		return KittyPlacement{}, fmt.Errorf("viewer: empty image")
	}
	cols = clamp(cols, 1, maxPlaceholderSpan)
	rows = clamp(rows, 1, maxPlaceholderSpan)

	payload := base64.StdEncoding.EncodeToString(img.PNG)
	var transmit strings.Builder
	for start := 0; start < len(payload); start += kittyChunk {
		end := start + kittyChunk
		if end > len(payload) {
			end = len(payload)
		}
		var opts []string
		if start == 0 {
			opts = []string{"a=T", "U=1", "q=2", "f=100",
				fmt.Sprintf("i=%d", id), fmt.Sprintf("c=%d", cols), fmt.Sprintf("r=%d", rows)}
		} else {
			opts = []string{"q=2"}
		}
		more := "m=1"
		if end == len(payload) {
			more = "m=0"
		}
		opts = append(opts, more)
		transmit.WriteString(wrapTmux(ansi.KittyGraphics([]byte(payload[start:end]), opts...), tmuxDepth))
	}

	del := ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", id), "q=2")
	return KittyPlacement{
		ID:       id,
		Cols:     cols,
		Rows:     rows,
		Transmit: transmit.String(),
		Lines:    placeholderLines(id, cols, rows),
		Delete:   wrapTmux(del, tmuxDepth),
	}, nil
}

// placeholderLines renders the Unicode placeholder grid: each cell is
// U+10EEEE followed by the row and column diacritics, and the image id
// rides in the foreground colour.
func placeholderLines(id, cols, rows int) []string {
	lines := make([]string, rows)
	for r := 0; r < rows; r++ {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("\x1b[38;5;%dm", id))
		for c := 0; c < cols; c++ {
			sb.WriteRune(kitty.Placeholder)
			sb.WriteRune(kitty.Diacritic(r))
			sb.WriteRune(kitty.Diacritic(c))
		}
		sb.WriteString("\x1b[39m")
		lines[r] = sb.String()
	}
	return lines
}

// wrapTmux wraps seq in depth layers of tmux passthrough (DCS tmux; ...
// ST with every ESC doubled). The innermost layer is for the outermost
// server: each server strips one layer and forwards the remainder, so
// the terminal finally receives the raw sequence.
func wrapTmux(seq string, depth int) string {
	for i := 0; i < depth; i++ {
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
