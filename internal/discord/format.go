// Package discord implements a bidirectional bridge between mosaic
// and Discord. It creates per-session threads, streams transcript
// events, and handles slash commands for remote session management.
package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/pkg/transcript"
)

// Embed colors used across the Discord package.
const (
	colorGreen  = 0x2ECC71
	colorOrange = 0xF39C12
	colorRed    = 0xE74C3C
)

// Button interaction constants.
const (
	customIDPrefix = "mosaic"
	actionApprove  = "approve"
	actionDeny     = "deny"
)

// enterKey is the empty string sentinel passed to tmux.SendKeys to
// press Enter without preceding text input.
const enterKey = ""

// discordSubagentLabel builds a label for a sub-agent, combining
// type and description when both are available.
// Format: "[Explore] search for auth patterns" (bracket notation).
// See also subagentLabel in internal/tui/views.go which uses
// colon-separated notation instead.
func discordSubagentLabel(sub store.Subagent) string {
	hasType, typ, desc := sub.LabelParts()
	switch {
	case hasType && desc != "":
		return "[" + typ + "] " + desc
	case desc != "":
		return desc
	default:
		return typ
	}
}

// statusEmoji maps a session status string to a Discord-friendly emoji.
func statusEmoji(status string) string {
	switch store.SessionStatus(status) {
	case store.StatusRunning:
		return "\U0001F7E2" // green circle
	case store.StatusIdle:
		return "\u26AA" // white circle
	case store.StatusWaiting:
		return "\U0001F7E0" // orange circle
	case store.StatusError:
		return "\U0001F534" // red circle
	default:
		return "\u26AB" // black circle
	}
}

// embedColor returns a Discord embed color based on session status.
func embedColor(status string) int {
	switch store.SessionStatus(status) {
	case store.StatusRunning:
		return colorGreen
	case store.StatusIdle:
		return 0x95A5A6 // grey
	case store.StatusWaiting:
		return colorOrange
	case store.StatusError:
		return colorRed
	default:
		return 0x2C3E50 // dark
	}
}

// formatSessionList renders a slice of sessions into a Discord embed.
func formatSessionList(sessions []store.Session) *discordgo.MessageEmbed {
	if len(sessions) == 0 {
		return &discordgo.MessageEmbed{
			Title:       "Mosaic Sessions",
			Description: "No active sessions.",
			Color:       0x95A5A6,
		}
	}

	var lines []string
	for _, s := range sessions {
		emoji := statusEmoji(s.Status)
		branch := ""
		if s.Branch.Valid && s.Branch.String != "" {
			branch = fmt.Sprintf(" `%s`", s.Branch.String)
		}
		ago := timeAgo(s.LastActiveAt)
		line := fmt.Sprintf("%s **%s** — %s%s (%s)", emoji, s.Name, s.Status, branch, ago)
		lines = append(lines, line)
	}

	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Mosaic Sessions (%d)", len(sessions)),
		Description: strings.Join(lines, "\n"),
		Color:       0x3498DB,
	}
}

// formatSessionDetail renders a single session with its sub-agents.
func formatSessionDetail(s store.Session, subs []store.Subagent) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{
		{Name: "Status", Value: s.Status, Inline: true},
		{Name: "Tool", Value: s.Tool, Inline: true},
	}
	if s.Branch.Valid && s.Branch.String != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Branch", Value: s.Branch.String, Inline: true,
		})
	}
	fields = append(fields, &discordgo.MessageEmbedField{
		Name: "Project", Value: s.ProjectPath, Inline: false,
	})
	fields = append(fields, &discordgo.MessageEmbedField{
		Name: "Active", Value: timeAgo(s.LastActiveAt), Inline: true,
	})

	if len(subs) > 0 {
		var subLines []string
		for _, sub := range subs {
			emoji := statusEmoji(sub.Status)
			desc := discordSubagentLabel(sub)
			tool := ""
			if sub.CurrentTool.Valid && sub.CurrentTool.String != "" {
				tool = fmt.Sprintf(" — %s", sub.CurrentTool.String)
			}
			subLines = append(subLines, fmt.Sprintf("%s %s%s", emoji, desc, tool))
		}
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:  fmt.Sprintf("Sub-agents (%d)", len(subs)),
			Value: strings.Join(subLines, "\n"),
		})
	}

	return &discordgo.MessageEmbed{
		Title:  fmt.Sprintf("%s %s", statusEmoji(s.Status), s.Name),
		Color:  embedColor(s.Status),
		Fields: fields,
	}
}

// formatToolStart renders a tool_use block for Discord.
func formatToolStart(block transcript.ContentBlock) string {
	name := block.Name
	if name == "" {
		name = "unknown"
	}
	return fmt.Sprintf("\U0001F527 **%s**", name)
}

// formatToolResult renders a tool_result block for Discord.
func formatToolResult(block transcript.ContentBlock) string {
	if block.IsError {
		return "\u274C Error"
	}
	return "\u2705 Done"
}

// formatAssistantText renders an assistant text block, truncated for
// Discord's 2000-char message limit. Truncation is rune-aware to
// avoid splitting multi-byte UTF-8 characters.
func formatAssistantText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	const maxRunes = 1900
	runes := []rune(text)
	if len(runes) > maxRunes {
		text = string(runes[:maxRunes]) + "\n..."
	}
	return text
}

// timeAgo returns a human-readable relative time string.
func timeAgo(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}
