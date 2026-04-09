package live_test

import (
	"time"

	"github.com/Mizune/mosaic/pkg/transcript"
)

// buildToolUseEntry constructs a transcript.Entry equivalent to one
// `assistant` line carrying a single tool_use block. It is shared by
// the live package's tests.
func buildToolUseEntry(id, name string) transcript.Entry {
	return transcript.Entry{
		Type:      transcript.EntryAssistant,
		Timestamp: time.Now(),
		Message: &transcript.Message{
			Role: "assistant",
			Content: transcript.ContentList{
				{Type: "tool_use", ID: id, Name: name},
			},
		},
	}
}

// buildToolResultEntry constructs a user-role transcript entry that
// carries a tool_result block matching the given tool_use id. The
// state machine treats this as the "tool finished" signal.
func buildToolResultEntry(toolUseID string) transcript.Entry {
	return transcript.Entry{
		Type:      transcript.EntryUser,
		Timestamp: time.Now(),
		Message: &transcript.Message{
			Role: "user",
			Content: transcript.ContentList{
				{Type: "tool_result", ToolUseID: toolUseID},
			},
		},
	}
}
