// Package features contains the Godog (Cucumber for Go) end-to-end
// scenarios that act as the executable Definition of Done for each
// phase. The .feature files in this directory are run by `go test`
// against the actual mosaic binary built by `make build`.
package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cucumber/godog"
	_ "modernc.org/sqlite"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/claudeio"
)

// world holds the per-scenario state shared between step definitions.
// godog gives us a fresh Context per scenario, and we attach a *world
// to it via [withWorld] / [worldFromContext].
type world struct {
	t           *testing.T
	binary      string
	home        string
	projectDir  string
	socket      string // unique tmux -L socket name for this scenario
	repoDir     string // path to the per-scenario git repo (for worktree scenarios)
	planPath    string // path to the per-scenario conductor plan
	lastStdout  string
	lastStderr  string
	lastExit    int
	lastErr     error
	globalToml  []byte
	projectToml []byte

	// claudeio scenarios
	claudeHome    string
	claudeProject string
	claudeSession string
	bus           *bus.Bus
	watcher       *claudeio.Watcher
	stateMachine  *claudeio.StateMachine
	wEvents       []claudeio.TranscriptEntry
	wSpawned      []claudeio.SubagentSpawned
	wMu           sync.Mutex
}

// cleanupTmux kills any tmux server that the scenario may have spawned
// against the scenario's isolated socket name. It is invoked from the
// godog After hook so a failing scenario does not leave processes
// around.
func (w *world) cleanupTmux() {
	if w == nil || w.socket == "" {
		return
	}
	cmd := exec.Command("tmux", "-L", w.socket, "kill-server")
	_ = cmd.Run()
}

// cleanupWatcher tears down any claudeio watcher that the scenario
// started. Called from the After hook.
func (w *world) cleanupWatcher() {
	if w == nil {
		return
	}
	if w.watcher != nil {
		w.watcher.Stop()
		w.watcher = nil
	}
	if w.bus != nil {
		w.bus.Close()
		w.bus = nil
	}
}

type worldKey struct{}

func withWorld(ctx context.Context, w *world) context.Context {
	return context.WithValue(ctx, worldKey{}, w)
}

func worldFromContext(ctx context.Context) *world {
	w, ok := ctx.Value(worldKey{}).(*world)
	if !ok {
		panic("no world in context — did the scenario register a hook?")
	}
	return w
}

// TestFeatures is the entry point invoked by `go test ./features/...`.
// It builds the mosaic binary, then runs every .feature file in this
// directory.
func TestFeatures(t *testing.T) {
	binary := buildBinary(t)

	suite := godog.TestSuite{
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			initSteps(s, binary, t)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"."},
			TestingT: t,
		},
	}
	if status := suite.Run(); status != 0 {
		t.Fatalf("godog suite failed (exit %d)", status)
	}
}

// buildBinary compiles a fresh mosaic binary into a temp dir for the
// duration of the test run.
func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "mosaic")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, "../cmd/mosaic")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build mosaic: %v", err)
	}
	return out
}

