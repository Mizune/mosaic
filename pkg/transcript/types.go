// Package transcript exposes the JSONL line schema that Claude Code
// writes to ~/.claude/projects/<sanitized>/<sessionId>.jsonl. It is a
// stable surface area: every other mosaic package consumes these
// types via the `pkg/` import path so external tools (and tests) can
// build on the same definitions.
//
// The schema was reverse-engineered from real Claude Code transcripts
// shipped with version 2.1.87. Newer versions may add fields; the
// types below are deliberately tolerant — unknown JSON fields are
// silently ignored, and every field is a pointer / optional value
// where the source schema permits it.
package transcript

import (
	"encoding/json"
	"errors"
	"time"
)

// EntryType is the value of the top-level "type" field.
type EntryType string

const (
	EntryUser                EntryType = "user"
	EntryAssistant           EntryType = "assistant"
	EntrySystem              EntryType = "system"
	EntryFileHistorySnapshot EntryType = "file-history-snapshot"
	EntryQueueOperation      EntryType = "queue-operation"
	EntryAgentName           EntryType = "agent-name"
	EntryCustomTitle         EntryType = "custom-title"
)

// Entry is one parsed JSONL line.
type Entry struct {
	Type      EntryType       `json:"type"`
	UUID      string          `json:"uuid"`
	ParentUUID string         `json:"parentUuid"`
	SessionID string          `json:"sessionId"`
	AgentID   string          `json:"agentId"`
	IsSidechain bool          `json:"isSidechain"`
	CWD       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Timestamp time.Time       `json:"timestamp"`
	Version   string          `json:"version"`
	Slug      string          `json:"slug"`
	Message   *Message        `json:"message,omitempty"`
	// Raw is the original JSON line so callers can inspect fields the
	// strongly typed struct ignored.
	Raw json.RawMessage `json:"-"`
}

// Message is the assistant/user payload.
//
// Claude Code emits Content as either a JSON array of typed blocks
// (the assistant case, and the structured tool_result case) OR as a
// plain string (the simple user prompt case). [ContentList] is a
// custom-unmarshalling slice that accepts both shapes; a string body
// is exposed as a single text block so the rest of mosaic only ever
// sees blocks.
type Message struct {
	Role    string      `json:"role"`
	Model   string      `json:"model"`
	Content ContentList `json:"content"`
}

// ContentList is a slice of [ContentBlock] that knows how to decode
// either a JSON array OR a JSON string, transparently.
type ContentList []ContentBlock

// UnmarshalJSON implements the dual decoding rule.
func (c *ContentList) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*c = nil
		return nil
	}
	switch data[0] {
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*c = ContentList{{Type: "text", Text: s}}
		return nil
	case '[':
		var arr []ContentBlock
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		*c = ContentList(arr)
		return nil
	}
	return errors.New("transcript: content must be a string or an array")
}

// ContentBlock is one entry in Message.Content.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`         // tool_use id
	Name      string          `json:"name,omitempty"`       // tool_use tool name
	Input     json.RawMessage `json:"input,omitempty"`      // tool_use args
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result reference
	Content   json.RawMessage `json:"content,omitempty"`     // tool_result body
	IsError   bool            `json:"is_error,omitempty"`
}

// IsToolUse reports whether the block represents a tool invocation.
func (b ContentBlock) IsToolUse() bool { return b.Type == "tool_use" }

// IsToolResult reports whether the block represents a tool result.
func (b ContentBlock) IsToolResult() bool { return b.Type == "tool_result" }

// IsText reports whether the block is plain assistant text.
func (b ContentBlock) IsText() bool { return b.Type == "text" }

// SubAgentInput is the typed view of the Agent / Task tool's input
// payload. mosaic uses it to extract the sub-agent type and the
// human-readable description for the TUI.
type SubAgentInput struct {
	Description    string `json:"description"`
	SubagentType   string `json:"subagent_type"`
	Prompt         string `json:"prompt"`
	Model          string `json:"model"`
	RunInBackground bool  `json:"run_in_background"`
}

// ParseSubAgentInput decodes a tool_use block whose name is "Agent"
// (a.k.a. the Task tool) into a SubAgentInput.
func ParseSubAgentInput(raw json.RawMessage) (SubAgentInput, error) {
	var v SubAgentInput
	if len(raw) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, err
	}
	return v, nil
}
