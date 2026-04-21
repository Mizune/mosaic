// Package service contains the application services that orchestrate
// the lower-level packages (store, tmux, bus, ...) into operations
// that the CLI and the TUI both consume.
//
// Each service is intentionally thin: it does not enforce policy that
// belongs in the lower layers, but it does compose them safely (e.g.
// "create a session" means insert a row, spawn a tmux session, and
// publish a SessionStarted event in the right order).
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/git"
	"github.com/Mizune/mosaic/internal/sandbox"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tmux"
	"github.com/Mizune/mosaic/internal/worktree"
)

// SessionService composes the sessions repository, the tmux backend
// and the event bus into the operations exposed by `mosaic new` /
// `ls` / `attach` / `kill` and the TUI session list.
type SessionService struct {
	store    *store.Store
	tmux     tmux.Backend
	bus      *bus.Bus
	worktree *worktree.Manager
	git      *git.Client

	// Cfg groups the forwarded configuration fields that
	// openRuntime copies from the merged config at startup.
	// Kept in a dedicated struct so the service's construction
	// dependencies (store/tmux/bus/worktree) stay separate from
	// its runtime-configuration surface.
	Cfg SessionServiceConfig
}

// SessionServiceConfig groups the config-derived fields that
// [openRuntime] forwards into the session service. Collecting them
// in one struct clarifies the service's configuration surface and
// prevents the field list from growing unboundedly on the service
// itself.
type SessionServiceConfig struct {
	// HookSocketPath is the unix socket path the hook server
	// listens on. When non-empty, every tmux session mosaic
	// creates exports MOSAIC_SOCKET_PATH into the agent's env.
	// Left empty in CLI-only commands where no hook server runs.
	HookSocketPath string

	// DiffBaseBranch is the fallback ref the Diff helpers compare
	// against when a session row has no explicit base.
	DiffBaseBranch string

	// ToolFlags carries per-tool launch flag overrides from the
	// user's config.toml.
	ToolFlags map[string]config.ToolLaunchFlags

	// Docker carries the resolved [docker] defaults for sandbox
	// spec resolution.
	Docker config.DockerSection

	// Worktree carries the [worktree] section so Kill can honour
	// the auto_cleanup setting.
	Worktree config.WorktreeSection

	// Projects is the registered project list used for per-project
	// sandbox opt-in resolution.
	Projects []config.ProjectEntry
}

// NewSessionService constructs a session service. Pass nil for the
// worktree manager when the caller does not need worktree support
// (e.g. fake-tmux unit tests). The service constructs its own
// [git.Client] — callers that want to share one with the worktree
// manager can pass a ready instance via [SessionService.SetGit]
// after construction.
func NewSessionService(s *store.Store, t tmux.Backend, b *bus.Bus, w *worktree.Manager) *SessionService {
	return &SessionService{
		store:    s,
		tmux:     t,
		bus:      b,
		worktree: w,
		git:      git.New(),
		Cfg:      SessionServiceConfig{DiffBaseBranch: "main"},
	}
}

