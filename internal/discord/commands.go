package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/Mizune/mosaic/internal/service"
)

// slashCommands defines the Discord slash commands the bridge
// registers. They are created in the configured guild (or globally
// when GuildID is empty) during [Bridge.Start].
var slashCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "mosaic-list",
		Description: "List all mosaic sessions",
	},
	{
		Name:        "mosaic-status",
		Description: "Show detailed status for a session",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "name",
				Description:  "Session name",
				Required:     true,
				Autocomplete: true,
			},
		},
	},
	{
		Name:        "mosaic-kill",
		Description: "Stop a session",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "name",
				Description:  "Session name",
				Required:     true,
				Autocomplete: true,
			},
		},
	},
	{
		Name:        "mosaic-send",
		Description: "Send a message to a session agent",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "session",
				Description:  "Session name",
				Required:     true,
				Autocomplete: true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "message",
				Description: "Message to send",
				Required:    true,
			},
		},
	},
}

// registerCommands creates the slash commands in Discord and stores
// their IDs for cleanup in [Bridge.deregisterCommands].
func (br *Bridge) registerCommands() {
	if br.dg == nil {
		return
	}
	for _, cmd := range slashCommands {
		created, err := br.dg.ApplicationCommandCreate(
			br.dg.State.User.ID, br.cfg.GuildID, cmd,
		)
		if err != nil {
			br.log.Warn("register command", "name", cmd.Name, "err", err)
			continue
		}
		br.commandIDs = append(br.commandIDs, created.ID)
	}
}

// deregisterCommands removes the slash commands registered during Start.
func (br *Bridge) deregisterCommands() {
	if br.dg == nil {
		return
	}
	for _, id := range br.commandIDs {
		if err := br.dg.ApplicationCommandDelete(
			br.dg.State.User.ID, br.cfg.GuildID, id,
		); err != nil {
			br.log.Warn("deregister command", "id", id, "err", err)
		}
	}
	br.commandIDs = nil
}

// routeInteraction dispatches slash command interactions and message
// component interactions (buttons) to the appropriate handler.
func (br *Bridge) routeInteraction(i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommandAutocomplete:
		br.handleAutocomplete(i)
	case discordgo.InteractionApplicationCommand:
		switch i.ApplicationCommandData().Name {
		case "mosaic-list":
			br.cmdList(i)
		case "mosaic-status":
			br.cmdStatus(i)
		case "mosaic-kill":
			br.cmdKill(i)
		case "mosaic-send":
			br.cmdSend(i)
		}
	case discordgo.InteractionMessageComponent:
		br.handleComponent(i)
	}
}

// cmdList handles /mosaic-list.
func (br *Bridge) cmdList(i *discordgo.InteractionCreate) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	sessions, err := br.store.Sessions().List(ctx)
	if err != nil {
		br.log.Warn("list sessions failed", "err", err)
		br.respondEphemeral(i, "\u274C Failed to list sessions")
		return
	}

	embed := formatSessionList(sessions)
	br.respondEmbed(i, embed)
}

// cmdStatus handles /mosaic-status <name>.
func (br *Bridge) cmdStatus(i *discordgo.InteractionCreate) {
	opts := i.ApplicationCommandData().Options
	if len(opts) == 0 {
		br.respondEphemeral(i, "\u274C Missing required option: name")
		return
	}
	name := opts[0].StringValue()

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	session, err := br.store.Sessions().GetByName(ctx, name)
	if err != nil {
		br.respondEphemeral(i, "\u274C Session '"+name+"' not found")
		return
	}

	subs, _ := br.store.Subagents().ListByParent(ctx, session.ID)
	embed := formatSessionDetail(session, subs)
	br.respondEmbed(i, embed)
}

// cmdKill handles /mosaic-kill <name>.
func (br *Bridge) cmdKill(i *discordgo.InteractionCreate) {
	if !br.isAuthorized(i) {
		br.respondEphemeral(i, "\u274C You are not authorized to kill sessions")
		return
	}

	opts := i.ApplicationCommandData().Options
	if len(opts) == 0 {
		br.respondEphemeral(i, "\u274C Missing required option: name")
		return
	}
	name := opts[0].StringValue()

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	if err := br.sessions.Kill(ctx, name, service.KillOptions{}); err != nil {
		br.log.Warn("kill session failed", "name", name, "err", err)
		br.respondEphemeral(i, "\u274C Failed to kill '"+name+"'")
		return
	}

	br.respondEphemeral(i, "\u2705 Session '"+name+"' killed")
}

