package features

import (
	"time"

	"github.com/Mizune/mosaic/pkg/transcript"
)

// buildToolUseEntry constructs a transcript.Entry equivalent to one
// `assistant` line carrying a single tool_use block. It is used by the
// state-machine Gherkin steps.
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

// buildToolResultEntry constructs a transcript.Entry equivalent to a
// `user` line carrying the matching tool_result.
func buildToolResultEntry(id string) transcript.Entry {
	return transcript.Entry{
		Type:      transcript.EntryUser,
		Timestamp: time.Now(),
		Message: &transcript.Message{
			Role: "user",
			Content: transcript.ContentList{
				{Type: "tool_result", ToolUseID: id, Content: []byte(`"ok"`)},
			},
		},
	}
}
