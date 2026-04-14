package discord

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/internal/hook"
)

const queryTimeout = 2 * time.Second

// handle is the bus event router. It is invoked on the bus's
// dispatch goroutine, so all work should be fast or fire-and-forget.
func (br *Bridge) handle(e bus.Event) {
	switch v := e.(type) {
	case bus.SessionStarted:
		br.handleSessionStarted(v)
	case bus.SessionStopped:
		br.handleSessionStopped(v)
	case claudeio.TranscriptEntry:
		br.handleTranscript(v)
	case hook.NotificationEvent:
		br.handleNotification(v)
	}
}

// handleSessionStarted creates a Discord thread for the new session.
func (br *Bridge) handleSessionStarted(ev bus.SessionStarted) {
	if br.dg == nil || br.cfg.ChannelID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	session, err := br.store.Sessions().GetByID(ctx, ev.SessionID)
	if err != nil {
		br.log.Warn("session started: lookup failed", "id", ev.SessionID, "err", err)
		return
	}

	// Create a thread in the parent channel. Discord limits thread
	// names to 100 characters; truncate by rune to avoid splitting
	// multi-byte characters.
	threadName := fmt.Sprintf("\U0001F7E2 %s", session.Name)
	if runes := []rune(threadName); len(runes) > 100 {
		threadName = string(runes[:100])
	}

	thread, err := br.dg.ThreadStartComplex(br.cfg.ChannelID, &discordgo.ThreadStart{
		Name:                threadName,
		AutoArchiveDuration: 1440, // 24 hours
		Type:                discordgo.ChannelTypeGuildPublicThread,
	})
	if err != nil {
		br.log.Warn("create thread", "session", session.Name, "err", err)
		return
	}

	br.setThread(session.ID, thread.ID)

	// Post a welcome message with session details.
	branch := ""
	if session.Branch.Valid && session.Branch.String != "" {
		branch = fmt.Sprintf(" | `%s`", session.Branch.String)
	}
	welcome := fmt.Sprintf("\U0001F7E2 **Session started** — %s%s\nSend messages here to interact with this agent.",
		session.Tool, branch)
	br.sendToThread(thread.ID, welcome)

	br.log.Info("thread created", "session", session.Name, "thread", thread.ID)
}

// handleSessionStopped posts a close message to the session thread.
func (br *Bridge) handleSessionStopped(ev bus.SessionStopped) {
	threadID := br.threadFor(ev.SessionID)
	if threadID == "" {
		return
	}

	if br.cfg.NotifyOnStop {
		br.sendToThread(threadID, "\u26AB **Session stopped**")
	}

	// Archive the thread and clean up maps.
	if br.dg != nil {
		archived := true
		if _, err := br.dg.ChannelEditComplex(threadID, &discordgo.ChannelEdit{
			Archived: &archived,
		}); err != nil {
			br.log.Warn("archive thread", "thread", threadID, "err", err)
		}
	}
	br.removeThread(ev.SessionID)
}

// handleNotification forwards blocking notifications (permission
// prompts, approval requests) to the session thread.
func (br *Bridge) handleNotification(ev hook.NotificationEvent) {
	if !br.cfg.NotifyOnAttention {
		return
	}
	if !hook.IsBlockingNotification(ev) {
		return
	}

	// Look up the session by worktree ID (hook events carry this).
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	session, err := br.store.Sessions().GetByID(ctx, ev.WorktreeID)
	if err != nil {
		return
	}

	threadID := br.threadFor(session.ID)
	if threadID == "" {
		return
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("\U0001F7E0 %s", ev.Title),
		Description: ev.Body,
		Color:       0xF39C12, // orange
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Reply in this thread to respond",
		},
	}
	br.sendEmbedToThread(threadID, embed)
}