// cmdSend handles /mosaic-send <session> <message>.
func (br *Bridge) cmdSend(i *discordgo.InteractionCreate) {
	if !br.isAuthorized(i) {
		br.respondEphemeral(i, "\u274C You are not authorized to send messages")
		return
	}

	opts := i.ApplicationCommandData().Options
	if len(opts) < 2 {
		br.respondEphemeral(i, "\u274C Missing required options: session and message")
		return
	}
	sessionName := opts[0].StringValue()
	message := opts[1].StringValue()

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	if _, err := br.messaging.Post(ctx, service.PostInput{
		ToSession: sessionName,
		Kind:      "discord",
		Body:      message,
	}); err != nil {
		br.log.Warn("send message failed", "session", sessionName, "err", err)
		br.respondEphemeral(i, "\u274C Failed to send to '"+sessionName+"'")
		return
	}

	br.respondEphemeral(i, "\u2705 Message sent to '"+sessionName+"'")
}

// handleAutocomplete provides session name suggestions.
func (br *Bridge) handleAutocomplete(i *discordgo.InteractionCreate) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	sessions, err := br.store.Sessions().List(ctx)
	if err != nil {
		return
	}

	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, s := range sessions {
		if len(choices) >= 25 {
			break
		}
		label := fmt.Sprintf("%s %s (%s)", statusEmoji(s.Status), s.Name, s.Status)
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  label,
			Value: s.Name,
		})
	}

	_ = br.dg.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	})
}

// handleComponent dispatches button clicks from permission
// notification embeds. Custom IDs have the form "mosaic:<action>:<sessionID>".
// Session IDs are ULIDs and never contain colons, so SplitN(_, ":", 3) is safe.
func (br *Bridge) handleComponent(i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	parts := strings.SplitN(customID, ":", 3)
	if len(parts) != 3 || parts[0] != customIDPrefix {
		return
	}

	action := parts[1]
	sessionID := parts[2]

	// Deduplicate: if this message was already handled (e.g. a
	// concurrent click), acknowledge Discord but do nothing else.
	msgID := i.Message.ID
	br.mu.Lock()
	if _, already := br.handledInteractions[msgID]; already {
		br.mu.Unlock()
		br.respondEphemeral(i, "Already handled")
		return
	}
	br.handledInteractions[msgID] = struct{}{}
	br.mu.Unlock()

	if !br.isAuthorized(i) {
		br.respondEphemeral(i, "\u274C You are not authorized to respond to permission requests")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	session, err := br.store.Sessions().GetByID(ctx, sessionID)
	if err != nil {
		br.respondEphemeral(i, "\u274C Session not found")
		return
	}

	var keys []string
	var label string
	switch action {
	case actionApprove:
		// The TUI prompt defaults the cursor to "1. Yes",
		// so a bare Enter confirms it.
		keys = []string{enterKey}
		label = "\u2705 Approved"
	case actionDeny:
		// Menu: 1. Yes, 2. Yes (always), 3. No — move down twice, then confirm.
		keys = []string{"Down", "Down", enterKey}
		label = "\u274C Denied"
	default:
		return
	}

	for _, k := range keys {
		if err := br.tmux.SendKeys(ctx, session.TmuxSession, k, true); err != nil {
			br.log.Warn("permission response", "session", session.Name, "action", action, "err", err)
			br.respondEphemeral(i, "\u274C Failed to send response to session")
			return
		}
	}

	// Update the original message to show the decision and remove buttons.
	var embeds []*discordgo.MessageEmbed
	if i.Message != nil && len(i.Message.Embeds) > 0 {
		embeds = i.Message.Embeds
		if action == actionApprove {
			embeds[0].Color = colorGreen
		} else {
			embeds[0].Color = colorRed
		}
		embeds[0].Footer = &discordgo.MessageEmbedFooter{
			Text: label,
		}
	}
	if err := br.dg.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     embeds,
			Components: []discordgo.MessageComponent{}, // remove buttons
		},
	}); err != nil {
		br.log.Warn("update interaction", "err", err)
	}
}

// respondEphemeral sends an ephemeral text response.
func (br *Bridge) respondEphemeral(i *discordgo.InteractionCreate, content string) {
	_ = br.dg.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// respondEmbed sends an embed response.
func (br *Bridge) respondEmbed(i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	_ = br.dg.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}
