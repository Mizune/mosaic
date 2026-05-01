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
// To avoid false positives on assistant message text that happens to
// contain a marker phrase (e.g. an answer that quotes "Permission
// required"), every marker hit must be corroborated by an actual
// prompt-UI shape near the marker line. Today that means a numbered-
// choice line ("1. ...", optionally led by `›` or `❯`) within a
// short window after the marker. The choice list is the most stable
// signal: it appears in every Claude Code prompt variant we have
// seen and is essentially never present in narrative output.
//
// When multiple markers are present, the first corroborated match
// wins so the returned Title stays stable across ticks.
func DetectAttention(capture string) AttentionHint {
	if capture == "" {
		return AttentionHint{}
	}
	lines := splitPlainLines(capture)

	for _, m := range attentionMarkers {
		hitIdx := findMarker(lines, m.marker)
		if hitIdx < 0 {
			continue
		}
		if !hasChoiceListNear(lines, hitIdx) {
			continue
		}
		return AttentionHint{
			Waiting: true,
			Title:   m.title,
			Body:    nearestContextAboveAt(lines, hitIdx),
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

// choiceListWindow is how many lines after the marker we look for a
// numbered choice. Claude Code's prompt UI puts the first choice on
// the very next non-empty line, so 6 is generous enough to absorb a
// blank line or two of padding without letting unrelated text two
// paragraphs down qualify.
const choiceListWindow = 6

// numberedChoice matches a line that starts with an optional cursor
// glyph (`›`, `❯`, `>`) and then `1.`. We only require choice "1"
// because `2.`/`3.` are not always present (some prompts are
// 2-option), and a single line of this exact shape is already a
// near-zero-false-positive signal in real captures.
var numberedChoice = regexp.MustCompile(`^\s*[›❯>]?\s*1\.\s+\S`)

func findMarker(lines []string, marker string) int {
	for i, l := range lines {
		if strings.Contains(l, marker) {
			return i
		}
	}
	return -1
}

func hasChoiceListNear(lines []string, hitIdx int) bool {
	end := hitIdx + 1 + choiceListWindow
	if end > len(lines) {
		end = len(lines)
	}
	for i := hitIdx + 1; i < end; i++ {
		if numberedChoice.MatchString(lines[i]) {
			return true
		}
	}
	return false
}

// nearestContextAboveAt walks up from hitIdx and returns the first
// non-empty line it finds. That line is almost always the tool-use
// description (e.g. `Web Search("...")`), which gives the user enough
// context to decide without leaving mosaic.
func nearestContextAboveAt(lines []string, hitIdx int) string {
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
