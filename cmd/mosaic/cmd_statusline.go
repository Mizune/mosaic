package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/store"
)

// newStatuslineCmd builds `mosaic statusline`. It is meant to be
// called from tmux's status-right via:
//
//	set -g status-right '#(mosaic statusline 2>/dev/null)'
//
// The command reads the session store, counts statuses, prints
// one compact line to stdout, and exits. Errors are swallowed
// (empty output) so the tmux status bar never shows a traceback.
func newStatuslineCmd() *cobra.Command {
	var opts runtimeOptions

	cmd := &cobra.Command{
		Use:    "statusline",
		Short:  "Print a one-line session summary for tmux status-right",
		Hidden: true, // not user-facing docs, but discoverable via --help
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				// Swallow: empty output is the safe fallback
				// for tmux status-right.
				return nil
			}
			defer rt.Close()

			sessions, err := rt.store.Sessions().List(cmd.Context())
			if err != nil {
				return nil
			}

			var running, idle, waiting, stopped int
			for _, s := range sessions {
				switch {
				case s.AttentionState == string(store.AttentionWaitingInput):
					waiting++
				case s.Status == string(store.StatusRunning):
					running++
				case s.Status == string(store.StatusIdle):
					idle++
				case s.Status == string(store.StatusStopped):
					stopped++
				}
			}

			var parts []string
			if running > 0 {
				parts = append(parts, fmt.Sprintf("● %d run", running))
			}
			if idle > 0 {
				parts = append(parts, fmt.Sprintf("○ %d idle", idle))
			}
			if waiting > 0 {
				parts = append(parts, fmt.Sprintf("⚠ %d wait", waiting))
			}
			if stopped > 0 {
				parts = append(parts, fmt.Sprintf("■ %d stop", stopped))
			}
			if len(parts) == 0 {
				parts = append(parts, "mosaic: no sessions")
			}

			line := "mosaic"
			for _, p := range parts {
				line += "  " + p
			}
			fmt.Fprintln(cmd.OutOrStdout(), line)
			return nil
		},
	}
	addRuntimeFlags(cmd, &opts)
	return cmd
}
