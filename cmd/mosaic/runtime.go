package main

import (
	"context"
	"fmt"
	"io"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/git"
	"github.com/Mizune/mosaic/internal/hook"
	"github.com/Mizune/mosaic/internal/live"
	"github.com/Mizune/mosaic/internal/logging"
	"github.com/Mizune/mosaic/internal/mosaicpath"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tmux"
	"github.com/Mizune/mosaic/internal/worktree"
)

// runtimeOptions are the cross-cutting flags every state-mutating
// subcommand needs (`new`, `ls`, `attach`, `kill`, `import`, ...). They
// are deliberately tiny so cobra commands can declare them once and
// hand them straight to [openRuntime].
type runtimeOptions struct {
	home       string
	profile    string
	projectDir string
	live       bool // start watcher / hook server / reconciler (TUI mode)
}

// runtime is the materialised set of objects each command needs:
// resolved paths, merged config, the SQLite store, the tmux backend,
// the in-process event bus and the session service. The struct also
// owns Close so commands can `defer rt.Close()` to release the DB
// connection and log file in one call.
//
// When [openRuntime] is called with [runtimeOptions.live] set, the
// runtime additionally starts the claudeio watcher, the unix-socket
// hook server and the live reconciler/binder. They are stored on the
// struct so Close can stop them in the right order.
type runtime struct {
	paths    mosaicpath.Paths
	config   config.Config
	store    *store.Store
	tmux     tmux.Backend
	bus      *bus.Bus
	sessions *service.SessionService

	// Live layer (TUI mode only).
	watcher    *claudeio.Watcher
	hookServer *hook.Server
	reconciler *live.Reconciler
	binder     *live.Binder
	messaging  *service.MessagingService
	liveCancel context.CancelFunc

	logCloser io.Closer
}

// openRuntime resolves paths, loads config, opens the database and
// runs migrations. It is the single entry point that every command
// uses to acquire its dependencies.
func openRuntime(ctx context.Context, opts runtimeOptions) (*runtime, error) {
	paths, err := mosaicpath.Resolve(opts.home)
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureAll(); err != nil {
		return nil, err
	}

	loadOpts := config.LoadOptions{GlobalPath: paths.GlobalConfigFile}
	if opts.projectDir != "" {
		loadOpts.ProjectPath = mosaicpath.ProjectConfigFile(opts.projectDir)
	}
	res, err := config.Load(loadOpts)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	profile := opts.profile
	if profile == "" {
		profile = res.Config.Mosaic.Profile
	}
	if err := paths.EnsureProfile(profile); err != nil {
		return nil, err
	}

	st, err := store.Open(paths.StateDB(profile))
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	if err := st.Migrate(ctx); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	logCloser, err := logging.Setup(paths.LogFile, res.Config.Mosaic.LogLevel)
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("setup logging: %w", err)
	}

	tmuxBackend := &tmux.CLI{Socket: res.Config.Tmux.SocketName}
	if tmuxBackend.Socket == "" {
		tmuxBackend.Socket = tmux.DefaultSocket
	}
	// Regenerate the mosaic-managed tmux config on every startup so
	// the latest Ctrl+Q detach binding (and future tweaks) are
	// always in effect. The config is loaded via -f when NewSession
	// starts a fresh tmux server; when a server is already running
	// from a previous mosaic invocation, we follow up with an
	// explicit source-file so existing sessions also pick up the
	// new bindings without a kill-server. Both steps are best
	// effort: worst case the user has to use the tmux prefix to
	// detach.
	if err := tmux.EnsureConfigFile(paths.TmuxConfigFile); err == nil {
		tmuxBackend.ConfigFile = paths.TmuxConfigFile
		_ = tmuxBackend.SourceConfig(ctx)
	}

	b := bus.New()
	wt := worktree.NewManager(git.New())

	sessionSvc := service.NewSessionService(st, tmuxBackend, b, wt)
	// Use the project's default base branch as the Diff View
	// comparison ref. It falls back to "main" when unset.
	if base := res.Config.Worktree.DefaultBaseBranch; base != "" {
		sessionSvc.DiffBaseBranch = base
	}
	// Per-tool launch flag overrides from the user's config
	// (e.g. claude bypass_permissions / auto_mode).
	if len(res.Config.Mosaic.Tools) > 0 {
		sessionSvc.ToolFlags = res.Config.Mosaic.Tools
	}
	rt := &runtime{
		paths:     paths,
		config:    res.Config,
		store:     st,
		tmux:      tmuxBackend,
		bus:       b,
		sessions:  sessionSvc,
		logCloser: logCloser,
	}

	if opts.live {
		if err := rt.startLive(ctx, opts.home); err != nil {
			_ = rt.Close()
			return nil, fmt.Errorf("start live layer: %w", err)
		}
	}

	return rt, nil
}

// startLive boots the watcher, hook socket and reconciler/binder so
// the TUI shows real-time sub-agent state.
func (r *runtime) startLive(ctx context.Context, home string) error {
	cctx, cancel := context.WithCancel(ctx)
	r.liveCancel = cancel

	r.watcher = claudeio.NewWatcher(r.bus, home)
	if err := r.watcher.Start(cctx); err != nil {
		return fmt.Errorf("watcher: %w", err)
	}

	r.hookServer = hook.NewServer(r.bus)
	if socketPath, err := r.hookServer.Listen(cctx); err != nil {
		// Hook server is best-effort: failure (e.g. socket dir
		// permission denied) should not break the TUI.
		r.hookServer = nil
	} else {
		// Tell the session service where the hook socket is so
		// every tmux session mosaic spawns exports MOSAIC_SOCKET_PATH
		// (and its companions) into the agent's environment. Without
		// this step the installed hook commands see an unset
		// MOSAIC_SOCKET_PATH and short-circuit to true via their env
		// guard — which is the "hooks installed but silent" bug we
		// hit without this step.
		r.sessions.HookSocketPath = socketPath
	}

	r.reconciler = live.NewReconciler(r.store, r.bus)
	r.reconciler.Start()

	r.binder = live.NewBinder(r.store, r.watcher, home)
	r.binder.Start(cctx)

	// Messaging delivery loop — turns queued inter-agent messages
	// into tmux send-keys injections on a tick. Cheap while idle;
	// failing tmux calls degrade gracefully so a missing session
	// does not stall the loop.
	r.messaging = service.NewMessagingService(r.store, r.tmux, r.bus, 0)
	r.messaging.Start(cctx)
	return nil
}

// Close releases every resource the runtime owns. Commands should
// `defer rt.Close()` immediately after openRuntime succeeds.
func (r *runtime) Close() error {
	if r == nil {
		return nil
	}
	// Shut down live components in reverse start order so the bus
	// has nobody publishing to it by the time it itself closes.
	if r.messaging != nil {
		r.messaging.Stop()
		r.messaging = nil
	}
	if r.binder != nil {
		r.binder.Stop()
		r.binder = nil
	}
	if r.reconciler != nil {
		r.reconciler.Stop()
		r.reconciler = nil
	}
	if r.hookServer != nil {
		_ = r.hookServer.Close()
		r.hookServer = nil
	}
	if r.watcher != nil {
		r.watcher.Stop()
		r.watcher = nil
	}
	if r.liveCancel != nil {
		r.liveCancel()
		r.liveCancel = nil
	}
	if r.bus != nil {
		r.bus.Close()
	}
	if r.store != nil {
		_ = r.store.Close()
	}
	if r.logCloser != nil {
		_ = r.logCloser.Close()
	}
	return nil
}
