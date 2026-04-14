package discord

import (
	"context"
	"fmt"

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

// routeInteraction dispatches slash command interactions to the
// appropriate handler.
func (br *Bridge) routeInteraction(i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionApplicationCommandAutocomplete {
		br.handleAutocomplete(i)
		return
	}
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

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

	if err := br.sessions.Kill(ctx, name); err != nil {
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
