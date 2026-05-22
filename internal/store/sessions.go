package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/oklog/ulid/v2"
)

// SessionStatus is the value stored in sessions.status.
type SessionStatus string

const (
	StatusIdle    SessionStatus = "idle"
	StatusRunning SessionStatus = "running"
	StatusWaiting SessionStatus = "waiting"
	StatusError   SessionStatus = "error"
	StatusStopped SessionStatus = "stopped"
)

// SessionTool is the agent runtime a session is launched with.
type SessionTool string

const (
	ToolClaude SessionTool = "claude"
	ToolCodex  SessionTool = "codex"
	ToolGemini SessionTool = "gemini"
	ToolShell  SessionTool = "shell"
)

// defaultSandboxMode is the value stored in sessions.sandbox_mode
// when the caller does not supply one. It mirrors the canonical
// "none" token from internal/sandbox but is kept as a plain string
// so the store layer does not import higher-level packages.
const defaultSandboxMode = "none"

// AttentionState classifies whether the user needs to act on a
// session: none, waiting for input, or in an error state.
type AttentionState string

const (
	AttentionNone         AttentionState = "none"
	AttentionWaitingInput AttentionState = "waiting_input"
	AttentionError        AttentionState = "error"
)

// Session is the row representation of a mosaic session.
type Session struct {
	ID                 string         `db:"id"`
	Name               string         `db:"name"`
	ProjectPath        string         `db:"project_path"`
	WorktreePath       string         `db:"worktree_path"`
	Branch             sql.NullString `db:"branch"`
	TmuxSession        string         `db:"tmux_session"`
	Tool               string         `db:"tool"`
	Status             string         `db:"status"`
	ClaudeSessionID    sql.NullString `db:"claude_session_id"`
	AgentSessionID     sql.NullString `db:"agent_session_id"`
	AgentSessionSource string         `db:"agent_session_source"`
	AgentSessionPath   string         `db:"agent_session_path"`
	AgentCLIVersion    string         `db:"agent_cli_version"`
	ConductorID        sql.NullString `db:"conductor_id"`
	CreatedAt          time.Time      `db:"created_at"`
	LastActiveAt       time.Time      `db:"last_active_at"`
	AttentionState     string         `db:"attention_state"`
	AttentionTitle     sql.NullString `db:"attention_title"`
	AttentionBody      sql.NullString `db:"attention_body"`
	AttentionSince     sql.NullTime   `db:"attention_since"`
	AttentionSource    sql.NullString `db:"attention_source"`

	// SandboxMode records the isolation backend the session was
	// launched with: "none" (direct host execution) or "docker".
	// Persisted so Stop/Delete can issue `docker rm -f` without
	// having to re-derive the mode from config on the reattach
	// path (where that config may have changed).
	SandboxMode string `db:"sandbox_mode"`

	// SandboxContainer is the deterministic container name
	// (mosaic-<id>) for docker-backed sessions. Empty when
	// SandboxMode != "docker". Stored verbatim so cleanup code
	// does not depend on how the name was computed.
	SandboxContainer string `db:"sandbox_container"`
}

// NewSession is the input shape for [SessionsRepo.Insert]. Required
// fields are validated; optional fields default sensibly.
type NewSession struct {
	// ID, when non-empty, is used verbatim as the session's
	// ULID instead of minting one in Insert. The only caller
	// that pre-generates is SessionService.Create, which needs
	// the ID to derive the sandbox container name before the
	// row exists. All other callers leave this empty.
	ID           string
	Name         string
	ProjectPath  string
	WorktreePath string
	Branch       string
	Tool         SessionTool

	// SandboxMode records which isolation backend the session
	// uses. Defaults to "none" when empty.
	SandboxMode string

	// SandboxContainer is the container name for docker sessions.
	// Empty when SandboxMode != "docker".
	SandboxContainer string
}

// sessionColumns is the explicit column list for SELECT queries on the
// sessions table. Using this instead of SELECT * prevents sqlx from
// emitting "missing destination name" warnings when the database still
// has columns (e.g. legacy_id) that are absent from the Session struct.
var sessionColumns = strings.Join([]string{
	"id", "name", "project_path", "worktree_path", "branch",
	"tmux_session", "tool", "status", "claude_session_id",
	"agent_session_id", "agent_session_source", "agent_session_path", "agent_cli_version",
	"conductor_id",
	"created_at", "last_active_at", "attention_state", "attention_title",
	"attention_body", "attention_since", "attention_source",
	"sandbox_mode", "sandbox_container",
}, ", ")

// SessionsRepo is the typed accessor for the sessions table.
type SessionsRepo struct {
	db *sqlx.DB
}

func newSessionsRepo(db *sqlx.DB) *SessionsRepo {
	return &SessionsRepo{db: db}
}

// ErrSessionNotFound is returned by [SessionsRepo.GetByID] and friends
// when no row matches.
var ErrSessionNotFound = errors.New("session not found")

