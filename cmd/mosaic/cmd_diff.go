package main

import (
	"os"

	"github.com/spf13/cobra"
)

// newDiffCmd builds `mosaic diff [<name>]`. It is a thin wrapper
// around `git diff <base>...` for the session's worktree, intended to
// be run from inside an attached tmux pane (e.g. via prefix+D) so the
// user can see what the agent has changed without leaving the
// session.
//
// Session resolution order:
//
//  1. positional <name> if supplied;
//  2. MOSAIC_WORKTREE_ID env var (set by mosaic on every spawned
//     pane), looked up by ULID;
//  3. error.
//
// Stdin/stdout/stderr are wired straight to the child git process so
// git's own TTY detection drives color and pager (less) invocation.
// That keeps the command useful both interactively (`mosaic diff` in
// a split pane → paged colored diff) and in scripts (`mosaic diff
// foo > patch.diff` → plain unified diff).
func newDiffCmd() *cobra.Command {
	var (
		opts   runtimeOptions
		staged bool
	)

	cmd := &cobra.Command{
		Use:   "diff [<name>]",
		Short: "Show `git diff` for a session's worktree against its base branch",
		Long: `Run git diff inside a session's worktree comparing the working tree to
the configured diff base (typically main). When invoked from inside a
mosaic-spawned tmux pane, the session is auto-resolved from the
MOSAIC_WORKTREE_ID env var, so a bare ` + "`mosaic diff`" + ` works.

The output is streamed through git unmodified, so color/pager handling
matches what you'd get from running git diff yourself.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			var name string
			if len(args) == 1 {
				name = args[0]
			}
			row, err := rt.sessions.ResolveSession(
				cmd.Context(), name, os.Getenv("MOSAIC_WORKTREE_ID"))
			if err != nil {
				return err
			}

			child, err := rt.sessions.DiffCommand(cmd.Context(), row, staged)
			if err != nil {
				return err
			}
			child.Stdin = os.Stdin
			child.Stdout = cmd.OutOrStdout()
			child.Stderr = cmd.ErrOrStderr()
			return child.Run()
		},
	}
	cmd.Flags().BoolVar(&staged, "staged", false,
		"Show only staged changes (git diff --cached) instead of working-tree diff")
	addRuntimeFlags(cmd, &opts)
	return cmd
}
