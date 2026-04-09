package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newKillCmd builds `mosaic kill <name>`.
func newKillCmd() *cobra.Command {
	var opts runtimeOptions

	cmd := &cobra.Command{
		Use:     "kill <name>",
		Aliases: []string{"rm"},
		Short:   "Kill the tmux session and remove the mosaic row",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			if err := rt.sessions.Kill(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "killed session %s\n", args[0])
			return nil
		},
	}
	addRuntimeFlags(cmd, &opts)
	return cmd
}