// CreateInput is the user-facing argument shape for [SessionService.Create].
type CreateInput struct {
	Name         string
	ProjectPath  string
	WorktreePath string
	Branch       string
	Tool         store.SessionTool
	Command      string // optional shell command for the first pane

	// ForceContinue makes the session launch with `--continue`
	// even when AutoContinue is false in the config. Set by
	// `mosaic new --continue` and the TUI wizard's "continue
	// previous" toggle. The flag ONLY controls the Create
	// path; respawn always continues regardless.
	ForceContinue bool

	// InitialPrompt is a non-empty string that mosaic types into
	// the worker's pane after the tool has had a moment to boot.
	// It is the right way to feed a first-message to an LLM
	// agent like Claude Code — setting it leaves the tmux
	// command alone (so the normal tool launcher runs) and then
	// drives the agent via tmux send-keys.
	//
	// Conductor runs populate this from [TaskSpec.Prompt].
	// Direct CLI / TUI callers typically leave it empty.
	InitialPrompt string

	// CreateWorktree triggers worktree creation before tmux spawn.
	CreateWorktree bool

	// WorktreeRepo is the source repository when CreateWorktree is
	// true. Defaults to ProjectPath when empty.
	WorktreeRepo string

	// WorktreeBaseRef is the ref to branch from. Defaults to the
	// repo's current branch when empty.
	WorktreeBaseRef string

	// WorktreeSetup is the recipe to apply to the new worktree.
	WorktreeSetup config.WorktreeSetup

	// SandboxMode selects the isolation backend for this session.
	// When empty, the resolver falls back to the per-project
	// `sandbox` field and ultimately to "none". Explicitly
	// passing sandbox.ModeNone forces host execution even when
	// the project opts into docker.
	SandboxMode sandbox.Mode

	// SandboxImage overrides the [docker] default image for this
	// session only. Ignored when SandboxMode != ModeDocker.
	//
	// Like ForceContinue, this override is applied to the initial
	// Create only and is NOT persisted on the session row. Respawn
	// re-resolves the image from current config, so a per-session
	// override does not survive a respawn — edit [docker].image in
	// config.toml if you need the override to stick.
	SandboxImage string
}

// Create inserts a session row, optionally creates a worktree, spawns
// the matching tmux session, and publishes [bus.SessionStarted].
//
// On any failure after a side effect has happened, Create rolls back
// the previously committed steps so the user is not left with a
// half-created session.
func (s *SessionService) Create(ctx context.Context, in CreateInput) (store.Session, error) {
	if in.WorktreePath == "" && in.ProjectPath != "" {
		in.WorktreePath = in.ProjectPath
	}
	if in.WorktreePath == "" {
		return store.Session{}, errors.New("session: worktree_path or project_path is required")
	}
	abs, err := filepath.Abs(in.WorktreePath)
	if err != nil {
		return store.Session{}, fmt.Errorf("resolve worktree_path: %w", err)
	}
	in.WorktreePath = abs
	if in.ProjectPath == "" {
		in.ProjectPath = abs
	}

	// 1. Optionally create a git worktree before anything else so
	//    that tmux can launch directly inside it. The repo defaults
	//    to ProjectPath, the destination is the requested worktree
	//    path.
	worktreeCreated := false
	worktreeRepo := in.WorktreeRepo
	if worktreeRepo == "" {
		worktreeRepo = in.ProjectPath
	}
	if in.CreateWorktree {
		if s.worktree == nil {
			return store.Session{}, errors.New("session: worktree manager not configured")
		}
		if in.Branch == "" {
			return store.Session{}, errors.New("session: --branch is required when creating a worktree")
		}
		_, err := s.worktree.Create(ctx, worktree.CreateInput{
			Repo:         worktreeRepo,
			DestPath:     in.WorktreePath,
			Branch:       in.Branch,
			BaseRef:      in.WorktreeBaseRef,
			CreateBranch: true,
			Setup:        in.WorktreeSetup,
		})
		if err != nil {
			return store.Session{}, fmt.Errorf("worktree create: %w", err)
		}
		worktreeCreated = true
	}

	// rollbackWT undoes the worktree side-effect on any early
	// return. Capturing the decision once avoids repeating the
	// four-line guard on every error path.
	rollbackWT := func() {
		if worktreeCreated && s.worktree != nil {
			_ = s.worktree.Remove(ctx, worktreeRepo, in.WorktreePath, true)
		}
	}

	spec, err := s.resolveSandboxSpec(in)
	if err != nil {
		rollbackWT()
		return store.Session{}, err
	}

	// Pre-generate the ULID so the sandbox container name can be
	// derived before the row is persisted.
	id, err := store.NewSessionID()
	if err != nil {
		rollbackWT()
		return store.Session{}, fmt.Errorf("generate session id: %w", err)
	}
	containerName := ""
	if spec.Mode == sandbox.ModeDocker {
		containerName = sandbox.ContainerName(id)
	}

	row, err := s.store.Sessions().Insert(ctx, store.NewSession{
		ID:               id,
		Name:             in.Name,
		ProjectPath:      in.ProjectPath,
		WorktreePath:     in.WorktreePath,
		Branch:           in.Branch,
		Tool:             in.Tool,
		SandboxMode:      spec.Mode.String(),
		SandboxContainer: containerName,
	})
	if err != nil {
		rollbackWT()
		return store.Session{}, err
	}

	launchCommand, launchArgv := s.resolveLaunchCommand(in)
	tmuxOpts := s.buildLaunchTmuxOpts(row, spec, launchCommand, launchArgv)

	if err := s.tmux.NewSession(ctx, tmuxOpts); err != nil {
		_ = s.store.Sessions().Delete(ctx, row.ID)
		rollbackWT()
		return store.Session{}, fmt.Errorf("tmux create: %w", err)
	}

	// Leave the new session at its default idle status. Marking it
	// "running" here was a lie — nothing has actually happened in
	// the pane yet. The reconciler will flip the status to running
	// the moment it observes an Agent tool_use (or a hook busy
	// event), and back to idle when activity dies down, so the
	// status column reflects what the agent is really doing.
	if s.bus != nil {
		s.bus.Publish(bus.SessionStarted{SessionID: row.ID, At: time.Now()})
	}

	// If an initial prompt was supplied, type it into the worker's
	// pane after a brief delay so the tool has had time to boot.
	// The delay is fire-and-forget: we never block Create on it
	// because conductor dispatches a whole batch of sessions and
	// must not serialise.
	if in.InitialPrompt != "" {
		go s.injectInitialPrompt(row.TmuxSession, in.InitialPrompt)
	}
	return row, nil
}

