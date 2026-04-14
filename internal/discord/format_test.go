package discord

import (
	"database/sql"
	"testing"
	"time"

	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/pkg/transcript"
)

func TestStatusEmoji(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"running", "\U0001F7E2"},
		{"idle", "\u26AA"},
		{"waiting", "\U0001F7E0"},
		{"error", "\U0001F534"},
		{"stopped", "\u26AB"},
		{"unknown", "\u26AB"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := statusEmoji(tt.status)
			if got != tt.want {
				t.Errorf("statusEmoji(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestEmbedColor(t *testing.T) {
	tests := []struct {
		status string
		want   int
	}{
		{"running", 0x2ECC71},
		{"idle", 0x95A5A6},
		{"waiting", 0xF39C12},
		{"error", 0xE74C3C},
		{"stopped", 0x2C3E50},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := embedColor(tt.status)
			if got != tt.want {
				t.Errorf("embedColor(%q) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

func TestFormatSessionList_Empty(t *testing.T) {
	embed := formatSessionList(nil)
	if embed.Title != "Mosaic Sessions" {
		t.Errorf("title = %q, want %q", embed.Title, "Mosaic Sessions")
	}
	if embed.Description != "No active sessions." {
		t.Errorf("description = %q", embed.Description)
	}
}

func TestFormatSessionList_WithSessions(t *testing.T) {
	sessions := []store.Session{
		{
			Name:         "feature-auth",
			Status:       "running",
			Branch:       sql.NullString{String: "feature/auth", Valid: true},
			LastActiveAt: time.Now().Add(-2 * time.Minute),
		},
		{
			Name:         "bugfix-css",
			Status:       "idle",
			LastActiveAt: time.Now().Add(-15 * time.Minute),
		},
	}
	embed := formatSessionList(sessions)
	if embed.Title != "Mosaic Sessions (2)" {
		t.Errorf("title = %q", embed.Title)
	}
	if embed.Description == "" {
		t.Error("description should not be empty")
	}
}

func TestFormatSessionDetail(t *testing.T) {
	s := store.Session{
		Name:        "test-session",
		Status:      "running",
		Tool:        "claude",
		ProjectPath: "/home/user/project",
		Branch:      sql.NullString{String: "main", Valid: true},
	}
	subs := []store.Subagent{
		{
			SubagentType: "code-reviewer",
			Status:       "running",
			CurrentTool:  sql.NullString{String: "Read", Valid: true},
		},
	}
	embed := formatSessionDetail(s, subs)
	if embed.Title == "" {
		t.Error("title should not be empty")
	}
	if len(embed.Fields) == 0 {
		t.Error("should have fields")
	}
}

func TestFormatToolStart(t *testing.T) {
	block := transcript.ContentBlock{
		Type: "tool_use",
		Name: "Edit",
	}
	got := formatToolStart(block)
	if got != "\U0001F527 **Edit**" {
		t.Errorf("formatToolStart = %q", got)
	}
}

func TestFormatToolResult(t *testing.T) {
	got := formatToolResult(transcript.ContentBlock{IsError: false})
	if got != "\u2705 Done" {
		t.Errorf("formatToolResult(ok) = %q", got)
	}

	got = formatToolResult(transcript.ContentBlock{IsError: true})
	if got != "\u274C Error" {
		t.Errorf("formatToolResult(err) = %q", got)
	}
}

func TestFormatAssistantText(t *testing.T) {
	got := formatAssistantText("")
	if got != "" {
		t.Errorf("empty text should return empty, got %q", got)
	}

	got = formatAssistantText("Hello")
	if got != "Hello" {
		t.Errorf("short text = %q", got)
	}

	long := make([]rune, 2000)
	for i := range long {
		long[i] = 'a'
	}
	got = formatAssistantText(string(long))
	runes := []rune(got)
	// 1900 runes + "\n..." = 1904 runes
	if len(runes) > 1910 {
		t.Errorf("long text should be truncated, got %d runes", len(runes))
	}

	// Test UTF-8 safety: multi-byte characters should not be split.
	jp := make([]rune, 2000)
	for i := range jp {
		jp[i] = '\u3042' // hiragana 'a' (3 bytes in UTF-8)
	}
	got = formatAssistantText(string(jp))
	runes = []rune(got)
	if len(runes) > 1910 {
		t.Errorf("Japanese text should be truncated, got %d runes", len(runes))
	}
}

func TestTimeAgo(t *testing.T) {
	if timeAgo(time.Time{}) != "never" {
		t.Error("zero time should be 'never'")
	}
	if timeAgo(time.Now()) != "just now" {
		t.Error("now should be 'just now'")
	}
	got := timeAgo(time.Now().Add(-5 * time.Minute))
	if got != "5m ago" {
		t.Errorf("5 minutes ago = %q", got)
	}
}
