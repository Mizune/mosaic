package claudeio_test

import (
	"testing"

	"github.com/Mizune/mosaic/internal/claudeio"
)

func TestScanOSC_Notify(t *testing.T) {
	capture := "noise\x1b]777;notify;Need input;Approve Bash?\x07trailing"
	got := claudeio.ScanOSC(capture)
	if len(got) != 1 {
		t.Fatalf("want 1 hint, got %d (%+v)", len(got), got)
	}
	h := got[0]
	if h.Kind != claudeio.OscNotify {
		t.Errorf("kind = %v", h.Kind)
	}
	if h.Title != "Need input" || h.Body != "Approve Bash?" {
		t.Errorf("title/body mismatch: %+v", h)
	}
}

func TestScanOSC_NotifyWithST(t *testing.T) {
	// Same payload, but closed with the ST terminator (ESC \)
	// instead of BEL.
	capture := "\x1b]777;notify;Hi;there\x1b\\"
	got := claudeio.ScanOSC(capture)
	if len(got) != 1 || got[0].Title != "Hi" || got[0].Body != "there" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestScanOSC_ProgressError(t *testing.T) {
	capture := "\x1b]9;4;2;42\x07"
	got := claudeio.ScanOSC(capture)
	if len(got) != 1 {
		t.Fatalf("want 1 hint, got %d", len(got))
	}
	h := got[0]
	if h.Kind != claudeio.OscProgress {
		t.Errorf("kind = %v", h.Kind)
	}
	if h.ProgressState != 2 || h.ProgressPercent != 42 {
		t.Errorf("progress mismatch: %+v", h)
	}
}

func TestScanOSC_MultipleAndUnknown(t *testing.T) {
	capture := "\x1b]0;window title\x07pre\x1b]777;notify;A;B\x07mid\x1b]9;4;1;10\x07post"
	got := claudeio.ScanOSC(capture)
	if len(got) != 2 {
		t.Fatalf("want 2 hints, got %d (%+v)", len(got), got)
	}
	if got[0].Kind != claudeio.OscNotify || got[0].Title != "A" {
		t.Errorf("first hint: %+v", got[0])
	}
	if got[1].Kind != claudeio.OscProgress || got[1].ProgressPercent != 10 {
		t.Errorf("second hint: %+v", got[1])
	}
}

func TestScanOSC_UnterminatedIsSafe(t *testing.T) {
	capture := "\x1b]777;notify;truncated before terminator"
	got := claudeio.ScanOSC(capture)
	if len(got) != 0 {
		t.Fatalf("want 0 hints from unterminated OSC, got %+v", got)
	}
}

func TestScanOSC_EmptyReturnsNil(t *testing.T) {
	if got := claudeio.ScanOSC(""); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestOscAttention_NotifyWins(t *testing.T) {
	hints := []claudeio.OscHint{
		{Kind: claudeio.OscProgress, ProgressState: 1, ProgressPercent: 50},
		{Kind: claudeio.OscNotify, Title: "Permission required", Body: "Bash"},
	}
	att := claudeio.OscAttention(hints)
	if !att.Waiting {
		t.Fatalf("expected waiting: %+v", att)
	}
	if att.Title != "Permission required" || att.Body != "Bash" || att.Source != "osc" {
		t.Errorf("attention mismatch: %+v", att)
	}
}

func TestOscAttention_ProgressErrorBecomesAttention(t *testing.T) {
	hints := []claudeio.OscHint{
		{Kind: claudeio.OscProgress, ProgressState: 1, ProgressPercent: 30},
		{Kind: claudeio.OscProgress, ProgressState: 2, ProgressPercent: 30},
	}
	att := claudeio.OscAttention(hints)
	if !att.Waiting {
		t.Fatalf("expected waiting: %+v", att)
	}
	if att.Title != "Error" {
		t.Errorf("title: %q", att.Title)
	}
	if att.Source != "osc" {
		t.Errorf("source: %q", att.Source)
	}
}

func TestOscAttention_NoInterestingHints(t *testing.T) {
	hints := []claudeio.OscHint{
		{Kind: claudeio.OscProgress, ProgressState: 1, ProgressPercent: 50},
	}
	att := claudeio.OscAttention(hints)
	if att.Waiting {
		t.Fatalf("progress updates should not trigger attention: %+v", att)
	}
}

func TestOscAttention_NotifyWithoutTitle(t *testing.T) {
	hints := []claudeio.OscHint{
		{Kind: claudeio.OscNotify, Body: "body only"},
	}
	att := claudeio.OscAttention(hints)
	if att.Title != "Notification" {
		t.Errorf("fallback title missing: %+v", att)
	}
}
