package claudeio_test

import (
	"testing"

	"github.com/Mizune/mosaic/internal/claudeio"
)

func TestDetectAttention_ProceedPrompt(t *testing.T) {
	capture := `Tool use

  Web Search("Claude Code release date 2026")
  Claude wants to search the web for: Claude Code ...

Do you want to proceed?
› 1. Yes
  2. Yes, and don't ask again for Web Search commands
  3. No`

	got := claudeio.DetectAttention(capture)
	if !got.Waiting {
		t.Fatalf("expected Waiting=true, got %+v", got)
	}
	if got.Title != "Permission required" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Body == "" {
		t.Errorf("body should carry the tool-use context line, got empty")
	}
}

func TestDetectAttention_NoPrompt(t *testing.T) {
	capture := `$ claude

welcome back!
type a message below.
> `
	got := claudeio.DetectAttention(capture)
	if got.Waiting {
		t.Fatalf("expected Waiting=false, got %+v", got)
	}
}

func TestDetectAttention_StripsANSI(t *testing.T) {
	capture := "\x1b[1;31mDo you want to proceed?\x1b[0m\n› 1. Yes"
	got := claudeio.DetectAttention(capture)
	if !got.Waiting {
		t.Errorf("ANSI-wrapped marker should still match")
	}
}

// Regression: marker phrases appearing in narrative assistant output
// must not flip a session into waiting_input. Without the choice-list
// corroboration any answer that quotes "Permission required" or
// "Do you want to proceed?" would be misread as a real prompt — which
// in auto mode pinned the ⚠ icon for the entire conversation.
func TestDetectAttention_AssistantTextDoesNotMatch(t *testing.T) {
	cases := []string{
		// Plain narrative containing the marker phrase.
		`Background: the title shown is "Permission required" but it is
just a label, the agent does not actually need approval here.`,
		// Marker followed by an unrelated numbered list (a choice list
		// entry must look like a Claude Code option line, not "1) item"
		// or paragraph numbering on a different topic).
		`Permission required is a string used in 3 places:

  - internal/hook/classify.go
  - internal/claudeio/prompt_detect.go
  - internal/tui/views.go`,
		// Marker buried in a long answer, no UI shape near it.
		`...applyAttentionHint sets attention_state to waiting_input
when the capture matches one of: Do you want to proceed?,
Permission required, (y/n).`,
	}
	for i, capture := range cases {
		got := claudeio.DetectAttention(capture)
		if got.Waiting {
			t.Errorf("case %d: expected Waiting=false, got %+v", i, got)
		}
	}
}

// Regression: an earlier narrative mention of a marker phrase must
// not shadow a genuine prompt that appears later in the same capture.
// Before the fix, findMarker returned only the first hit; if the
// choice-list corroboration failed at that line the real prompt below
// was missed and the session never flipped to waiting_input.
func TestDetectAttention_NarrativeBeforeRealPrompt(t *testing.T) {
	capture := `assistant: I'll explain the "Permission required" banner first
and then ask you to confirm.

Tool use

  Web Search("Claude Code release date 2026")

Permission required
› 1. Yes
  2. Yes, and don't ask again for Web Search commands
  3. No`

	got := claudeio.DetectAttention(capture)
	if !got.Waiting {
		t.Fatalf("expected Waiting=true, got %+v", got)
	}
	if got.Title != "Permission required" {
		t.Errorf("title = %q", got.Title)
	}
}

// Sanity: the corroborating choice list works even when the cursor
// glyph (`›` / `❯` / `>`) is missing — Claude Code occasionally
// renders the list without the leading caret on a re-paint tick.
func TestDetectAttention_ChoiceListWithoutCursor(t *testing.T) {
	capture := `Bash command

  Permission required

  1. Yes
  2. Yes, and don't ask again
  3. No`
	got := claudeio.DetectAttention(capture)
	if !got.Waiting {
		t.Fatalf("expected Waiting=true, got %+v", got)
	}
}
