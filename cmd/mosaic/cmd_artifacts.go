package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tmux"
	"github.com/Mizune/mosaic/internal/tui"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

// newArtifactsCmd builds `mosaic artifacts [<name>]`: the artifact
// picker as a standalone program, meant to run inside an attached
// session through the tmux prefix+O binding (a popup or a split) so
// the user can look at diagrams and screenshots without detaching.
//
// Session resolution order:
//
//  1. positional <name> if supplied;
//  2. MOSAIC_WORKTREE_ID (set by the live TUI on every pane it spawns);
//  3. the tmux session the process runs in: $TMUX names the server and
//     the session index, `display-message '#S'` gives `mos_<id>`.
func newArtifactsCmd() *cobra.Command {
	var opts runtimeOptions

	cmd := &cobra.Command{
		Use:   "artifacts [<name>]",
		Short: "Browse a session's artifacts (Mermaid, images, HTML) in a picker",
		Long: `Open the artifact picker for a session as a standalone program.

Inside an attached mosaic session press prefix+O: mosaic's tmux config
binds it to this command in a popup (or a split, see [viewer]
tmux_viewer). The session is resolved from the pane you are in, so a
bare ` + "`mosaic artifacts`" + ` works there; pass <name> elsewhere.

Enter opens the selected artifact in a browser pane or the OS browser;
on Kitty-graphics terminals (Ghostty, kitty, WezTerm) the selected
artifact is also drawn inline.`,
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
			row, err := resolveArtifactsSession(cmd.Context(), rt, name)
			if err != nil {
				return err
			}

			th := theme.ByName(rt.config.UI.Theme)
			prog := tui.NewArtifactPickerProgram(rt.store, row.ID, row.Name, th, rt.config.Viewer, os.Getenv("MOSAIC_OUTER_TMUX"))
			_, err = tea.NewProgram(prog, tea.WithAltScreen()).Run()
			return err
		},
	}
	addRuntimeFlags(cmd, &opts)
	return cmd
}

// tmuxSessionNamer resolves a tmux target on the server behind socket
// to its session name ([tmux.CLI.SessionNameAt]).
type tmuxSessionNamer func(ctx context.Context, socket, target string) (string, error)

// sessionIDFromTmux derives the mosaic session id from the tmux session
// the process runs in: $TMUX gives the socket and the session index,
// the session name is `mos_<id>`.
func sessionIDFromTmux(ctx context.Context, tmuxEnv string, nameOf tmuxSessionNamer) (string, error) {
	env, err := tmux.ParseEnv(tmuxEnv)
	if err != nil {
		return "", err
	}
	name, err := nameOf(ctx, env.Socket, env.SessionTarget())
	if err != nil {
		return "", err
	}
	id := strings.TrimPrefix(name, tmux.SessionPrefix)
	if id == "" || id == name {
		return "", fmt.Errorf("tmux session %q is not a mosaic session", name)
	}
	return id, nil
}

// resolveArtifactsSession applies the resolution order documented on
// newArtifactsCmd.
func resolveArtifactsSession(ctx context.Context, rt *runtime, name string) (store.Session, error) {
	id := os.Getenv("MOSAIC_WORKTREE_ID")
	if name == "" && id == "" {
		if fromTmux, err := sessionIDFromTmux(ctx, os.Getenv("TMUX"), rt.tmux.SessionNameAt); err == nil {
			id = fromTmux
		}
	}
	row, err := rt.sessions.ResolveSession(ctx, name, id)
	if err != nil {
		return store.Session{}, fmt.Errorf("%w — pass <name> or press prefix+O inside an attached mosaic session", err)
	}
	return row, nil
}
