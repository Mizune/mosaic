package claudeio

import (
	"strconv"
	"strings"
)

// OscKind identifies which OSC family a scanned hint belongs to. It
// exists so the TUI can branch on "notification style" vs "progress
// style" without having to inspect the raw sequence.
type OscKind int

const (
	// OscUnknown is the zero value. Hints with this kind are never
	// returned by [ScanOSC].
	OscUnknown OscKind = iota

	// OscNotify is the rxvt-style notification escape:
	//
	//	ESC ] 777 ; notify ; <title> ; <body> BEL
	//
	// mosaic treats these as attention events (the same way a
	// Claude Code `Notification` hook would) so a plain shell
	// script can surface "needs input" state without installing
	// hooks.
	OscNotify

	// OscProgress is the ConEmu-compatible progress escape:
	//
	//	ESC ] 9 ; 4 ; <state> ; <percent> BEL
	//
	// state 0 = remove, 1 = normal, 2 = error, 3 = indeterminate,
	// 4 = paused/warning. mosaic surfaces state=2 (error) as an
	// attention event; other states are returned so the caller
	// can render progress bars if desired.
	OscProgress
)

// OscHint is a structured view of a single OSC escape sequence that
// mosaic recognised in a terminal capture.
type OscHint struct {
	Kind            OscKind
	Title           string
	Body            string
	ProgressState   int
	ProgressPercent int
}

// ScanOSC walks capture and returns every OSC hint mosaic recognises,
// in the order they appear. Sequences mosaic does not understand are
// silently skipped — the scanner is forgiving so passing a real tmux
// capture-pane snapshot (with unrelated OSC 0;title-set sequences,
// truncated fragments, whatever) never surprises the caller.
//
// Both BEL (\x07) and the ST terminator (ESC \) are accepted as
// sequence terminators, matching what every modern terminal emits.
func ScanOSC(capture string) []OscHint {
	if capture == "" {
		return nil
	}
	var hints []OscHint
	for i := 0; i < len(capture); {
		// An OSC sequence starts with ESC ].
		if capture[i] != 0x1b || i+1 >= len(capture) || capture[i+1] != ']' {
			i++
			continue
		}
		start := i + 2
		end, termLen := findOscTerminator(capture, start)
		if end < 0 {
			// Unterminated: treat the rest of the string as garbage
			// and stop — anything after a half-written OSC is
			// almost certainly corrupted.
			break
		}
		body := capture[start:end]
		if hint, ok := parseOscBody(body); ok {
			hints = append(hints, hint)
		}
		i = end + termLen
	}
	return hints
}

// findOscTerminator returns the index of the terminator (BEL or ST)
// that closes an OSC sequence whose body starts at i, and the length
// of that terminator in bytes. Returns (-1, 0) when no terminator is
// found before end-of-string.
func findOscTerminator(s string, i int) (int, int) {
	for j := i; j < len(s); j++ {
		switch s[j] {
		case 0x07:
			return j, 1
		case 0x1b:
			if j+1 < len(s) && s[j+1] == '\\' {
				return j, 2
			}
		}
	}
	return -1, 0
}

// parseOscBody decodes the body of an OSC sequence (i.e. the text
// between `ESC ]` and the BEL/ST terminator). It returns a recognised
// hint or ok=false.
func parseOscBody(body string) (OscHint, bool) {
	// OSC 777 ; notify ; title ; body
	if rest, ok := strings.CutPrefix(body, "777;"); ok {
		parts := strings.SplitN(rest, ";", 3)
		if len(parts) >= 2 && parts[0] == "notify" {
			h := OscHint{Kind: OscNotify, Title: parts[1]}
			if len(parts) == 3 {
				h.Body = parts[2]
			}
			return h, true
		}
		return OscHint{}, false
	}

	// OSC 9 ; 4 ; state ; percent (ConEmu-compatible progress)
	if rest, ok := strings.CutPrefix(body, "9;"); ok {
		parts := strings.Split(rest, ";")
		if len(parts) >= 2 && parts[0] == "4" {
			h := OscHint{Kind: OscProgress}
			if n, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
				h.ProgressState = n
			}
			if len(parts) >= 3 {
				if n, err := strconv.Atoi(strings.TrimSpace(parts[2])); err == nil {
					h.ProgressPercent = n
				}
			}
			return h, true
		}
		return OscHint{}, false
	}

	return OscHint{}, false
}

// OscAttention picks the most attention-worthy hint out of a list of
// [OscHint] and returns it as an [AttentionHint], or the zero value
// if nothing in the list warrants attention.
//
// Rules, highest priority first:
//
//  1. An OscNotify hint always becomes a waiting attention whose
//     Title/Body mirror the escape sequence.
//  2. An OscProgress hint with state=2 (error) becomes an "Error"
//     attention whose Body records the percent at the failure point.
//  3. Everything else — progress updates, removal signals, unknown
//     kinds — is ignored.
//
// This helper lives here rather than next to applyAttentionHint so
// the TUI can keep its attention logic source-agnostic.
func OscAttention(hints []OscHint) AttentionHint {
	for _, h := range hints {
		if h.Kind == OscNotify {
			title := h.Title
			if title == "" {
				title = "Notification"
			}
			return AttentionHint{
				Waiting: true,
				Title:   title,
				Body:    h.Body,
				Source:  "osc",
			}
		}
	}
	for _, h := range hints {
		if h.Kind == OscProgress && h.ProgressState == 2 {
			return AttentionHint{
				Waiting: true,
				Title:   "Error",
				Body:    "progress reported error at " + strconv.Itoa(h.ProgressPercent) + "%",
				Source:  "osc",
			}
		}
	}
	return AttentionHint{}
}
