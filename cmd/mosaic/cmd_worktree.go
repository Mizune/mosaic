package main

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/mosaicpath"
	"github.com/Mizune/mosaic/internal/tui"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

// newWorktreeCmd builds the `mosaic worktree` subcommand tree.
// Today the only child is `setup new`; future additions (list,
// delete, validate) can hang off the same parent.
func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage worktree setup recipes",
		Long: `The worktree command manages the declarative setup recipes that
mosaic applies when creating a new git worktree for a session
(see [worktree.setup.<name>] in config.toml).`,
	}
	cmd.AddCommand(newWorktreeSetupCmd())
	return cmd
}

// newWorktreeSetupCmd is the parent for every `setup` verb. Right
// now it only wires `new`; listing / deleting recipes can follow
// the same pattern later.
func newWorktreeSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Manage worktree setup recipes",
	}
	cmd.AddCommand(newWorktreeSetupNewCmd())
	return cmd
}

// newWorktreeSetupNewCmd launches the interactive recipe wizard and
// writes the result to the project's .mosaic/config.toml.
func newWorktreeSetupNewCmd() *cobra.Command {
	var (
		opts        runtimeOptions
		projectFlag string
	)
	cmd := &cobra.Command{
		Use:   "new [name]",
		Short: "Create a new worktree setup recipe via an interactive wizard",
		Long: `Runs an interactive wizard that walks you through building a
worktree setup recipe (Copy, Commands, PostCreateHook, Env) and
writes it as [worktree.setup.<name>] into <project>/.mosaic/config.toml.

The optional positional argument pre-fills the recipe name on the
first wizard page so users with a name already in mind can press
Enter once to get past the Name step.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project := projectFlag
			if project == "" {
				project = opts.projectDir
			}
			if project == "" {
				project = "."
			}
			projectAbs, err := filepath.Abs(project)
			if err != nil {
				return fmt.Errorf("resolve project path: %w", err)
			}

			// Load the merged config so we can (a) seed the theme and
			// (b) warn the user if their chosen recipe name already
			// exists in the defaults / global config / project file.
			paths, err := mosaicpath.Resolve(opts.home)
			if err != nil {
				return err
			}
			res, err := config.Load(config.LoadOptions{
				GlobalPath:  paths.GlobalConfigFile,
				ProjectPath: mosaicpath.ProjectConfigFile(projectAbs),
			})
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			existing := make(map[string]struct{}, len(res.Config.Worktree.Setup))
			for k := range res.Config.Worktree.Setup {
				existing[k] = struct{}{}
			}

			initialName := ""
			if len(args) == 1 {
				initialName = args[0]
			}

			th := theme.ByName(res.Config.UI.Theme)
			program := tui.NewRecipeWizardProgram(projectAbs, initialName, existing, th)
			if _, err := tea.NewProgram(program, tea.WithAltScreen()).Run(); err != nil {
				return fmt.Errorf("wizard: %w", err)
			}

			if program.Saved() {
				fmt.Fprintf(cmd.OutOrStdout(),
					"saved recipe %q to %s\n", program.Name(), program.SavePath())
				return nil
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "recipe wizard cancelled")
			return nil
		},
	}
	addRuntimeFlags(cmd, &opts)
	cmd.Flags().StringVar(&projectFlag, "path", "",
		"Project directory whose .mosaic/config.toml will be updated (default: --project or cwd)")
	return cmd
}