// Insert creates a new session row. The ID and tmux session name are
// generated automatically; pass [NewSession] with the user-facing
// fields populated.
func (r *SessionsRepo) Insert(ctx context.Context, in NewSession) (Session, error) {
	if in.Name == "" {
		return Session{}, errors.New("session name is required")
	}
	if in.WorktreePath == "" {
		return Session{}, errors.New("session worktree_path is required")
	}
	if in.ProjectPath == "" {
		in.ProjectPath = in.WorktreePath
	}
	if in.Tool == "" {
		in.Tool = ToolClaude
	}

	id := in.ID
	if id == "" {
		generated, err := newULID()
		if err != nil {
			return Session{}, fmt.Errorf("generate id: %w", err)
		}
		id = generated
	}

	sandboxMode := in.SandboxMode
	if sandboxMode == "" {
		sandboxMode = defaultSandboxMode
	}

	sess := Session{
		ID:               id,
		Name:             in.Name,
		ProjectPath:      in.ProjectPath,
		WorktreePath:     in.WorktreePath,
		Branch:           sql.NullString{String: in.Branch, Valid: in.Branch != ""},
		TmuxSession:      "mos_" + id,
		Tool:             string(in.Tool),
		Status:           string(StatusIdle),
		CreatedAt:        time.Now().UTC(),
		LastActiveAt:     time.Now().UTC(),
		SandboxMode:      sandboxMode,
		SandboxContainer: in.SandboxContainer,
	}

	_, err := r.db.NamedExecContext(ctx, `
        INSERT INTO sessions (
            id, name, project_path, worktree_path, branch,
            tmux_session, tool, status,
            created_at, last_active_at,
            sandbox_mode, sandbox_container
        ) VALUES (
            :id, :name, :project_path, :worktree_path, :branch,
            :tmux_session, :tool, :status,
            :created_at, :last_active_at,
            :sandbox_mode, :sandbox_container
        )`, sess)
	if err != nil {
		return Session{}, fmt.Errorf("insert session: %w", err)
	}
	return sess, nil
}

// GetByID returns a session by its ULID, or [ErrSessionNotFound].
func (r *SessionsRepo) GetByID(ctx context.Context, id string) (Session, error) {
	var s Session
	err := r.db.GetContext(ctx, &s, `SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, err
	}
	return s, nil
}

// GetByName returns a session by its user-visible name, or
// [ErrSessionNotFound].
func (r *SessionsRepo) GetByName(ctx context.Context, name string) (Session, error) {
	var s Session
	err := r.db.GetContext(ctx, &s, `SELECT `+sessionColumns+` FROM sessions WHERE name = ?`, name)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, err
	}
	return s, nil
}

// List returns all sessions ordered by most recently active first.
func (r *SessionsRepo) List(ctx context.Context) ([]Session, error) {
	var out []Session
	err := r.db.SelectContext(ctx, &out, `
        SELECT `+sessionColumns+` FROM sessions ORDER BY last_active_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateStatus changes the status of a session and refreshes its
// last_active_at timestamp.
func (r *SessionsRepo) UpdateStatus(ctx context.Context, id string, status SessionStatus) error {
	res, err := r.db.ExecContext(ctx, `
        UPDATE sessions
        SET status = ?, last_active_at = ?
        WHERE id = ?`, string(status), time.Now().UTC(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// SetAttention records that a session is asking for the user's
// attention. Pass [AttentionNone] together with empty title/body to
// clear it.
func (r *SessionsRepo) SetAttention(
	ctx context.Context,
	id string,
	state AttentionState,
	title, body, source string,
) error {
	now := sql.NullTime{Time: time.Now().UTC(), Valid: state != AttentionNone}
	_, err := r.db.ExecContext(ctx, `
        UPDATE sessions
        SET attention_state = ?, attention_title = ?, attention_body = ?,
            attention_since = ?, attention_source = ?, last_active_at = ?
        WHERE id = ?`,
		string(state),
		nullString(title), nullString(body),
		now, nullString(source),
		time.Now().UTC(), id)
	return err
}

// BindAgentSession associates an agent runtime's native session id
// with a mosaic session. Claude writes both the generic fields and the
// legacy claude_session_id mirror so older integrations keep working.
func (r *SessionsRepo) BindAgentSession(ctx context.Context, id string, runtime SessionTool, agentSessionID, path, cliVersion string) error {
	if runtime == "" {
		runtime = ToolClaude
	}
	if runtime == ToolClaude {
		_, err := r.db.ExecContext(ctx, `
            UPDATE sessions
            SET agent_session_id = ?, agent_session_source = ?,
                agent_session_path = ?, agent_cli_version = ?,
                claude_session_id = ?
            WHERE id = ?`,
			nullString(agentSessionID), string(runtime), path, cliVersion,
			nullString(agentSessionID), id)
		return err
	}
	_, err := r.db.ExecContext(ctx, `
        UPDATE sessions
        SET agent_session_id = ?, agent_session_source = ?,
            agent_session_path = ?, agent_cli_version = ?
        WHERE id = ?`,
		nullString(agentSessionID), string(runtime), path, cliVersion, id)
	return err
}

// BindClaudeSession associates a Claude Code session id with a mosaic
// session. Deprecated: use BindAgentSession for new integrations.
func (r *SessionsRepo) BindClaudeSession(ctx context.Context, id, claudeSessionID string) error {
	return r.BindAgentSession(ctx, id, ToolClaude, claudeSessionID, "", "")
}

// Delete removes a session row.
func (r *SessionsRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// newULID generates a fresh ULID string.
func newULID() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// NewSessionID mints a fresh session ULID without touching the
// database. It is the public entry point for callers that need
// the ID before calling [SessionsRepo.Insert] (typically so they
// can derive related identifiers — the sandbox container name,
// for example — and pass them in via [NewSession] instead of
// letting Insert synthesise them).
func NewSessionID() (string, error) {
	return newULID()
}
