package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/git"
	"github.com/Mizune/mosaic/internal/store"
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

			row, err := resolveDiffSession(cmd.Context(), rt, args)
			if err != nil {
				return err
			}

			gc := git.New()
			base := rt.config.Worktree.DefaultBaseBranch
			if base == "" {
				base = "main"
			}
			resolved, rerr := gc.ResolveDiffBase(cmd.Context(),
				row.WorktreePath, base)
			if rerr != nil {
				return fmt.Errorf("resolve diff base: %w", rerr)
			}

			// `git diff <base>` (no `..`/`...`) compares the
			// resolved base directly to the working tree, so
			// uncommitted edits the agent just made show up
			// alongside any branch commits. `--cached` switches
			// to staged-only when the user wants to preview
			// what would be committed next.
			gitArgs := []string{"-C", row.WorktreePath, "diff"}
			if staged {
				gitArgs = append(gitArgs, "--cached")
			}
			gitArgs = append(gitArgs, resolved)

			child := exec.CommandContext(cmd.Context(), "git", gitArgs...)
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

// resolveDiffSession picks the session a `mosaic diff` invocation
// should target. The lookup is intentionally permissive: most users
// will run the command from inside a pane mosaic spawned, so the env
// var path is the common case.
func resolveDiffSession(ctx context.Context, rt *runtime, args []string) (store.Session, error) {
	if len(args) == 1 {
		row, err := rt.store.Sessions().GetByName(ctx, args[0])
		if err != nil {
			return store.Session{}, fmt.Errorf("session %q: %w", args[0], err)
		}
		return row, nil
	}
	if id := os.Getenv("MOSAIC_WORKTREE_ID"); id != "" {
		row, err := rt.store.Sessions().GetByID(ctx, id)
		if err != nil {
			return store.Session{}, fmt.Errorf(
				"resolve session from MOSAIC_WORKTREE_ID=%s: %w", id, err)
		}
		return row, nil
	}
	return store.Session{}, errors.New(
		"no session specified — pass <name> or run from inside a mosaic pane (sets MOSAIC_WORKTREE_ID)")
}
