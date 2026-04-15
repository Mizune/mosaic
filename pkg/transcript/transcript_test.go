package transcript_test

import (
	"strings"
	"testing"

	"github.com/Mizune/mosaic/pkg/transcript"
)

func TestParseLine_Assistant_ToolUse(t *testing.T) {
	const line = `{"type":"assistant","uuid":"abc","sessionId":"sid","timestamp":"2026-04-07T12:00:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_x","name":"Bash","input":{"command":"ls"}}]}}`
	e, err := transcript.ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if e.Type != transcript.EntryAssistant {
		t.Fatalf("type = %v, want assistant", e.Type)
	}
	if e.SessionID != "sid" {
		t.Fatalf("sessionId = %q", e.SessionID)
	}
	if e.Message == nil || len(e.Message.Content) != 1 {
		t.Fatalf("message missing or wrong length")
	}
	cb := e.Message.Content[0]
	if !cb.IsToolUse() || cb.Name != "Bash" || cb.ID != "toolu_x" {
		t.Fatalf("unexpected content block: %+v", cb)
	}
}

func TestParseLine_User_ToolResult(t *testing.T) {
	const line = `{"type":"user","sessionId":"sid","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_x","content":"ok"}]}}`
	e, err := transcript.ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if e.Type != transcript.EntryUser {
		t.Fatalf("type = %v", e.Type)
	}
	cb := e.Message.Content[0]
	if !cb.IsToolResult() || cb.ToolUseID != "toolu_x" {
		t.Fatalf("unexpected: %+v", cb)
	}
}

func TestDecoder_StreamingMultipleLines(t *testing.T) {
	body := strings.Join([]string{
		`{"type":"assistant","uuid":"a","sessionId":"sid"}`,
		`{"type":"user","uuid":"b","sessionId":"sid"}`,
		``, // blank line should be skipped
		`{"type":"system","uuid":"c","sessionId":"sid"}`,
	}, "\n") + "\n"

	d := transcript.NewDecoder(strings.NewReader(body))
	want := []transcript.EntryType{
		transcript.EntryAssistant,
		transcript.EntryUser,
		transcript.EntrySystem,
	}
	for i, w := range want {
		e, err := d.Next()
		if err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
		if e.Type != w {
			t.Fatalf("entry %d type = %v, want %v", i, e.Type, w)
		}
	}
}

func TestDecoder_MalformedJSON_SkipsLine(t *testing.T) {
	body := strings.Join([]string{
		`{"type":"assistant","uuid":"a","sessionId":"sid"}`,
		`{this is not valid json}`,
		`{"type":"user","uuid":"b","sessionId":"sid"}`,
	}, "\n") + "\n"

	d := transcript.NewDecoder(strings.NewReader(body))

	e1, err := d.Next()
	if err != nil {
		t.Fatalf("entry 1: %v", err)
	}
	if e1.Type != transcript.EntryAssistant {
		t.Fatalf("entry 1 type = %v, want assistant", e1.Type)
	}

	// The malformed line should be silently skipped.
	e2, err := d.Next()
	if err != nil {
		t.Fatalf("entry 2: %v", err)
	}
	if e2.Type != transcript.EntryUser {
		t.Fatalf("entry 2 type = %v, want user", e2.Type)
	}
}

func TestDecoder_OversizedLine_DoesNotSpin(t *testing.T) {
	// Build a line that exceeds the 16 MiB scanner buffer.
	oversized := `{"type":"assistant","data":"` + strings.Repeat("x", 17*1024*1024) + `"}`
	body := strings.Join([]string{
		`{"type":"user","uuid":"a","sessionId":"sid"}`,
		oversized,
	}, "\n") + "\n"

	d := transcript.NewDecoder(strings.NewReader(body))

	// First valid entry should be returned.
	e, err := d.Next()
	if err != nil {
		t.Fatalf("first entry: %v", err)
	}
	if e.Type != transcript.EntryUser {
		t.Fatalf("type = %v, want user", e.Type)
	}

	// Next call must not hang — it should return a non-EOF error
	// (bufio.ErrTooLong) because the oversized line exceeds the buffer.
	_, err = d.Next()
	if err == nil {
		t.Fatal("expected error for oversized line, got nil")
	}
}

func TestDecoder_LargeLine_8MB(t *testing.T) {
	// An 8 MiB line is within the 16 MiB buffer and should parse fine.
	payload := strings.Repeat("y", 8*1024*1024)
	line := `{"type":"assistant","uuid":"a","sessionId":"sid","data":"` + payload + `"}`
	body := line + "\n"

	d := transcript.NewDecoder(strings.NewReader(body))
	e, err := d.Next()
	if err != nil {
		t.Fatalf("parse 8MB line: %v", err)
	}
	if e.Type != transcript.EntryAssistant {
		t.Fatalf("type = %v, want assistant", e.Type)
	}
}

func TestParseSubAgentInput(t *testing.T) {
	const raw = `{"description":"refactor","subagent_type":"general-purpose","prompt":"do thing","run_in_background":true}`
	in, err := transcript.ParseSubAgentInput([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if in.SubagentType != "general-purpose" {
		t.Fatalf("subagent_type = %q", in.SubagentType)
	}
	if !in.RunInBackground {
		t.Fatalf("run_in_background should be true")
	}
}