func initSteps(s *godog.ScenarioContext, binary string, t *testing.T) {
	s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w := &world{t: t, binary: binary}
		return withWorld(ctx, w), nil
	})

	// Each scenario that touches real tmux uses an isolated socket
	// name (`mosaic-test-<rand>`) so parallel runs do not collide and
	// the After hook can wipe everything.
	s.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w := worldFromContext(ctx)
		w.cleanupTmux()
		w.cleanupWatcher()
		return ctx, nil
	})

	s.Step(`^a clean mosaic data directory$`, aCleanMosaicDataDirectory)
	s.Step(`^a global config that sets default_tool to "([^"]*)"$`, aGlobalConfigSetsDefaultTool)
	s.Step(`^a project config that sets default_tool to "([^"]*)"$`, aProjectConfigSetsDefaultTool)
	s.Step(`^a git repository with one commit$`, aGitRepoWithOneCommit)
	s.Step(`^a git repository with one commit and a \.env file$`, aGitRepoWithEnvFile)
	s.Step(`^a conductor plan with three sequential tasks$`, aSequentialConductorPlan)
	s.Step(`^a conductor plan that contains a cycle$`, aCyclicConductorPlan)
	s.Step(`^the worktree directory should exist for branch "([^"]*)"$`, theWorktreeDirExists)
	s.Step(`^the worktree should contain a copy of \.env$`, theWorktreeHasEnv)

	// Claude transcript / sub-agent scenarios.
	s.Step(`^a clean Claude transcript directory for project "([^"]*)" session "([^"]*)"$`, aCleanClaudeDir)
	s.Step(`^the watcher is started for project "([^"]*)" session "([^"]*)"$`, theWatcherIsStartedFor)
	s.Step(`^a sub-agent transcript "([^"]*)" is appended with one tool_use$`, aSubagentTranscriptIsAppended)
	s.Step(`^a SubagentSpawned event should arrive within (\d+) seconds?$`, aSpawnedEventShouldArrive)
	s.Step(`^a TranscriptEntry event should arrive within (\d+) seconds?$`, aTranscriptEntryShouldArrive)
	s.Step(`^a fresh state machine$`, aFreshStateMachine)
	s.Step(`^a tool_use "([^"]*)" with id "([^"]*)" is observed$`, aToolUseIsObserved)
	s.Step(`^a tool_result for "([^"]*)" is observed$`, aToolResultIsObserved)
	s.Step(`^a Notification "([^"]*)" is observed$`, aNotificationIsObserved)
	s.Step(`^the snapshot should report "([^"]*)" as the current tool$`, snapshotCurrentToolShouldBe)
	s.Step(`^the snapshot should report no current tool$`, snapshotShouldHaveNoCurrentTool)
	s.Step(`^the snapshot should be awaiting input$`, snapshotShouldBeAwaitingInput)
	s.Step(`^the snapshot should not be awaiting input$`, snapshotShouldNotBeAwaitingInput)
	s.Step(`^I run "mosaic ([^"]+)"$`, iRunMosaic)
	s.Step(`^it should exit with status (\d+)$`, itShouldExitWithStatus)
	s.Step(`^the output should contain "([^"]*)"$`, theOutputShouldContain)
	s.Step(`^the data directory should exist$`, theDataDirectoryShouldExist)
	s.Step(`^the SQLite database should exist$`, theSQLiteDatabaseShouldExist)
	s.Step(`^the sessions table should contain a row named "([^"]*)"$`, theSessionsTableHasRowNamed)
	s.Step(`^the schema_migrations table should contain version (\d+) exactly once$`, theSchemaMigrationsHasVersionOnce)
}

// --- step definitions -----------------------------------------------------

func aCleanMosaicDataDirectory(ctx context.Context) (context.Context, error) {
	w := worldFromContext(ctx)
	w.home = w.t.TempDir()
	w.projectDir = w.t.TempDir()
	// Use a unique tmux socket per scenario so cleanup is total and
	// scenarios cannot interfere with each other or with the user's
	// real mosaic socket.
	w.socket = fmt.Sprintf("mosaic-test-%d", time.Now().UnixNano())
	// Pass the unique socket name to mosaic via a global config that
	// the runtime picks up automatically.
	cfgDir := filepath.Join(w.home, ".config", "mosaic")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return ctx, err
	}
	if err := os.WriteFile(
		filepath.Join(cfgDir, "config.toml"),
		[]byte(fmt.Sprintf("[tmux]\nsocket_name = %q\n", w.socket)),
		0o644,
	); err != nil {
		return ctx, err
	}
	return ctx, nil
}

func aGlobalConfigSetsDefaultTool(ctx context.Context, tool string) (context.Context, error) {
	w := worldFromContext(ctx)
	dir := filepath.Join(w.home, ".config", "mosaic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ctx, err
	}
	body := fmt.Sprintf("[mosaic]\ndefault_tool = %q\n", tool)
	w.globalToml = []byte(body)
	return ctx, os.WriteFile(filepath.Join(dir, "config.toml"), w.globalToml, 0o644)
}

func aProjectConfigSetsDefaultTool(ctx context.Context, tool string) (context.Context, error) {
	w := worldFromContext(ctx)
	dir := filepath.Join(w.projectDir, ".mosaic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ctx, err
	}
	body := fmt.Sprintf("[mosaic]\ndefault_tool = %q\n", tool)
	w.projectToml = []byte(body)
	return ctx, os.WriteFile(filepath.Join(dir, "config.toml"), w.projectToml, 0o644)
}

