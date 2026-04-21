package main

import (
	"fmt"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/spf13/cobra"
)

// newKillCmd builds `mosaic kill <name>`.
func newKillCmd() *cobra.Command {
	var opts runtimeOptions
	var cleanupWorktree bool

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

			killOpts := service.KillOptions{}
			if cmd.Flags().Changed("cleanup-worktree") {
				killOpts.CleanupWorktree = config.BoolPtr(cleanupWorktree)
			}

			if err := rt.sessions.Kill(cmd.Context(), args[0], killOpts); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "killed session %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&cleanupWorktree, "cleanup-worktree", false,
		"Also remove the git worktree from disk")
	addRuntimeFlags(cmd, &opts)
	return cmd
}