// resolveLaunchCommand returns the verbatim command string and its
// tokenised argv. The caller passes both to buildLaunchTmuxOpts so
// the sandbox-wrapping / tmux-spawning code does not have to know
// which source (user --cmd override vs tool launcher) produced
// the command.
func (s *SessionService) resolveLaunchCommand(in CreateInput) (command string, argv []string) {
	if in.Command != "" {
		return in.Command, splitCommandString(in.Command)
	}
	flags := s.flagsFor(in.Tool)
	argv = LaunchArgvForSession(in.Tool, in.WorktreePath, flags, in.ForceContinue)
	return "", argv
}

// buildLaunchTmuxOpts assembles the [tmux.NewSessionOptions] used
// to spawn a session's first pane. It is the single source of
// truth shared by Create and doRespawn so the sandbox-wrap /
// argv-vs-command decision cannot drift between the two paths.
func (s *SessionService) buildLaunchTmuxOpts(
	row store.Session,
	spec sandbox.Spec,
	launchCommand string,
	launchArgv []string,
) tmux.NewSessionOptions {
	env := s.hookEnv(row)
	opts := tmux.NewSessionOptions{
		Name:       row.TmuxSession,
		WorkingDir: row.WorktreePath,
		Env:        env,
	}

	// Docker mode: each token is a separate argv element so tmux
	// execs without a shell.
	if spec.Mode == sandbox.ModeDocker {
		opts.Argv = sandbox.Wrap(spec, sandbox.Input{
			SessionID:  row.ID,
			WorkingDir: row.WorktreePath,
			Env:        env,
			Argv:       launchArgv,
		})
		return opts
	}

	// Host mode: preserve the user-supplied string byte-for-byte
	// when available, fall back to whitespace-joining the
	// synthesised argv.
	if launchCommand != "" {
		opts.Command = launchCommand
	} else if len(launchArgv) > 0 {
		opts.Command = joinArgv(launchArgv)
	}
	return opts
}

