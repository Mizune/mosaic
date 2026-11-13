package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/hook"
)

// newStopCmd builds `mosaic stop`. It is the "turn everything off"
// escape hatch for situations where the user just wants their
// machine back: kill every tmux session mosaic is managing, remove
// stale hook sockets, and (with --wipe-state) drop the SQLite rows
// so the next `mosaic` launch starts from a clean slate.
//
// The command is deliberately explicit about what it touches so
// scripting it in CI / cleanup hooks is predictable. Default is
// "kill all tmux sessions AND the server" because that is the main
// thing users reach for.
func newStopCmd() *cobra.Command {
	var (
		opts       runtimeOptions
		wipeState  bool
		keepTmux   bool
		killMosaic bool
	)

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop every mosaic-managed tmux session (and optionally the TUI process too)",
		Long: `stop shuts down everything mosaic owns:

  - every tmux session on the mosaic socket (tmux -L mosaic kill-server)
  - stale hook sockets under /tmp/mosaic-<uid>/
  - optionally, every running mosaic process                  (--kill-mosaic)
  - optionally, the SQLite rows so the next launch starts clean (--wipe-state)

Safe to run while the TUI is live — the TUI will notice its tmux
sessions are gone on the next refresh and mark them "stopped".`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			out := cmd.OutOrStdout()

			// 1. Kill every tmux session on the mosaic socket.
			if !keepTmux {
				if err := killTmuxServer(cmd.Context(), rt); err != nil {
					fmt.Fprintf(out, "  ✗ tmux server: %v\n", err)
				} else {
					fmt.Fprintln(out, "  ✓ tmux server killed")
				}
			}

			// 2. Remove stale /tmp/mosaic-<uid>/pid-<pid> sockets
			//    left behind by crashed mosaic processes.
			if removed, err := pruneHookSockets(); err != nil {
				fmt.Fprintf(out, "  ✗ hook sockets: %v\n", err)
			} else if removed > 0 {
				fmt.Fprintf(out, "  ✓ pruned %d stale hook socket(s)\n", removed)
			} else {
				fmt.Fprintln(out, "  ✓ no stale hook sockets")
			}

			// 3. Optionally wipe SQLite state.
			if wipeState {
				if err := wipeStoreState(rt); err != nil {
					fmt.Fprintf(out, "  ✗ wipe state: %v\n", err)
				} else {
					fmt.Fprintln(out, "  ✓ sessions / subagents / conductors cleared")
				}
			}

			// 4. Optionally send SIGTERM to any other mosaic
			//    process on this machine.
			if killMosaic {
				if err := killMosaicProcesses(out); err != nil {
					fmt.Fprintf(out, "  ✗ kill mosaic processes: %v\n", err)
				}
			}

			return nil
		},
	}
	addRuntimeFlags(cmd, &opts)
	cmd.Flags().BoolVar(&wipeState, "wipe-state", false,
		"Also clear sessions, subagents, conductors, and notifications from SQLite")
	cmd.Flags().BoolVar(&keepTmux, "keep-tmux", false,
		"Do not kill tmux sessions (use with --wipe-state to forget sessions without stopping them)")
	cmd.Flags().BoolVar(&killMosaic, "kill-mosaic", false,
		"Also send SIGTERM to every running mosaic process (best-effort)")
	return cmd
}

// killTmuxServer runs `tmux -L mosaic kill-server` via the shared
// CLI wrapper. A runtime without a tmux CLI (tests) is a no-op.
func killTmuxServer(ctx context.Context, rt *runtime) error {
	if rt.tmux == nil {
		return nil
	}
	return rt.tmux.KillServer(ctx)
}

// pruneHookSockets removes every /tmp/mosaic-<uid>/pid-* file
// regardless of whether the pid is still alive. `mosaic stop` is
// the user saying "take everything down"; surviving processes will
// simply recreate their own socket next time they start.
func pruneHookSockets() (int, error) {
	dir := hook.SocketDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		name := e.Name()
		if len(name) < 4 || name[:4] != "pid-" {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
		removed++
	}
	return removed, nil
}

// wipeStoreState clears the mutable SQLite tables without dropping
// the schema, so the next mosaic launch starts clean but
// migrations do not re-run.
func wipeStoreState(rt *runtime) error {
	db := rt.store.DB()
	// Order matters: child tables first so foreign keys do not
	// complain. schema_migrations is deliberately preserved.
	for _, table := range []string{
		"messages",
		"tasks",
		"conductors",
		"notifications",
		"subagent_events",
		"subagents",
		"worktrees",
		"sessions",
	} {
		// Missing tables from earlier migrations are ignored.
		_, _ = db.Exec("DELETE FROM " + table)
	}
	return nil
}

// killMosaicProcesses shells out to `pgrep -f mosaic` and sends
// SIGTERM to every pid that is not our own.
func killMosaicProcesses(out io.Writer) error {
	self := os.Getpid()
	pids, err := readProcPids()
	if err != nil {
		return err
	}
	killed := 0
	for _, pid := range pids {
		if pid == self {
			continue
		}
		if err := sendSigterm(pid); err == nil {
			killed++
		}
	}
	fmt.Fprintf(out, "  ✓ sent SIGTERM to %d mosaic process(es)\n", killed)
	return nil
}
