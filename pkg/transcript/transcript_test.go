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