// injectInitialPrompt waits long enough for the tool's welcome
// screen to render, then types the prompt into the session's
// active pane and presses Enter. Errors are swallowed — a
// failed injection should not crash the caller, and the log
// captures it for later inspection.
//
// The delay is a best-effort heuristic. Claude Code boots in
// ~800ms on a warm system; shells are instant. Overshooting
// slightly costs nothing but a handful of milliseconds before
// the agent sees its first instruction.
func (s *SessionService) injectInitialPrompt(tmuxSession, prompt string) {
	select {
	case <-time.After(1500 * time.Millisecond):
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.tmux.SendKeys(ctx, tmuxSession, prompt, true)
}

// KillOptions controls optional cleanup behaviour during Kill.
type KillOptions struct {
	// CleanupWorktree, when non-nil, overrides the config-driven
	// auto_cleanup setting for this invocation. true = remove the
	// worktree, false = keep it.
	CleanupWorktree *bool
}

// Kill removes the tmux session and deletes its database row.
//
// When the session was launched in docker mode and the docker
// config has auto_cleanup=true, Kill also issues a best-effort
// `docker rm -f` so a wedged container (one that survived the
// initial `--rm` because the agent did not exit cleanly) is
// not left lying around. Errors are swallowed because the
// cleanup is a hint, not a correctness boundary.
//
// When the effective worktree cleanup decision is true (via
// config or opts override), Kill also removes the git worktree
// from disk. Only worktrees under a `.worktrees/` directory
// are eligible — the main project directory is never deleted.
func (s *SessionService) Kill(ctx context.Context, name string, opts KillOptions) error {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return err
	}
	if err := s.tmux.Kill(ctx, row.TmuxSession); err != nil {
		return fmt.Errorf("tmux kill: %w", err)
	}
	if row.SandboxMode == sandbox.ModeDocker.String() && config.BoolOr(s.Cfg.Docker.AutoCleanup, true) {
		removeSandboxContainer(row.SandboxContainer)
	}
	s.maybeCleanupWorktree(ctx, row, opts)
	if err := s.store.Sessions().Delete(ctx, row.ID); err != nil {
		return err
	}
	if s.bus != nil {
		s.bus.Publish(bus.SessionStopped{SessionID: row.ID, At: time.Now()})
	}
	return nil
}

// worktreePathSegment is the directory component that identifies a
// mosaic-managed worktree. Only paths containing this segment are
// eligible for automatic cleanup — the main project directory is
// never deleted.
const worktreePathSegment = "/.worktrees/"

// maybeCleanupWorktree removes the session's git worktree when the
// effective cleanup decision is true. Safety: only acts when the
// worktree path differs from the project path (i.e., mosaic created
// a separate worktree), AND the path lives under a `.worktrees/`
// directory. Errors are swallowed (best-effort, matching Docker).
func (s *SessionService) maybeCleanupWorktree(ctx context.Context, row store.Session, opts KillOptions) {
	if s.worktree == nil {
		return
	}
	shouldClean := config.BoolOr(s.Cfg.Worktree.AutoCleanup, false)
	if opts.CleanupWorktree != nil {
		shouldClean = *opts.CleanupWorktree
	}
	if !shouldClean {
		return
	}
	if row.WorktreePath == row.ProjectPath {
		return
	}
	if !strings.Contains(row.WorktreePath, worktreePathSegment) {
		return
	}
	_ = s.worktree.Remove(ctx, row.ProjectPath, row.WorktreePath, true)
}

// RespawnResult reports the outcome of a single session respawn attempt.
type RespawnResult struct {
	Name    string
	Success bool
	Err     string // empty on success
}

