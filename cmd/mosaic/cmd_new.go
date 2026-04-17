package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/sandbox"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
)

// newCreateCmd builds `mosaic new <name>`.
func newCreateCmd() *cobra.Command {
	var (
		opts             runtimeOptions
		projectFlag      string
		toolFlag         string
		branchFlag       string
		commandFlag      string
		worktreeFlag     bool
		setupFlag        string
		baseRefFlag      string
		destFlag         string
		continueFlag     bool
		sandboxFlag      string
		sandboxImageFlag string
	)

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new mosaic session backed by a tmux session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			project := projectFlag
			if project == "" {
				project = opts.projectDir
			}
			if project == "" {
				project = "."
			}
			projectAbs, err := filepath.Abs(project)
			if err != nil {
				return err
			}

			tool := store.SessionTool(toolFlag)
			if tool == "" {
				tool = store.SessionTool(rt.config.Mosaic.DefaultTool)
			}

			// auto_worktree config opts the user in by default; the
			// --worktree flag is an explicit override.
			createWorktree := worktreeFlag || rt.config.Worktree.AutoWorktree

			worktreePath := projectAbs
			var setupRecipe = rt.config.Worktree.Setup[setupName(setupFlag, rt.config.Worktree.DefaultSetup)]
			if createWorktree {
				if branchFlag == "" {
					return fmt.Errorf("--branch is required with --worktree")
				}
				dest := destFlag
				if dest == "" {
					dest = filepath.Join(projectAbs, ".worktrees", branchFlag)
				} else if !filepath.IsAbs(dest) {
					dest, err = filepath.Abs(dest)
					if err != nil {
						return err
					}
				}
				worktreePath = dest
			}

			// Sandbox mode is only forwarded when the user set
			// --sandbox explicitly. Leaving it empty lets the
			// service fall back to the per-project opt-in.
			var sandboxMode sandbox.Mode
			if sandboxFlag != "" {
				sandboxMode = sandbox.ParseMode(sandboxFlag)
				// ParseMode falls back to ModeNone for unknown
				// tokens; reject anything except the explicit
				// "none" spelling so typos do not silently
				// disable sandboxing.
				if sandboxMode == sandbox.ModeNone && sandboxFlag != sandbox.ModeNone.String() {
					return fmt.Errorf("unknown --sandbox value %q (expected: %s, %s)",
						sandboxFlag, sandbox.ModeNone, sandbox.ModeDocker)
				}
			}

			row, err := rt.sessions.Create(cmd.Context(), service.CreateInput{
				Name:            args[0],
				ProjectPath:     projectAbs,
				WorktreePath:    worktreePath,
				Branch:          branchFlag,
				Tool:            tool,
				Command:         commandFlag,
				ForceContinue:   continueFlag,
				CreateWorktree:  createWorktree,
				WorktreeRepo:    projectAbs,
				WorktreeBaseRef: baseRefFlag,
				WorktreeSetup:   setupRecipe,
				SandboxMode:     sandboxMode,
				SandboxImage:    sandboxImageFlag,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"created session %s (id=%s, tmux=%s, worktree=%s)\n",
				row.Name, row.ID, row.TmuxSession, row.WorktreePath)
			return nil
		},
	}
	addRuntimeFlags(cmd, &opts)
	cmd.Flags().StringVar(&projectFlag, "path", "", "Source repository / project root")
	cmd.Flags().StringVar(&toolFlag, "tool", "", "Tool to launch (claude, codex, gemini, shell)")
	cmd.Flags().StringVar(&branchFlag, "branch", "", "Branch name (required with --worktree)")
	cmd.Flags().StringVar(&commandFlag, "cmd", "", "Initial command to run in the tmux pane")
	cmd.Flags().BoolVar(&worktreeFlag, "worktree", false, "Create a git worktree for the session")
	cmd.Flags().StringVar(&setupFlag, "setup", "", "Worktree setup recipe to apply (default: project default_setup)")
	cmd.Flags().StringVar(&baseRefFlag, "base", "", "Base ref to branch from (default: current branch)")
	cmd.Flags().StringVar(&destFlag, "dest", "", "Worktree destination path (default: <project>/.worktrees/<branch>)")
	cmd.Flags().BoolVar(&continueFlag, "continue", false, "Resume the most recent Claude session in this worktree (claude --continue)")
	cmd.Flags().StringVar(&sandboxFlag, "sandbox", "", "Sandbox mode for this session: none (default) or docker")
	cmd.Flags().StringVar(&sandboxImageFlag, "sandbox-image", "", "Docker image override for --sandbox=docker (defaults to [docker].image)")
	return cmd
}

// setupName picks the worktree setup name to use, falling back to
// "default" when neither flag nor config provide one.
func setupName(flag, configDefault string) string {
	if flag != "" {
		return flag
	}
	if configDefault != "" {
		return configDefault
	}
	return "default"
}

// addRuntimeFlags wires the cross-cutting --home/--profile/--project
// flags onto a cobra command. Centralising them avoids drift between
// the various subcommands.
func addRuntimeFlags(cmd *cobra.Command, opts *runtimeOptions) {
	cmd.Flags().StringVar(&opts.home, "home", "", "Override $HOME (testing)")
	cmd.Flags().StringVar(&opts.profile, "profile", "", "Profile name")
	cmd.Flags().StringVar(&opts.projectDir, "project", ".", "Project directory to read .mosaic/config.toml from")
}
