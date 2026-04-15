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
	ID              string         `db:"id"`
	Name            string         `db:"name"`
	ProjectPath     string         `db:"project_path"`
	WorktreePath    string         `db:"worktree_path"`
	Branch          sql.NullString `db:"branch"`
	TmuxSession     string         `db:"tmux_session"`
	Tool            string         `db:"tool"`
	Status          string         `db:"status"`
	ClaudeSessionID sql.NullString `db:"claude_session_id"`
	ConductorID     sql.NullString `db:"conductor_id"`
	CreatedAt       time.Time      `db:"created_at"`
	LastActiveAt    time.Time      `db:"last_active_at"`
	AttentionState  string         `db:"attention_state"`
	AttentionTitle  sql.NullString `db:"attention_title"`
	AttentionBody   sql.NullString `db:"attention_body"`
	AttentionSince  sql.NullTime   `db:"attention_since"`
	AttentionSource sql.NullString `db:"attention_source"`
}

// NewSession is the input shape for [SessionsRepo.Insert]. Required
// fields are validated; optional fields default sensibly.
type NewSession struct {
	Name         string
	ProjectPath  string
	WorktreePath string
	Branch       string
	Tool         SessionTool
}

// sessionColumns is the explicit column list for SELECT queries on the
// sessions table. Using this instead of SELECT * prevents sqlx from
// emitting "missing destination name" warnings when the database still
// has columns (e.g. legacy_id) that are absent from the Session struct.
var sessionColumns = strings.Join([]string{
	"id", "name", "project_path", "worktree_path", "branch",
	"tmux_session", "tool", "status", "claude_session_id", "conductor_id",
	"created_at", "last_active_at", "attention_state", "attention_title",
	"attention_body", "attention_since", "attention_source",
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

	id, err := newULID()
	if err != nil {
		return Session{}, fmt.Errorf("generate id: %w", err)
	}

	sess := Session{
		ID:           id,
		Name:         in.Name,
		ProjectPath:  in.ProjectPath,
		WorktreePath: in.WorktreePath,
		Branch:       sql.NullString{String: in.Branch, Valid: in.Branch != ""},
		TmuxSession:  "mos_" + id,
		Tool:         string(in.Tool),
		Status:       string(StatusIdle),
		CreatedAt:    time.Now().UTC(),
		LastActiveAt: time.Now().UTC(),
	}

	_, err = r.db.NamedExecContext(ctx, `
        INSERT INTO sessions (
            id, name, project_path, worktree_path, branch,
            tmux_session, tool, status,
            created_at, last_active_at
        ) VALUES (
            :id, :name, :project_path, :worktree_path, :branch,
            :tmux_session, :tool, :status,
            :created_at, :last_active_at
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
