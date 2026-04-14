package discord

import (
	"context"
	"strings"
	"unicode"

	"github.com/bwmarrin/discordgo"

	"github.com/Mizune/mosaic/internal/service"
)

// routeMessage handles messages in session threads. When a user
// posts in a tracked thread, the message body is forwarded to the
// corresponding mosaic session via the messaging service.
func (br *Bridge) routeMessage(m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}

	sessionID := br.sessionForThread(m.ChannelID)
	if sessionID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	session, err := br.store.Sessions().GetByID(ctx, sessionID)
	if err != nil {
		br.log.Warn("route message: session lookup failed",
			"sessionID", sessionID, "err", err)
		return
	}

	body := sanitizeInput(m.Content)
	if body == "" {
		return
	}

	if _, err := br.messaging.Post(ctx, service.PostInput{
		ToSession: session.Name,
		Kind:      "discord",
		Body:      body,
	}); err != nil {
		br.log.Warn("route message: post failed",
			"session", session.Name, "err", err)
		_ = br.dg.MessageReactionAdd(m.ChannelID, m.ID, "\u274C")
		return
	}

	_ = br.dg.MessageReactionAdd(m.ChannelID, m.ID, "\u2705")
}

// sanitizeInput strips control characters that could interfere with
// tmux send-keys. Newlines and tabs are preserved.
func sanitizeInput(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
