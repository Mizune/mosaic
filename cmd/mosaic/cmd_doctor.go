package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/hook"
	"github.com/Mizune/mosaic/internal/logging"
	"github.com/Mizune/mosaic/internal/mosaicpath"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tmux"
)

// newDoctorCmd builds `mosaic doctor`. The command performs every
// step required to bring a fresh machine into a runnable state:
//
//   - resolves on-disk paths and creates them
//   - reads and merges the global / project config
//   - opens the SQLite database for the current profile
//   - runs migrations idempotently
//   - checks that tmux + git are on $PATH
//
// Each step prints a single status line to stdout. Any error is
// returned to cobra so the process exits non-zero.
func newDoctorCmd() *cobra.Command {
	var (
		printConfig  bool
		resetWizard  bool
		bench        bool
		profile      string
		homeOverride string
		projectDir   string
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the environment, run migrations, and report status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd.OutOrStdout(), doctorOptions{
				home:        homeOverride,
				profile:     profile,
				projectDir:  projectDir,
				printConfig: printConfig,
				resetWizard: resetWizard,
				bench:       bench,
			})
		},
	}
	cmd.Flags().BoolVar(&printConfig, "print-config", false, "Print the merged config and exit")
	cmd.Flags().BoolVar(&resetWizard, "reset-wizard", false, "Remove the first-run marker so the wizard runs again")
	cmd.Flags().BoolVar(&bench, "bench", false, "Run internal microbenchmarks against the Performance Budget")
	cmd.Flags().StringVar(&profile, "profile", "", "Profile name (default: from config)")
	cmd.Flags().StringVar(&homeOverride, "home", "", "Override $HOME for path resolution (testing)")
	cmd.Flags().StringVar(&projectDir, "project", ".", "Project directory to read .mosaic/config.toml from")
	return cmd
}

type doctorOptions struct {
	home        string
	profile     string
	projectDir  string
	printConfig bool
	resetWizard bool
	bench       bool
}

func runDoctor(out io.Writer, opts doctorOptions) error {
	ctx := context.Background()

	paths, err := mosaicpath.Resolve(opts.home)
	if err != nil {
		return fmt.Errorf("resolve paths: %w", err)
	}
	if err := paths.EnsureAll(); err != nil {
		return err
	}
	fmt.Fprintln(out, statusOK("data dir", paths.DataDir))

	cfg, sources, err := loadConfig(paths, opts.projectDir)
	if err != nil {
		return err
	}
	for _, src := range sources {
		fmt.Fprintln(out, statusOK("config", src))
	}
	if len(sources) == 0 {
		fmt.Fprintln(out, statusOK("config", "defaults only (no global or project file)"))
	}

	if opts.printConfig {
		return printMergedConfig(out, cfg)
	}

	profile := opts.profile
	if profile == "" {
		profile = cfg.Mosaic.Profile
	}
	if err := paths.EnsureProfile(profile); err != nil {
		return err
	}
	dbPath := paths.StateDB(profile)

	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite at %s: %w", dbPath, err)
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	applied, err := st.AppliedVersions(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, statusOK("sqlite", fmt.Sprintf("%s (migrations: %d)", dbPath, len(applied))))

	if err := checkBinary(out, "tmux"); err != nil {
		return err
	}
	if err := checkBinary(out, "git"); err != nil {
		return err
	}

	// Regenerate the mosaic-managed tmux config so the Ctrl+Q detach
	// binding (and other quality-of-life tweaks) are picked up by
	// every session mosaic spawns. Any failure here is reported but
	// does not fail doctor — the user can still use tmux's default
	// prefix to detach.
	if err := tmux.EnsureConfigFile(paths.TmuxConfigFile, tmux.ConfigOptions{ArtifactViewer: cfg.Viewer.TmuxViewer}); err != nil {
		fmt.Fprintln(out, statusFail("tmux conf", err.Error()))
	} else {
		fmt.Fprintln(out, statusOK("tmux conf", paths.TmuxConfigFile))
	}

	// Report whether mosaic's Claude Code hooks are wired up. Hooks
	// are optional, so "not installed" is a warning, not an error —
	// mosaic still works via fsnotify tailing, just with higher
	// latency.
	reportHookStatus(out, opts.home)

	logCloser, err := logging.Setup(paths.LogFile, cfg.Mosaic.LogLevel)
	if err != nil {
		return fmt.Errorf("setup logging: %w", err)
	}
	defer logCloser.Close()
	fmt.Fprintln(out, statusOK("logs", paths.LogFile))

	if opts.resetWizard {
		if err := os.Remove(paths.FirstRunMarker); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reset wizard marker: %w", err)
		}
		fmt.Fprintln(out, statusOK("wizard", "reset (will run on next launch)"))
	}

	if opts.bench {
		if err := runDoctorBench(out, st); err != nil {
			return err
		}
	}

	fmt.Fprintln(out, statusOK("ready", "mosaic "+version))
	return nil
}