// doRespawn recreates the tmux session for a database row whose
// backing tmux process has disappeared. It is idempotent: if the
// tmux session already exists, it returns nil immediately.
//
// Both [AttachCommand] and [Respawn] delegate here so the respawn
// logic lives in exactly one place.
func (s *SessionService) doRespawn(ctx context.Context, row store.Session) error {
	exists, err := s.tmux.HasSession(ctx, row.TmuxSession)
	if err != nil {
		return fmt.Errorf("tmux check: %w", err)
	}
	if exists {
		return nil
	}

	// Worktree path must exist before we ask tmux to cd
	// into it, otherwise the spawn fails with an opaque
	// "no such file or directory" and the user is left
	// staring at an unlabelled error.
	if row.WorktreePath != "" {
		if info, statErr := os.Stat(row.WorktreePath); statErr != nil || !info.IsDir() {
			return fmt.Errorf(
				"worktree path does not exist: %s — delete the row with `mosaic kill %s` or recreate the directory",
				row.WorktreePath, row.Name)
		}
	}

	tool := store.SessionTool(row.Tool)
	const respawn = true
	launchArgv := LaunchArgvForSession(tool, row.WorktreePath, s.flagsFor(tool), respawn)

	// Respawn reuses whatever sandbox mode the row was originally
	// created with so the user's "pick up where I left off"
	// intent includes the isolation they opted into. We
	// re-resolve the spec from current config (image, mounts,
	// extra args) so config edits since the first launch take
	// effect on the respawn.
	spec := sandbox.Spec{Mode: sandbox.ModeNone}
	if row.SandboxMode == sandbox.ModeDocker.String() {
		resolved, err := s.resolveSandboxSpec(CreateInput{
			SandboxMode: sandbox.ModeDocker,
			ProjectPath: row.ProjectPath,
		})
		if err != nil {
			return fmt.Errorf("respawn: resolve sandbox: %w", err)
		}
		spec = resolved
	}

	// Respawn has no user-supplied Command string — the launcher
	// always comes from LaunchArgvForSession, so launchCommand
	// is empty and the helper falls through to the argv-join
	// path for the non-docker case.
	tmuxOpts := s.buildLaunchTmuxOpts(row, spec, "", launchArgv)

	// Container names are deterministic (`mosaic-<id>`). If the
	// previous incarnation died without cleanup — e.g. tmux was
	// killed while the container was still running, so `docker
	// run --rm` never fired — the next `docker run` will fail
	// with a name conflict. Issue a best-effort `docker rm -f`
	// before spawning so respawn is self-healing, mirroring what
	// Kill does on the teardown side.
	if spec.Mode == sandbox.ModeDocker {
		removeSandboxContainer(row.SandboxContainer)
	}

	if err := s.tmux.NewSession(ctx, tmuxOpts); err != nil {
		return fmt.Errorf("respawn tmux session %q in %s: %w",
			row.TmuxSession, row.WorktreePath, err)
	}

	// Respawned sessions that were previously "stopped" need
	// to come back as idle, not running — the pane is fresh
	// and nothing is active yet. The reconciler will upgrade
	// them on real activity.
	if row.Status == string(store.StatusStopped) {
		_ = s.store.Sessions().UpdateStatus(ctx, row.ID, store.StatusIdle)
	}
	if s.bus != nil {
		s.bus.Publish(bus.SessionStarted{SessionID: row.ID, At: time.Now()})
	}
	return nil
}

// AttachCommand returns an *exec.Cmd that hands the caller's
// terminal to tmux. Cobra commands set stdin/stdout/stderr to os.*
// and call Run() on the returned command.
//
// If the backing tmux session has disappeared (common after the
// user `/exit`s Claude and the last tmux session dies), mosaic
// silently recreates it at the original worktree path so the user
// can keep working without noticing the restart.
func (s *SessionService) AttachCommand(ctx context.Context, name string) (*exec.Cmd, error) {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := s.doRespawn(ctx, row); err != nil {
		return nil, err
	}
	return s.tmux.AttachCommand(row.TmuxSession), nil
}

// Respawn recreates the tmux session for a stopped session without
// attaching to it. It is idempotent: calling Respawn on a session
// whose tmux backend is already alive is a no-op.
func (s *SessionService) Respawn(ctx context.Context, name string) error {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return err
	}
	return s.doRespawn(ctx, row)
}

