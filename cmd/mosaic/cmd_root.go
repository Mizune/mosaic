package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof" // side-effect: registers /debug/pprof handlers
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/tui"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

// newRootCmd builds the cobra command tree. Splitting this out makes
// the CLI testable from cmd_test.go without globals.
//
// The bare `mosaic` invocation launches the TUI; subcommands handle
// the scriptable CRUD/import flows that don't require interactive
// rendering.
func newRootCmd() *cobra.Command {
	var (
		opts      runtimeOptions
		pprofAddr string
	)

	root := &cobra.Command{
		Use:   "mosaic",
		Short: "TUI command center for parallel AI coding agents",
		Long: `mosaic is a terminal session manager for Claude Code, Codex, Gemini and
similar LLM coding agents. It provides sub-agent visibility, attention
detection, declarative worktree setup, and a built-in conductor.`,
		Version:           version,
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// --pprof :6060 turns on net/http/pprof before the
			// TUI takes over the screen. The handler registry
			// is wired via the blank import above.
			if pprofAddr != "" {
				go func() {
					_ = http.ListenAndServe(pprofAddr, nil)
				}()
				fmt.Fprintf(cmd.ErrOrStderr(),
					"pprof listening on http://%s/debug/pprof/\n", pprofAddr)
			}

			// The root command launches the TUI; turn on the live
			// layer (watcher / hook server / reconciler / binder) so
			// sub-agent state actually streams in.
			opts.live = true
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()
			// Resolve the theme from config; ByName falls back
			// to Catppuccin Mocha on an empty/unknown name so
			// there is no crash path for a misconfigured TOML.
			th := theme.ByName(rt.config.UI.Theme)
			model := tui.NewModel(rt.sessions, rt.store, rt.bus, th, rt.config)
			model.SetVersion(version)
			// Give the TUI the on-disk paths it needs for the
			// Settings screen. Everything is derived from the
			// runtime so the Model stays decoupled from the
			// cmd / mosaicpath packages for tests.
			profile := rt.config.Mosaic.Profile
			if profile == "" {
				profile = "default"
			}
			model.SetConfigPaths(
				rt.paths.GlobalConfigFile,
				rt.paths.ProfileDir(profile),
				rt.paths.StateDB(profile),
				rt.paths.LogFile,
			)

			// First-run wizard: if the sentinel marker is
			// missing we're on a fresh install, so kick off the
			// onboarding flow before the TUI shows the TOP
			// screen. The wizard writes the marker itself when
			// it finishes (or when the user presses Esc to
			// skip), so this check evaluates to false on every
			// subsequent launch.
			if _, err := os.Stat(rt.paths.FirstRunMarker); os.IsNotExist(err) {
				home := opts.home
				if home == "" {
					home, _ = os.UserHomeDir()
				}
				var settingsPath string
				if home != "" {
					if rt.config.Mosaic.DefaultTool == "codex" {
						settingsPath = filepath.Join(home, ".codex", "hooks.json")
					} else {
						settingsPath = filepath.Join(home, ".claude", "settings.json")
					}
				}
				model.ShowFirstRunWizard(rt.paths.FirstRunMarker, settingsPath)
			}

			p := tea.NewProgram(model, tea.WithAltScreen())
			_, err = p.Run()
			if err != nil {
				return err
			}
			// Profile switch requested via the picker: the
			// TUI quit cleanly, and we re-exec mosaic with
			// the new --profile flag. Because this runs
			// after defer rt.Close(), the old state is
			// torn down before the new profile's store
			// opens, and any runtime-scoped sockets are
			// released.
			if target := model.PendingProfile(); target != "" {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"switching to profile %q — restart mosaic with `mosaic --profile %s`\n",
					target, target)
			}
			return nil
		},
	}
	addRuntimeFlags(root, &opts)
	root.Flags().StringVar(&pprofAddr, "pprof", "",
		"Start a net/http/pprof server on the given address (e.g. :6060)")
	root.AddCommand(newCleanCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newCreateCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newAttachCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newArtifactsCmd())
	root.AddCommand(newKillCmd())
	root.AddCommand(newHookCmd())
	root.AddCommand(newConductorCmd())
	root.AddCommand(newSendCmd())
	root.AddCommand(newStatuslineCmd())
	root.AddCommand(newStopCmd())
	root.AddCommand(newDiscordCmd())
	root.AddCommand(newResumeCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newWorktreeCmd())
	return root
}
