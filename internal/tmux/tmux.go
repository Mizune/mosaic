package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Backend is the surface that the session service depends on. The
// production implementation is [*CLI]; tests can supply a fake.
type Backend interface {
	// HasSession reports whether a tmux session with the given name
	// already exists on this backend's socket.
	HasSession(ctx context.Context, name string) (bool, error)

	// NewSession creates a detached tmux session with the given name
	// and starting directory. The optional command is executed in the
	// initial window; an empty string starts a login shell.
	NewSession(ctx context.Context, opts NewSessionOptions) error

	// Kill removes the session. It is not an error to kill a session
	// that has already disappeared.
	Kill(ctx context.Context, name string) error

	// List returns the names of every session on this backend's socket.
	List(ctx context.Context) ([]string, error)

	// Capture returns the rendered text of the session's current pane,
	// optionally including scrollback history.
	Capture(ctx context.Context, name string, opts CaptureOptions) (string, error)

	// AttachCommand returns an *exec.Cmd that, when executed, hands the
	// caller's terminal to tmux. The session service exposes this so
	// that the cobra command can call cmd.Run() at the right moment.
	AttachCommand(name string) *exec.Cmd

	// SendKeys injects literal keystrokes into the given session's
	// active pane. Conductor messaging uses this to deliver an
	// inter-agent note by typing it into the receiving agent's
	// prompt.
	SendKeys(ctx context.Context, target, keys string, enterAfter bool) error
}

// NewSessionOptions configures [Backend.NewSession].
type NewSessionOptions struct {
	// Name is the tmux session name (typically `mos_<id>`).
	Name string

	// WorkingDir is the directory tmux launches the first pane in.
	WorkingDir string

	// Command is the shell command to run in the first pane. Empty
	// means "use the user's default shell". Ignored when Argv is
	// non-nil.
	Command string

	// Argv, when non-empty, is passed as one trailing positional
	// arg per element to `tmux new-session`, so tmux execs the
	// binary directly without going through a shell. Use this for
	// commands whose arguments contain spaces or shell
	// metacharacters (e.g. a `docker run ...` invocation with
	// volume bind specs). When Argv is set it takes precedence
	// over Command.
	Argv []string

	// Width and Height set the initial window dimensions. Zero values
	// fall back to tmux defaults.
	Width  int
	Height int

	// Env is exported into the new session's environment. Existing
	// environment values are preserved unless overridden.
	Env map[string]string
}

// CaptureOptions configures [Backend.Capture].
type CaptureOptions struct {
	// History includes scrollback history, not just the visible pane.
	History bool

	// MaxLines limits the captured region to the last N lines. Zero
	// means "no limit".
	MaxLines int

	// Escapes keeps the terminal escape sequences (colour, OSC,
	// etc.) in the captured output instead of stripping them. This
	// maps to `tmux capture-pane -e`. mosaic's attention scanner
	// needs this to detect OSC 777 / OSC 9;4 sequences; display
	// code can still strip ANSI before rendering.
	Escapes bool
}

// CLI is the production tmux backend backed by `tmux -L mosaic`.
type CLI struct {
	// Socket overrides the value passed to `tmux -L`. The zero value
	// uses [DefaultSocket].
	Socket string

	// Binary overrides the tmux binary path. Empty looks up `tmux`
	// in $PATH.
	Binary string

	// ConfigFile, when non-empty, is passed to every tmux invocation
	// as `-f <path>` so every session mosaic creates loads the
	// mosaic-managed tmux config (Ctrl+Q detach binding etc.).
	ConfigFile string
}

// New returns a default CLI bound to the mosaic socket.
func New() *CLI { return &CLI{} }

// HasSession runs `tmux has-session -t <name>`. Both "session
// not found" and "no tmux server is running at all" are
// reported as ok=false with nil error — from the caller's
// perspective they mean the same thing ("the pane I want does
// not exist"), and forcing them to branch on the raw errors
// leaks tmux implementation details.
func (c *CLI) HasSession(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return false, errors.New("tmux: session name is required")
	}
	_, err := c.run(ctx, "has-session", "-t", name)
	if err == nil {
		return true, nil
	}
	if IsNotFound(err) {
		return false, nil
	}
	return false, err
}

// NewSession runs `tmux new-session -d -s <name> -c <dir> [command]`.
//
// Every entry in opts.Env is passed as `-e KEY=VALUE`, which tmux
// treats as a session-scoped environment override. This is the only
// way to get variables like MOSAIC_SOCKET_PATH into the shell (and any
// Claude Code subprocess) that tmux launches inside the new session —
// setting them on the `tmux` subprocess itself would only affect the
// parent call and would be lost before the inner shell starts.
func (c *CLI) NewSession(ctx context.Context, opts NewSessionOptions) error {
	if opts.Name == "" {
		return errors.New("tmux: session name is required")
	}
	args := []string{"new-session", "-d", "-s", opts.Name}
	if opts.WorkingDir != "" {
		args = append(args, "-c", opts.WorkingDir)
	}
	if opts.Width > 0 && opts.Height > 0 {
		args = append(args, "-x", fmt.Sprintf("%d", opts.Width), "-y", fmt.Sprintf("%d", opts.Height))
	}
	// Sort env keys so the resulting argv is deterministic — makes
	// tests and logs comparable across runs.
	if len(opts.Env) > 0 {
		keys := make([]string, 0, len(opts.Env))
		for k := range opts.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "-e", k+"="+opts.Env[k])
		}
	}
	// Argv wins over Command so callers passing a pre-built argv
	// (e.g. the sandbox wrapper emitting `docker run ...`) do not
	// have to shell-quote their pieces. tmux treats the trailing
	// positional args as the command to exec.
	if len(opts.Argv) > 0 {
		args = append(args, opts.Argv...)
	} else if opts.Command != "" {
		args = append(args, opts.Command)
	}
	if _, err := c.run(ctx, args...); err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}
	return nil
}