// RespawnAll respawns every stopped session, returning per-session
// results. Sessions that are already alive are skipped.
// Unrecoverable sessions (e.g. missing worktree) are reported as
// failures but do not abort the batch.
func (s *SessionService) RespawnAll(ctx context.Context) ([]RespawnResult, error) {
	rows, err := s.store.Sessions().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	var results []RespawnResult
	for _, row := range rows {
		if row.Status != string(store.StatusStopped) {
			continue
		}
		if err := s.doRespawn(ctx, row); err != nil {
			results = append(results, RespawnResult{Name: row.Name, Err: err.Error()})
		} else {
			results = append(results, RespawnResult{Name: row.Name, Success: true})
		}
	}
	return results, nil
}

// ListResult bundles a session row with the live status reconciled
// against tmux.
type ListResult struct {
	Sessions    []store.Session
	TmuxMissing []string // names of sessions whose tmux backend is gone
}

// List returns every session, reconciling stored statuses against the
// current tmux state. Sessions whose tmux backend has disappeared are
// flipped to status="stopped" in the database and reported in
// TmuxMissing so the caller can render a hint.
func (s *SessionService) List(ctx context.Context) (ListResult, error) {
	rows, err := s.store.Sessions().List(ctx)
	if err != nil {
		return ListResult{}, err
	}

	live, err := s.tmux.List(ctx)
	if err != nil {
		// We still return the rows so the user can at least see the
		// list; reconcile is a best-effort operation.
		return ListResult{Sessions: rows}, nil
	}
	liveSet := make(map[string]struct{}, len(live))
	for _, name := range live {
		liveSet[name] = struct{}{}
	}

	var missing []string
	for i := range rows {
		_, alive := liveSet[rows[i].TmuxSession]
		if !alive && rows[i].Status != string(store.StatusStopped) {
			_ = s.store.Sessions().UpdateStatus(ctx, rows[i].ID, store.StatusStopped)
			rows[i].Status = string(store.StatusStopped)
			missing = append(missing, rows[i].Name)
		}
	}
	return ListResult{Sessions: rows, TmuxMissing: missing}, nil
}

// GetByName fetches a single session by user-visible name.
func (s *SessionService) GetByName(ctx context.Context, name string) (store.Session, error) {
	return s.store.Sessions().GetByName(ctx, name)
}

// flagsFor returns the configured launch flags for the given
// tool, or the zero value (no extra flags) when the user has
// not added a [mosaic.tools.<tool>] section to their config.
func (s *SessionService) flagsFor(tool store.SessionTool) config.ToolLaunchFlags {
	if s.Cfg.ToolFlags == nil {
		return config.ToolLaunchFlags{}
	}
	if f, ok := s.Cfg.ToolFlags[string(tool)]; ok {
		return f
	}
	return config.ToolLaunchFlags{}
}

// hookEnv returns the MOSAIC_* environment map that has to land in
// the tmux session so installed Claude Code hooks can report back.
//
// The keys mirror supacode's wire format: worktree/tab/surface ids
// plus the socket path. mosaic's "tab" and "surface" concepts are
// not split yet — one mosaic session maps to one tmux session — so
// TAB_ID reuses the session id and SURFACE_ID is a constant "tmux".
// Both can become real identifiers later without changing the hook
// command strings, because the server parses the header blindly.
//
// When HookSocketPath is empty the function returns nil: there is no
// socket to report to, so there is no point polluting the tmux env.
func (s *SessionService) hookEnv(row store.Session) map[string]string {
	if s.Cfg.HookSocketPath == "" {
		return nil
	}
	return map[string]string{
		"MOSAIC_SOCKET_PATH": s.Cfg.HookSocketPath,
		"MOSAIC_WORKTREE_ID": row.ID,
		"MOSAIC_TAB_ID":      row.ID,
		"MOSAIC_SURFACE_ID":  "tmux",
	}
}

// DiffResult carries the file-level summary the Diff View shows in
// its left pane.
type DiffResult struct {
	Session store.Session
	BaseRef string
	Files   []git.DiffFileStat
}

