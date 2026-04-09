package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// SessionMCP is one row of the session_mcps table: the intent "this
// mosaic session has MCP <name> attached, since <attached_at>".
type SessionMCP struct {
	SessionID  string    `db:"session_id"`
	MCPName    string    `db:"mcp_name"`
	AttachedAt time.Time `db:"attached_at"`
}

// SessionMCPsRepo is the typed accessor for session_mcps.
type SessionMCPsRepo struct {
	db *sqlx.DB
}

func newSessionMCPsRepo(db *sqlx.DB) *SessionMCPsRepo {
	return &SessionMCPsRepo{db: db}
}

// Attach records that sessionID should have mcpName active. Calling
// it on an already-attached pair is a no-op (the primary key makes
// the INSERT fail with a UNIQUE violation; we turn that into
// success because the desired state already holds).
func (r *SessionMCPsRepo) Attach(ctx context.Context, sessionID, mcpName string) error {
	if sessionID == "" || mcpName == "" {
		return errors.New("session_mcps: session_id and mcp_name are required")
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO session_mcps (session_id, mcp_name) VALUES (?, ?)
        ON CONFLICT(session_id, mcp_name) DO NOTHING`,
		sessionID, mcpName)
	return err
}

// Detach removes an attachment. Detaching something that is not
// attached is silently a no-op so the UI can toggle freely.
func (r *SessionMCPsRepo) Detach(ctx context.Context, sessionID, mcpName string) error {
	if sessionID == "" || mcpName == "" {
		return errors.New("session_mcps: session_id and mcp_name are required")
	}
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM session_mcps WHERE session_id = ? AND mcp_name = ?`,
		sessionID, mcpName)
	return err
}

// IsAttached reports whether a given (session, mcp) pair is
// currently attached.
func (r *SessionMCPsRepo) IsAttached(ctx context.Context, sessionID, mcpName string) (bool, error) {
	var dummy int
	err := r.db.GetContext(ctx, &dummy,
		`SELECT 1 FROM session_mcps WHERE session_id = ? AND mcp_name = ?`,
		sessionID, mcpName)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// ForSession lists every MCP currently attached to sessionID,
// sorted by attach time so the UI can surface the most recent
// additions first if it wants to.
func (r *SessionMCPsRepo) ForSession(ctx context.Context, sessionID string) ([]SessionMCP, error) {
	var rows []SessionMCP
	err := r.db.SelectContext(ctx, &rows, `
        SELECT session_id, mcp_name, attached_at
          FROM session_mcps
         WHERE session_id = ?
         ORDER BY attached_at DESC`,
		sessionID)
	return rows, err
}

// AttachedNames is a convenience that returns only the names of the
// attached MCPs for sessionID — the shape the TUI modal needs so it
// can render ☑/☐ next to each discovered server.
func (r *SessionMCPsRepo) AttachedNames(ctx context.Context, sessionID string) (map[string]bool, error) {
	rows, err := r.ForSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.MCPName] = true
	}
	return out, nil
}