// SourceConfig reloads the mosaic tmux config into the currently
// running server. tmux loads `-f <file>` only on server start, so
// calling this is the only way to get a new binding (e.g. Ctrl+Q
// detach) into a server that already exists from a previous mosaic
// invocation.
//
// The call is a no-op when there is no config file configured and
// when the server does not exist yet — in both cases the desired
// state is already satisfied: the config will be loaded next time
// [CLI.NewSession] starts the server via its -f argument.
func (c *CLI) SourceConfig(ctx context.Context) error {
	if c.ConfigFile == "" {
		return nil
	}
	_, err := c.run(ctx, "source-file", c.ConfigFile)
	if err == nil || IsNotFound(err) {
		return nil
	}
	return err
}

// KillServer runs `tmux kill-server` against this backend's socket.
// It is the nuclear option `mosaic stop` reaches for: every session
// on the socket goes away and the server process exits.
//
// A missing server is treated as success because "nothing to kill"
// already satisfies the caller's intent.
func (c *CLI) KillServer(ctx context.Context) error {
	_, err := c.run(ctx, "kill-server")
	if err == nil || IsNotFound(err) {
		return nil
	}
	return err
}

// Kill runs `tmux kill-session -t <name>`. A missing session — or a
// missing tmux server entirely — is treated as success: the desired
// state ("session is gone") already holds.
func (c *CLI) Kill(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("tmux: session name is required")
	}
	_, err := c.run(ctx, "kill-session", "-t", name)
	if err == nil || IsNotFound(err) {
		return nil
	}
	return err
}

// List runs `tmux list-sessions -F #S` and returns the session names.
// An empty list is returned with no error when no sessions exist.
func (c *CLI) List(ctx context.Context) ([]string, error) {
	out, err := c.run(ctx, "list-sessions", "-F", "#S")
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	out2 := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out2 = append(out2, l)
		}
	}
	return out2, nil
}

// Capture runs `tmux capture-pane -p [-S -<n>] -t <name>`. A
// missing session or a missing tmux server both yield ("", nil)
// because they are expected states during refresh polls.
func (c *CLI) Capture(ctx context.Context, name string, opts CaptureOptions) (string, error) {
	if name == "" {
		return "", errors.New("tmux: session name is required")
	}
	args := []string{"capture-pane", "-p", "-t", name}
	if opts.Escapes {
		args = append(args, "-e")
	}
	if opts.History {
		// `-S -` means "from the start of the scrollback".
		args = append(args, "-S", "-")
	} else if opts.MaxLines > 0 {
		args = append(args, "-S", fmt.Sprintf("-%d", opts.MaxLines))
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		if IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return out, nil
}

// AttachCommand returns an *exec.Cmd that hands the calling terminal to
// tmux. Callers are expected to wire stdin/stdout/stderr to os.* and
// then call Run.
func (c *CLI) AttachCommand(name string) *exec.Cmd {
	args := append(c.baseArgs(), "attach-session", "-t", name)
	return exec.Command(c.binary(), args...)
}

// run executes a tmux command and returns its stdout. stderr is parsed
// for the well-known "no server / no session" markers and translated
// into the typed sentinels.
func (c *CLI) run(ctx context.Context, args ...string) (string, error) {
	cmd := c.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if sentinel := classifyStderr(stderr.String()); sentinel != nil {
			return "", sentinel
		}
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// classifyStderr maps tmux's stderr to a sentinel error, or nil when
// the failure is not a "no server / no session" state.
//
// Newer tmux (seen on 3.8) reports a missing server as
// "error connecting to <socket> (No such file or directory)" instead
// of "no server running", which is exactly what happens after a
// reboot wipes the socket directory. A stale socket left by a crashed
// server shows up as "(Connection refused)".
func classifyStderr(stderr string) error {
	text := strings.ToLower(strings.TrimSpace(stderr))
	switch {
	case strings.Contains(text, "no server running"):
		return errTmuxNoServer
	case strings.Contains(text, "error connecting to") &&
		(strings.Contains(text, "no such file or directory") ||
			strings.Contains(text, "connection refused")):
		return errTmuxNoServer
	case strings.Contains(text, "can't find session"),
		strings.Contains(text, "session not found"),
		strings.Contains(text, "no such session"):
		return errTmuxNoSession
	}
	return nil
}

func (c *CLI) command(ctx context.Context, args ...string) *exec.Cmd {
	full := append(c.baseArgs(), args...)
	return exec.CommandContext(ctx, c.binary(), full...)
}

func (c *CLI) baseArgs() []string {
	socket := c.Socket
	if socket == "" {
		socket = DefaultSocket
	}
	args := []string{"-L", socket}
	if c.ConfigFile != "" {
		args = append(args, "-f", c.ConfigFile)
	}
	return args
}

func (c *CLI) binary() string {
	if c.Binary != "" {
		return c.Binary
	}
	return "tmux"
}

// Sentinel errors used to translate tmux's stderr into typed signals
// the higher layers can branch on.
var (
	errTmuxNoServer  = errors.New("tmux: no server running")
	errTmuxNoSession = errors.New("tmux: session not found")
)

// IsNotFound reports whether err means "the requested session does not
// exist on this socket".
func IsNotFound(err error) bool {
	return errors.Is(err, errTmuxNoSession) || errors.Is(err, errTmuxNoServer)
}