func iRunMosaic(ctx context.Context, args string) (context.Context, error) {
	w := worldFromContext(ctx)

	parts := strings.Fields(args)
	// Substitute scenario sentinels so the scenario text stays
	// human readable while still pointing at per-scenario temp dirs.
	for i, p := range parts {
		switch p {
		case "REPO_DIR":
			parts[i] = w.repoDir
		case "PLAN_FILE":
			parts[i] = w.planPath
		}
	}
	parts = append(parts, "--home", w.home, "--project", w.projectDir)
	cmd := exec.Command(w.binary, parts...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	w.lastErr = cmd.Run()
	w.lastStdout = stdout.String()
	w.lastStderr = stderr.String()
	w.lastExit = 0
	if w.lastErr != nil {
		var ee *exec.ExitError
		if errors.As(w.lastErr, &ee) {
			w.lastExit = ee.ExitCode()
		} else {
			w.lastExit = -1
		}
	}
	return ctx, nil
}

func itShouldExitWithStatus(ctx context.Context, want int) error {
	w := worldFromContext(ctx)
	if w.lastExit != want {
		return fmt.Errorf("expected exit %d, got %d\nstdout:\n%s\nstderr:\n%s",
			want, w.lastExit, w.lastStdout, w.lastStderr)
	}
	return nil
}

func theOutputShouldContain(ctx context.Context, substr string) error {
	w := worldFromContext(ctx)
	combined := w.lastStdout + w.lastStderr
	if !strings.Contains(combined, substr) {
		return fmt.Errorf("expected output to contain %q, got:\n%s", substr, combined)
	}
	return nil
}

func theDataDirectoryShouldExist(ctx context.Context) error {
	w := worldFromContext(ctx)
	dir := filepath.Join(w.home, ".mosaic")
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("expected %s to exist: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return nil
}

func theSQLiteDatabaseShouldExist(ctx context.Context) error {
	w := worldFromContext(ctx)
	db := filepath.Join(w.home, ".mosaic", "profiles", "default", "state.db")
	if _, err := os.Stat(db); err != nil {
		return fmt.Errorf("expected %s to exist: %w", db, err)
	}
	return nil
}

func theSessionsTableHasRowNamed(ctx context.Context, name string) error {
	w := worldFromContext(ctx)
	dbPath := filepath.Join(w.home, ".mosaic", "profiles", "default", "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sessions WHERE name = ?`, name).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("expected one row named %q, got %d", name, count)
	}
	return nil
}

// aGitRepoWithOneCommit initialises a fresh git repo with a single
// commit so worktree scenarios have a base ref to branch from.
func aGitRepoWithOneCommit(ctx context.Context) (context.Context, error) {
	w := worldFromContext(ctx)
	w.repoDir = w.t.TempDir()
	if err := initRepo(w.repoDir); err != nil {
		return ctx, err
	}
	return ctx, nil
}

func aGitRepoWithEnvFile(ctx context.Context) (context.Context, error) {
	ctx, err := aGitRepoWithOneCommit(ctx)
	if err != nil {
		return ctx, err
	}
	w := worldFromContext(ctx)
	return ctx, os.WriteFile(filepath.Join(w.repoDir, ".env"), []byte("FOO=bar\n"), 0o644)
}

func initRepo(dir string) error {
	for _, args := range [][]string{
		{"init", "-q", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		full := append([]string{"-C", dir}, args...)
		if err := exec.Command("git", full...).Run(); err != nil {
			return fmt.Errorf("git %v: %w", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "-q", "-m", "init"},
	} {
		full := append([]string{"-C", dir}, args...)
		cmd := exec.Command("git", full...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git %v: %w", args, err)
		}
	}
	return nil
}

func theWorktreeDirExists(ctx context.Context, branch string) error {
	w := worldFromContext(ctx)
	path := filepath.Join(w.repoDir, ".worktrees", branch)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("worktree %s missing: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func theWorktreeHasEnv(ctx context.Context) error {
	w := worldFromContext(ctx)
	matches, err := filepath.Glob(filepath.Join(w.repoDir, ".worktrees", "*", ".env"))
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return fmt.Errorf(".env not found in any worktree under %s", w.repoDir)
	}
	return nil
}

// --- conductor steps ---

func aSequentialConductorPlan(ctx context.Context) (context.Context, error) {
	w := worldFromContext(ctx)
	dir := w.t.TempDir()
	w.planPath = filepath.Join(dir, "plan.toml")
	body := `[conductor]
name = "triple"
project = "."

[[task]]
name = "scaffold"
prompt = "do scaffold"

[[task]]
name = "tests"
depends_on = ["scaffold"]
prompt = "run tests"

[[task]]
name = "docs"
depends_on = ["tests"]
prompt = "write docs"
`
	return ctx, os.WriteFile(w.planPath, []byte(body), 0o644)
}

func aCyclicConductorPlan(ctx context.Context) (context.Context, error) {
	w := worldFromContext(ctx)
	dir := w.t.TempDir()
	w.planPath = filepath.Join(dir, "plan.toml")
	body := `[conductor]
name = "loop"

[[task]]
name = "a"
depends_on = ["b"]
prompt = "x"

[[task]]
name = "b"
depends_on = ["a"]
prompt = "y"
`
	return ctx, os.WriteFile(w.planPath, []byte(body), 0o644)
}

// --- claudeio steps ---

func aCleanClaudeDir(ctx context.Context, project, session string) (context.Context, error) {
	w := worldFromContext(ctx)
	w.claudeHome = w.t.TempDir()
	w.claudeProject = "/Users/test/" + project
	w.claudeSession = session
	dir := claudeio.ProjectDir(w.claudeHome, w.claudeProject)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ctx, err
	}
	parent := claudeio.SessionTranscriptPath(w.claudeHome, w.claudeProject, w.claudeSession)
	body := fmt.Sprintf(`{"type":"system","uuid":"sys-1","sessionId":%q}`+"\n", w.claudeSession)
	return ctx, os.WriteFile(parent, []byte(body), 0o644)
}

func theWatcherIsStartedFor(ctx context.Context, project, session string) (context.Context, error) {
	w := worldFromContext(ctx)
	w.bus = bus.New()
	w.bus.Subscribe(func(e bus.Event) {
		w.wMu.Lock()
		defer w.wMu.Unlock()
		switch v := e.(type) {
		case claudeio.TranscriptEntry:
			w.wEvents = append(w.wEvents, v)
		case claudeio.SubagentSpawned:
			w.wSpawned = append(w.wSpawned, v)
		}
	})
	w.watcher = claudeio.NewWatcher(w.bus, w.claudeHome)
	if err := w.watcher.Start(context.Background()); err != nil {
		return ctx, err
	}
	if err := w.watcher.Watch(w.claudeProject, w.claudeSession); err != nil {
		return ctx, err
	}
	return ctx, nil
}

func aSubagentTranscriptIsAppended(ctx context.Context, agentID string) (context.Context, error) {
	w := worldFromContext(ctx)
	subDir := claudeio.SubagentDir(w.claudeHome, w.claudeProject, w.claudeSession)
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return ctx, err
	}
	path := filepath.Join(subDir, "agent-"+agentID+".jsonl")
	body := fmt.Sprintf(
		`{"type":"assistant","uuid":"u1","sessionId":%q,"agentId":%q,"isSidechain":true,"message":{"role":"assistant","content":[{"type":"tool_use","id":"id1","name":"Read","input":{"path":"src/foo.ts"}}]}}`+"\n",
		w.claudeSession, agentID,
	)
	return ctx, os.WriteFile(path, []byte(body), 0o644)
}

func aSpawnedEventShouldArrive(ctx context.Context, secs int) error {
	w := worldFromContext(ctx)
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	for time.Now().Before(deadline) {
		w.wMu.Lock()
		n := len(w.wSpawned)
		w.wMu.Unlock()
		if n > 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("no SubagentSpawned within %ds", secs)
}

func aTranscriptEntryShouldArrive(ctx context.Context, secs int) error {
	w := worldFromContext(ctx)
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	for time.Now().Before(deadline) {
		w.wMu.Lock()
		n := len(w.wEvents)
		w.wMu.Unlock()
		if n > 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("no TranscriptEntry within %ds", secs)
}

// --- state machine steps ---

func aFreshStateMachine(ctx context.Context) (context.Context, error) {
	w := worldFromContext(ctx)
	w.stateMachine = claudeio.NewStateMachine()
	return ctx, nil
}

func aToolUseIsObserved(ctx context.Context, name, id string) (context.Context, error) {
	w := worldFromContext(ctx)
	w.stateMachine.Observe(buildToolUseEntry(id, name))
	return ctx, nil
}

func aToolResultIsObserved(ctx context.Context, id string) (context.Context, error) {
	w := worldFromContext(ctx)
	w.stateMachine.Observe(buildToolResultEntry(id))
	return ctx, nil
}

func aNotificationIsObserved(ctx context.Context, title string) (context.Context, error) {
	w := worldFromContext(ctx)
	w.stateMachine.ObserveNotification(title, "")
	return ctx, nil
}

func snapshotCurrentToolShouldBe(ctx context.Context, name string) error {
	w := worldFromContext(ctx)
	got := w.stateMachine.Snapshot().CurrentTool
	if got != name {
		return fmt.Errorf("current tool = %q, want %q", got, name)
	}
	return nil
}

func snapshotShouldHaveNoCurrentTool(ctx context.Context) error {
	w := worldFromContext(ctx)
	if got := w.stateMachine.Snapshot().CurrentTool; got != "" {
		return fmt.Errorf("expected no current tool, got %q", got)
	}
	return nil
}

func snapshotShouldBeAwaitingInput(ctx context.Context) error {
	w := worldFromContext(ctx)
	if !w.stateMachine.Snapshot().AwaitingInput {
		return fmt.Errorf("expected awaiting input")
	}
	return nil
}

func snapshotShouldNotBeAwaitingInput(ctx context.Context) error {
	w := worldFromContext(ctx)
	if w.stateMachine.Snapshot().AwaitingInput {
		return fmt.Errorf("expected NOT awaiting input")
	}
	return nil
}

func theSchemaMigrationsHasVersionOnce(ctx context.Context, version int) error {
	w := worldFromContext(ctx)
	dbPath := filepath.Join(w.home, ".mosaic", "profiles", "default", "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	var count int
	row := db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = ?`, version)
	if err := row.Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("expected exactly one row for version %d, got %d", version, count)
	}
	return nil
}
