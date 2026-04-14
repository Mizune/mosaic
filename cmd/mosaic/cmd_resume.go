package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newResumeCmd builds `mosaic resume`. It respawns stopped sessions
// without attaching to them — the bulk-recovery counterpart to
// `mosaic attach`, which respawns a single session and enters it.
//
// After a PC restart every tmux session is gone and mosaic marks
// them "stopped". `mosaic resume --all` brings them all back in
// one shot; `mosaic resume name1 name2` targets specific ones.
func newResumeCmd() *cobra.Command {
	var (
		opts runtimeOptions
		all  bool
	)

	cmd := &cobra.Command{
		Use:   "resume [name ...]",
		Short: "Respawn stopped sessions without attaching",
		Long: `resume recreates the tmux backend for stopped sessions so they
are alive again without entering any of them. Useful after a PC
restart when every session is marked "stopped".

  mosaic resume --all          respawn every stopped session
  mosaic resume name1 name2    respawn specific sessions`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return fmt.Errorf("specify session names or use --all")
			}

			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			out := cmd.OutOrStdout()

			if all {
				results, err := rt.sessions.RespawnAll(cmd.Context())
				if err != nil {
					return err
				}
				if len(results) == 0 {
					fmt.Fprintln(out, "  no stopped sessions to resume")
					return nil
				}
				for _, r := range results {
					if r.Success {
						fmt.Fprintf(out, "  ✓ respawned %s\n", r.Name)
					} else {
						fmt.Fprintf(out, "  ✗ %s: %s\n", r.Name, r.Err)
					}
				}
				return nil
			}

			for _, name := range args {
				if err := rt.sessions.Respawn(cmd.Context(), name); err != nil {
					fmt.Fprintf(out, "  ✗ %s: %v\n", name, err)
				} else {
					fmt.Fprintf(out, "  ✓ respawned %s\n", name)
				}
			}
			return nil
		},
	}
	addRuntimeFlags(cmd, &opts)
	cmd.Flags().BoolVar(&all, "all", false,
		"Respawn every stopped session")
	return cmd
}
