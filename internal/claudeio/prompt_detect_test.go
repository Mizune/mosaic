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
