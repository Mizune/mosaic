package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/spf13/cobra"
)

func newDiscordCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "discord",
		Short: "Discord bridge diagnostics",
	}

	statusOpts := runtimeOptions{}
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show Discord bridge connection status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := openRuntime(cmd.Context(), statusOpts)
			if err != nil {
				return err
			}
			defer rt.Close()

			cfg := rt.config.Discord
			if !cfg.Enabled {
				fmt.Println("Discord bridge: disabled")
				fmt.Println("Enable with [discord] enabled = true in config.toml")
				return nil
			}

			fmt.Println("Discord bridge: enabled")
			fmt.Printf("  Channel ID: %s\n", cfg.ChannelID)
			fmt.Printf("  Guild ID:   %s\n", cfg.GuildID)
			fmt.Printf("  Notify on stop:      %v\n", cfg.NotifyOnStop)
			fmt.Printf("  Notify on attention: %v\n", cfg.NotifyOnAttention)

			token, fromConfig := resolveDiscordToken(cfg)
			switch {
			case token == "":
				fmt.Println("  Token: not set (check MOSAIC_DISCORD_TOKEN env var)")
			case fromConfig:
				fmt.Println("  Token: set (from config — prefer env var)")
			default:
				fmt.Println("  Token: set (from env var)")
			}

			if len(cfg.AllowedUserIDs) > 0 {
				fmt.Printf("  Allowed users: %v\n", cfg.AllowedUserIDs)
			} else {
				fmt.Println("  Allowed users: all (no restriction)")
			}

			return nil
		},
	}
	addRuntimeFlags(statusCmd, &statusOpts)
	root.AddCommand(statusCmd)

	testOpts := runtimeOptions{}
	testCmd := &cobra.Command{
		Use:   "test",
		Short: "Send a test message to the configured Discord channel",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := openRuntime(cmd.Context(), testOpts)
			if err != nil {
				return err
			}
			defer rt.Close()

			cfg := rt.config.Discord
			if !cfg.Enabled || cfg.ChannelID == "" {
				return fmt.Errorf("discord bridge is not configured (enable and set channel_id)")
			}

			token, _ := resolveDiscordToken(cfg)
			if token == "" {
				return fmt.Errorf("no bot token (set MOSAIC_DISCORD_TOKEN env var)")
			}

			dg, err := discordgo.New("Bot " + token)
			if err != nil {
				return fmt.Errorf("create discord session: %w", err)
			}

			if err := dg.Open(); err != nil {
				return fmt.Errorf("connect to discord: %w", err)
			}
			defer dg.Close()

			_, err = dg.ChannelMessageSend(cfg.ChannelID,
				"\U0001F9EA **Mosaic Discord Bridge** — test message. Connection OK!")
			if err != nil {
				return fmt.Errorf("send test message: %w", err)
			}

			fmt.Println("Test message sent successfully!")
			return nil
		},
	}
	addRuntimeFlags(testCmd, &testOpts)
	root.AddCommand(testCmd)

	return root
}
