package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/service"
)

// newSendCmd builds `mosaic send --to <session> [--from <session>]
// [--kind note] <body>...`. The message is stored in the DB
// immediately; delivery (typing it into the target tmux pane)
// happens the next time the messaging loop ticks on a running
// mosaic instance.
//
// Users who run `mosaic send` while no mosaic TUI is active can
// still send safely: the message sits in the store and is delivered
// whenever the recipient session's tmux pane is next available.
func newSendCmd() *cobra.Command {
	var (
		opts     runtimeOptions
		to, from string
		kind     string
	)
	cmd := &cobra.Command{
		Use:   "send <message...>",
		Short: "Send a message from one mosaic session to another",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if to == "" {
				return errors.New("--to is required")
			}
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			msg := strings.Join(args, " ")
			svc := service.NewMessagingService(rt.store, rt.tmux, rt.bus, 0)
			posted, err := svc.Post(cmd.Context(), service.PostInput{
				ToSession:   to,
				FromSession: from,
				Kind:        kind,
				Body:        msg,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "queued message #%d to %s (%s)\n",
				posted.ID, to, kind)
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "Recipient session name (required)")
	cmd.Flags().StringVar(&from, "from", "", "Sender session name (optional)")
	cmd.Flags().StringVar(&kind, "kind", "note", "Message kind label")
	addRuntimeFlags(cmd, &opts)
	return cmd
}