// Diff returns the list of files that have changed in a session's
// worktree against the configured base branch. It is purely a
// read operation and never mutates the repo.
//
// The git client's resolveDiffBase chain transparently falls
// back when the configured base does not exist in the repo, so
// imported / freshly-cloned worktrees never crash with an
// "ambiguous argument" error. The actually-used ref is reported
// back through DiffResult.BaseRef so the Diff View footer can
// label what is being compared.
func (s *SessionService) Diff(ctx context.Context, name string) (DiffResult, error) {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return DiffResult{}, err
	}
	base := s.Cfg.DiffBaseBranch
	if base == "" {
		base = "main"
	}
	files, err := s.git.DiffAgainst(ctx, row.WorktreePath, base)
	if err != nil {
		return DiffResult{}, fmt.Errorf("diff %s: %w", name, err)
	}
	// Re-resolve so the displayed BaseRef reflects what git
	// actually used after the fallback chain.
	if resolved, err := s.git.ResolveDiffBase(ctx, row.WorktreePath, base); err == nil {
		base = resolved
	}
	return DiffResult{Session: row, BaseRef: base, Files: files}, nil
}

// DiffFile returns the unified diff for one file in a session's
// worktree. Untracked files are rendered via [git.Client.ShowFile]
// so the caller gets a consistent unified-diff-shaped blob to
// display.
func (s *SessionService) DiffFile(ctx context.Context, name, path string, untracked bool) (string, error) {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return "", err
	}
	base := s.Cfg.DiffBaseBranch
	if base == "" {
		base = "main"
	}
	if untracked {
		return s.git.ShowFile(ctx, row.WorktreePath, path)
	}
	return s.git.DiffFile(ctx, row.WorktreePath, base, path)
}

// StageFile stages path in the given session's worktree. It is a
// thin wrapper that keeps the Diff View from needing its own git
// client.
func (s *SessionService) StageFile(ctx context.Context, name, path string) error {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return err
	}
	return s.git.StageFile(ctx, row.WorktreePath, path)
}

// StageHunk stages a single hunk in the session's worktree.
// The preamble / hunk pair should come from
// [git.ParseDiffHunks] applied to the same file's unified diff.
func (s *SessionService) StageHunk(ctx context.Context, name, preamble string, hunk git.DiffHunk) error {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return err
	}
	return s.git.ApplyHunk(ctx, row.WorktreePath, preamble, hunk)
}

// UnstageFile unstages path in the given session's worktree.
func (s *SessionService) UnstageFile(ctx context.Context, name, path string) error {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return err
	}
	return s.git.UnstageFile(ctx, row.WorktreePath, path)
}

// IsStaged reports whether path has staged changes in session's
// worktree — used by the Diff View so `s` knows which direction
// to toggle.
func (s *SessionService) IsStaged(ctx context.Context, name, path string) (bool, error) {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return false, err
	}
	return s.git.IsStaged(ctx, row.WorktreePath, path)
}

// Commit creates a new commit in session's worktree with the
// supplied message. The Diff View's inline commit modal is its
// primary caller.
func (s *SessionService) Commit(ctx context.Context, name, message string) error {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return err
	}
	return s.git.Commit(ctx, row.WorktreePath, message)
}

// CommitAmend runs `git commit --amend -m <message>` in the
// session's worktree. Used by the Diff View's capital-C path.
func (s *SessionService) CommitAmend(ctx context.Context, name, message string) error {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return err
	}
	return s.git.CommitAmend(ctx, row.WorktreePath, message)
}

// LastCommitMessage returns the HEAD commit message for a
// session's worktree — used to pre-fill the amend modal.
func (s *SessionService) LastCommitMessage(ctx context.Context, name string) (string, error) {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return "", err
	}
	return s.git.LastCommitMessage(ctx, row.WorktreePath)
}

