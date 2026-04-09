package claudeio

import (
	"regexp"
	"strings"
)

// AttentionHint summarises what an interactive prompt detector found
// in a terminal capture. Waiting=true means the user needs to act;
// Title + Body carry a short human-readable description the TUI
// shows in its attention banner.
type AttentionHint struct {
	Waiting bool
	Title   string
	Body    string
	Source  string // "capture" for tmux-pane detection
}

// DetectAttention scans a tmux capture-pane snapshot for the well-
// known interactive prompts Claude Code (and its cousins) emit when
// they need the user to choose an action. It is a pure function so
// it is trivial to unit-test and easy to extend with new markers.
//
// The detector recognises:
//
//   - "Do you want to proceed?" — Claude's default tool approval
//     dialog ("1. Yes  2. Yes, and don't ask again  3. No").
//   - "Permission required" — alternative phrasing used by
//     permission-prompt hooks.
//   - "Approve?" / "Continue? (y/n)" — common fallback prompts.
//
// When multiple markers are present, the first match wins so the
// returned Title stays stable across ticks.
func DetectAttention(capture string) AttentionHint {
	if capture == "" {
		return AttentionHint{}
	}
	lines := splitPlainLines(capture)
	joined := strings.Join(lines, "\n")

	for _, m := range attentionMarkers {
		if !strings.Contains(joined, m.marker) {
			continue
		}
		body := nearestContextAbove(lines, m.marker)
		return AttentionHint{
			Waiting: true,
			Title:   m.title,
			Body:    body,
			Source:  "capture",
		}
	}
	return AttentionHint{}
}

type attentionMarker struct {
	marker string
	title  string
}

var attentionMarkers = []attentionMarker{
	{marker: "Do you want to proceed?", title: "Permission required"},
	{marker: "Permission required", title: "Permission required"},
	{marker: "Do you want to continue?", title: "Confirmation needed"},
	{marker: "Approve this", title: "Approval needed"},
	{marker: "(y/n)", title: "Input needed"},
}

// nearestContextAbove walks up from the line containing marker and
// returns the first non-empty line it finds. That line is almost
// always the tool-use description (e.g. `Web Search("...")`), which
// gives the user enough context to decide without leaving mosaic.
func nearestContextAbove(lines []string, marker string) string {
	hitIdx := -1
	for i, l := range lines {
		if strings.Contains(l, marker) {
			hitIdx = i
			break
		}
	}
	if hitIdx <= 0 {
		return ""
	}
	for j := hitIdx - 1; j >= 0; j-- {
		if s := strings.TrimSpace(lines[j]); s != "" {
			// Trim a leading box-drawing gutter ("│ ", "┃ ")
			// so the body reads as plain text.
			return strings.TrimSpace(strings.TrimLeft(s, "│┃├└─ "))
		}
	}
	return ""
}

// splitPlainLines strips ANSI SGR escapes out of a capture string
// and returns it split on newlines. We only care about the visible
// text, not the colours.
func splitPlainLines(capture string) []string {
	plain := stripANSI(capture)
	return strings.Split(plain, "\n")
}

// ansiSGR matches any ECMA-48 SGR (and friends) escape sequence —
// enough to clean up a tmux capture so our plain-text matchers
// behave.
var ansiSGR = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiSGR.ReplaceAllString(s, "")
}
