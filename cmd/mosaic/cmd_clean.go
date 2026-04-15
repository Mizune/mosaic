package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/hook"
	"github.com/Mizune/mosaic/internal/mosaicpath"
	"github.com/Mizune/mosaic/internal/pidguard"
	"github.com/Mizune/mosaic/internal/store"
)

func newCleanCmd() *cobra.Command {
	var (
		logsOnly     bool
		dryRun       bool
		profile      string
		homeOverride string
	)

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove stale runtime files, truncate logs, and checkpoint the database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runClean(cmd.OutOrStdout(), cleanOptions{
				home:     homeOverride,
				profile:  profile,
				logsOnly: logsOnly,
				dryRun:   dryRun,
			})
		},
	}
	cmd.Flags().BoolVar(&logsOnly, "logs", false, "Only truncate the log file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be cleaned without doing it")
	cmd.Flags().StringVar(&profile, "profile", "", "Profile name (default: from config)")
	cmd.Flags().StringVar(&homeOverride, "home", "", "Override $HOME for path resolution (testing)")
	return cmd
}

type cleanOptions struct {
	home     string
	profile  string
	logsOnly bool
	dryRun   bool
}

func runClean(out io.Writer, opts cleanOptions) error {
	paths, err := mosaicpath.Resolve(opts.home)
	if err != nil {
		return fmt.Errorf("resolve paths: %w", err)
	}

	// --- Logs ---
	cleanLog(out, paths.LogFile, opts.dryRun)

	if opts.logsOnly {
		return nil
	}

	// --- Stale PID file ---
	cleanPID(out, paths.PIDFile, opts.dryRun)

	// --- Stale hook sockets ---
	cleanSockets(out, opts.dryRun)

	// --- SQLite WAL checkpoint ---
	profile := opts.profile
	if profile == "" {
		profile = "default"
	}
	cleanWAL(out, paths.StateDB(profile), opts.dryRun)

	return nil
}

func cleanLog(out io.Writer, path string, dryRun bool) {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintln(out, statusOK("logs", "no log file"))
		return
	}
	size := info.Size()
	if size == 0 {
		fmt.Fprintln(out, statusOK("logs", "already empty"))
		return
	}
	if dryRun {
		fmt.Fprintln(out, statusWarn("logs", fmt.Sprintf("would truncate %s (%s)", path, humanBytes(size))))
		return
	}
	if err := os.Truncate(path, 0); err != nil {
		fmt.Fprintln(out, statusFail("logs", fmt.Sprintf("truncate: %v", err)))
		return
	}
	fmt.Fprintln(out, statusOK("logs", fmt.Sprintf("truncated (was %s)", humanBytes(size))))
}

func cleanPID(out io.Writer, path string, dryRun bool) {
	if !pidguard.IsStale(path) {
		return
	}
	if dryRun {
		fmt.Fprintln(out, statusWarn("pid", "would remove stale PID file"))
		return
	}
	if pidguard.RemoveStale(path) {
		fmt.Fprintln(out, statusOK("pid", "removed stale PID file"))
	}
}

func cleanSockets(out io.Writer, dryRun bool) {
	removed, err := hook.CleanStaleSockets(dryRun)
	if err != nil {
		fmt.Fprintln(out, statusFail("sockets", fmt.Sprintf("clean: %v", err)))
		return
	}
	if removed == 0 {
		return
	}
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	fmt.Fprintln(out, statusOK("sockets", fmt.Sprintf("%s %d stale socket(s)", verb, removed)))
}

// cleanWAL opens a short-lived connection to checkpoint the WAL file.
// Migrations are NOT run here — the main TUI or `mosaic doctor` should
// have already migrated. Running this while the TUI is active is safe
// but the TRUNCATE checkpoint may be a no-op if the TUI holds WAL
// readers.
func cleanWAL(out io.Writer, dbPath string, dryRun bool) {
	if _, err := os.Stat(dbPath); err != nil {
		return
	}
	if dryRun {
		walPath := dbPath + "-wal"
		if info, err := os.Stat(walPath); err == nil && info.Size() > 0 {
			fmt.Fprintln(out, statusWarn("wal", fmt.Sprintf("would checkpoint %s (%s)", walPath, humanBytes(info.Size()))))
		}
		return
	}
	st, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintln(out, statusFail("wal", fmt.Sprintf("open: %v", err)))
		return
	}
	defer st.Close()

	if err := st.WALCheckpoint(); err != nil {
		fmt.Fprintln(out, statusFail("wal", fmt.Sprintf("checkpoint: %v", err)))
		return
	}
	fmt.Fprintln(out, statusOK("wal", "checkpoint complete"))
}

func humanBytes(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
	}
}