// Fork creates a sibling session that branches off from the given
// one: the source session's tool and project path are inherited,
// a new worktree is provisioned under <project>/.worktrees/<name>,
// and a fresh branch is cut from the source's current branch so
// the new workspace starts from the same commit.
//
// The source session is NOT touched. Fork returns the newly
// created session row (with the usual Create side effects: tmux
// spawned, SessionStarted published).
//
// If sourceName does not refer to an existing session, Fork
// returns ErrSessionNotFound.
func (s *SessionService) Fork(ctx context.Context, sourceName string) (store.Session, error) {
	src, err := s.store.Sessions().GetByName(ctx, sourceName)
	if err != nil {
		return store.Session{}, err
	}
	newName, err := s.uniqueForkName(ctx, src.Name)
	if err != nil {
		return store.Session{}, err
	}
	newBranch, err := s.uniqueForkBranch(ctx, src.ProjectPath, src.Name)
	if err != nil {
		return store.Session{}, err
	}

	// Derive a worktree path sibling to the source worktree. The
	// path is rooted under <project>/.worktrees/ so the repo
	// stays tidy, matching the convention the conductor runner
	// uses for auto-created worktrees.
	worktreePath := filepath.Join(src.ProjectPath, ".worktrees", newName)

	baseRef := ""
	if src.Branch.Valid {
		baseRef = src.Branch.String
	}
	if baseRef == "" {
		// Source has no branch recorded (e.g. ran inside an
		// already-checked-out repo). Fall back to the current
		// branch of the source's worktree.
		if ref, gerr := s.git.CurrentBranch(ctx, src.WorktreePath); gerr == nil {
			baseRef = ref
		}
	}

	return s.Create(ctx, CreateInput{
		Name:            newName,
		ProjectPath:     src.ProjectPath,
		WorktreePath:    worktreePath,
		Branch:          newBranch,
		Tool:            store.SessionTool(src.Tool),
		CreateWorktree:  true,
		WorktreeRepo:    src.ProjectPath,
		WorktreeBaseRef: baseRef,
	})
}

// uniqueForkName finds a session name that does not yet exist in
// the store by appending "-fork", "-fork2", etc. to the source
// name. 32 is a sanity cap so a bug cannot lock us into an
// infinite loop.
func (s *SessionService) uniqueForkName(ctx context.Context, source string) (string, error) {
	for i := 1; i <= 32; i++ {
		candidate := source + "-fork"
		if i > 1 {
			candidate = fmt.Sprintf("%s-fork%d", source, i)
		}
		_, err := s.store.Sessions().GetByName(ctx, candidate)
		if errors.Is(err, store.ErrSessionNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("fork: could not find an unused name after 32 tries")
}

// uniqueForkBranch picks a git branch name for the fork that does
// not already exist in the source repository.
func (s *SessionService) uniqueForkBranch(ctx context.Context, repo, source string) (string, error) {
	for i := 1; i <= 32; i++ {
		candidate := "fork/" + source
		if i > 1 {
			candidate = fmt.Sprintf("fork/%s-%d", source, i)
		}
		exists, err := s.git.BranchExists(ctx, repo, candidate)
		if err != nil {
			// Repo may not exist / be a git worktree (pure shell
			// session). Fall through with the candidate since
			// Create will fail later with a clearer error.
			return candidate, nil
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", errors.New("fork: could not find an unused branch after 32 tries")
}

// CapturePane returns the most recent maxLines lines rendered in
// the tmux pane backing the given session. It is how the TUI's
// preview pane shows live Claude output alongside the structured
// metadata.
//
// The capture always includes terminal escape sequences (the `-e`
// flag to tmux capture-pane) so that mosaic's OSC attention scanner
// can see OSC 777 / OSC 9;4 sequences the agent has emitted. Display
// code that renders the capture as text should strip ANSI with the
// helpers in internal/tui before laying it out.
//
// An empty string is returned (with no error) when the session has
// no live tmux pane yet — common during the brief window between
// session creation and the first render by tmux.
func (s *SessionService) CapturePane(ctx context.Context, name string, maxLines int) (string, error) {
	row, err := s.store.Sessions().GetByName(ctx, name)
	if err != nil {
		return "", err
	}
	out, err := s.tmux.Capture(ctx, row.TmuxSession, tmux.CaptureOptions{
		MaxLines: maxLines,
		Escapes:  true,
	})
	if err != nil && tmux.IsNotFound(err) {
		return "", nil
	}
	return out, err
}