// runDoctorBench runs a handful of cheap microbenchmarks and
// compares them against the Performance Budget thresholds from
// the mosaic plan. Each row reports the measured number plus a
// ✓ / ! marker so the user can tell at a glance whether mosaic
// is keeping up.
func runDoctorBench(out io.Writer, st *store.Store) error {
	ctx := context.Background()
	fmt.Fprintln(out, statusOK("bench", "running performance budget checks..."))

	// 1. Session list round-trip: measure how long N sessions
	//    take to fetch from SQLite. We seed the store with
	//    some in-memory rows (scoped to the test profile) if
	//    it is empty so cold installs still see a number.
	const seed = 100
	existing, err := st.Sessions().List(ctx)
	if err != nil {
		return err
	}
	added := 0
	for i := len(existing); i < seed; i++ {
		_, err := st.Sessions().Insert(ctx, store.NewSession{
			Name:         fmt.Sprintf("bench-%d", i),
			WorktreePath: fmt.Sprintf("/tmp/bench-%d", i),
		})
		if err != nil {
			break
		}
		added++
	}
	defer func() {
		// Clean up the synthetic rows so the benchmark does
		// not leak into the real profile state.
		rows, _ := st.Sessions().List(ctx)
		for _, r := range rows {
			if strings.HasPrefix(r.Name, "bench-") {
				_ = st.Sessions().Delete(ctx, r.ID)
			}
		}
	}()

	runs := 10
	var total time.Duration
	for i := 0; i < runs; i++ {
		t0 := time.Now()
		_, _ = st.Sessions().List(ctx)
		total += time.Since(t0)
	}
	avg := total / time.Duration(runs)
	printBenchRow(out, "sessions.List", avg, 5*time.Millisecond)

	// 2. Subagents.ListByParent: the hottest read in the TUI.
	t0 := time.Now()
	for _, r := range existing {
		_, _ = st.Subagents().ListByParent(ctx, r.ID)
	}
	subDur := time.Since(t0)
	printBenchRow(out, "subagents.ListByParent x"+fmt.Sprintf("%d", len(existing)),
		subDur, 10*time.Millisecond)

	// 3. Attention write round-trip: a common TUI write path.
	if len(existing) > 0 {
		id := existing[0].ID
		t1 := time.Now()
		for i := 0; i < 100; i++ {
			_ = st.Sessions().SetAttention(ctx, id,
				store.AttentionWaitingInput, "bench", "bench", "bench")
		}
		_ = st.Sessions().SetAttention(ctx, id,
			store.AttentionNone, "", "", "")
		avgSet := time.Since(t1) / 100
		printBenchRow(out, "sessions.SetAttention", avgSet, 1*time.Millisecond)
	}

	return nil
}

// printBenchRow prints one benchmark line, marking it ✓ when
// the measured duration fits inside the budget threshold and !
// otherwise. Colour comes from the shared status helpers so the
// output stays consistent with the rest of doctor.
func printBenchRow(out io.Writer, name string, dur, budget time.Duration) {
	label := fmt.Sprintf("%-36s %s", name, humanDur(dur))
	if dur <= budget {
		fmt.Fprintln(out, statusOK("bench", label+"  ≤ "+humanDur(budget)))
	} else {
		fmt.Fprintln(out, statusWarn("bench", label+"  > "+humanDur(budget)))
	}
}

// humanDur formats a duration with more signal than the default
// go stringer for sub-millisecond values. We expect everything
// relevant to land in the microsecond / millisecond range.
func humanDur(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d.Microseconds()))
	case d < time.Second:
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1e6)
	default:
		return d.String()
	}
}

func loadConfig(paths mosaicpath.Paths, projectDir string) (config.Config, []string, error) {
	opts := config.LoadOptions{GlobalPath: paths.GlobalConfigFile}
	if projectDir != "" {
		opts.ProjectPath = mosaicpath.ProjectConfigFile(projectDir)
	}
	res, err := config.Load(opts)
	if err != nil {
		return config.Config{}, nil, err
	}
	return res.Config, res.Sources, nil
}

func printMergedConfig(out io.Writer, cfg config.Config) error {
	enc := toml.NewEncoder(out)
	enc.Indent = "  "
	return enc.Encode(cfg)
}

func checkBinary(out io.Writer, name string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		fmt.Fprintln(out, statusFail(name, "not found in $PATH"))
		return fmt.Errorf("%s not found", name)
	}
	fmt.Fprintln(out, statusOK(name, path))
	return nil
}

// reportHookStatus prints a single status line for Claude Code hook
// integration. It is a report, never a gate — a missing settings.json
// or an uninstalled hook set is presented as a "warn" hint with the
// command the user can run to install.
func reportHookStatus(out io.Writer, homeOverride string) {
	home := homeOverride
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	if home == "" {
		fmt.Fprintln(out, statusWarn("hooks", "cannot resolve $HOME"))
		return
	}
	path := home + "/.claude/settings.json"
	inst := hook.NewInstaller(path)
	installed, err := inst.IsInstalled()
	if err != nil {
		fmt.Fprintln(out, statusWarn("hooks", fmt.Sprintf("%s (%v)", path, err)))
		return
	}
	if installed {
		fmt.Fprintln(out, statusOK("hooks", "installed in "+path))
		return
	}
	fmt.Fprintln(out, statusWarn("hooks",
		"not installed — run `mosaic hook install` for sub-second attention updates"))
}

func statusOK(label, value string) string   { return fmt.Sprintf("  ✓ %-9s %s", label, value) }
func statusFail(label, value string) string { return fmt.Sprintf("  ✗ %-9s %s", label, value) }
func statusWarn(label, value string) string { return fmt.Sprintf("  ! %-9s %s", label, value) }
