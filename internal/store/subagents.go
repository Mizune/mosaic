package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/oklog/ulid/v2"
)

// SubagentStatus values stored in the subagents.status column.
type SubagentStatus string

const (
	SubagentSpawning  SubagentStatus = "spawning"
	SubagentRunning   SubagentStatus = "running"
	SubagentCompleted SubagentStatus = "completed"
	SubagentErrored   SubagentStatus = "error"
)

// Subagent is the row type for the subagents table.
type Subagent struct {
	ID                  string         `db:"id"`
	ParentSessionID     string         `db:"parent_session_id"`
	ClaudeSessionID     string         `db:"claude_session_id"`
	AgentID             string         `db:"agent_id"`
	SubagentType        string         `db:"subagent_type"`
	Description         sql.NullString `db:"description"`
	Status              string         `db:"status"`
	StartedAt           time.Time      `db:"started_at"`
	CompletedAt         sql.NullTime   `db:"completed_at"`
	TranscriptPath      string         `db:"transcript_path"`
	CurrentTool         sql.NullString `db:"current_tool"`
	CurrentToolStartedAt sql.NullTime  `db:"current_tool_started_at"`
	LastEventOffset     int64          `db:"last_event_offset"`
}

// NewSubagent is the input shape for [SubagentsRepo.Upsert].
type NewSubagent struct {
	ParentSessionID string
	ClaudeSessionID string
	AgentID         string
	SubagentType    string
	Description     string
	TranscriptPath  string
}

// SubagentsRepo is the typed accessor for the subagents table.
type SubagentsRepo struct {
	db *sqlx.DB
}

// ErrSubagentNotFound is returned when a lookup misses.
var ErrSubagentNotFound = errors.New("subagent not found")

func newSubagentsRepo(db *sqlx.DB) *SubagentsRepo { return &SubagentsRepo{db: db} }

// Upsert inserts a sub-agent if it does not exist, otherwise returns
// the existing row. Sub-agents are uniquely identified by the
// (parent_session_id, agent_id) pair.
func (r *SubagentsRepo) Upsert(ctx context.Context, in NewSubagent) (Subagent, error) {
	if in.ParentSessionID == "" || in.AgentID == "" {
		return Subagent{}, errors.New("subagent: parent_session_id and agent_id are required")
	}

	existing, err := r.GetByAgentID(ctx, in.ParentSessionID, in.AgentID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrSubagentNotFound) {
		return Subagent{}, err
	}

	id, err := newULID()
	if err != nil {
		return Subagent{}, err
	}
	row := Subagent{
		ID:              id,
		ParentSessionID: in.ParentSessionID,
		ClaudeSessionID: in.ClaudeSessionID,
		AgentID:         in.AgentID,
		SubagentType:    in.SubagentType,
		Description:     sql.NullString{String: in.Description, Valid: in.Description != ""},
		Status:          string(SubagentSpawning),
		StartedAt:       time.Now().UTC(),
		TranscriptPath:  in.TranscriptPath,
	}
	_, err = r.db.NamedExecContext(ctx, `
        INSERT INTO subagents (
            id, parent_session_id, claude_session_id, agent_id, subagent_type,
            description, status, started_at, transcript_path
        ) VALUES (
            :id, :parent_session_id, :claude_session_id, :agent_id, :subagent_type,
            :description, :status, :started_at, :transcript_path
        )`, row)
	if err != nil {
		return Subagent{}, fmt.Errorf("insert subagent: %w", err)
	}
	return row, nil
}

// GetByAgentID looks up a sub-agent by (parent session, agent id).
func (r *SubagentsRepo) GetByAgentID(ctx context.Context, parentSessionID, agentID string) (Subagent, error) {
	var s Subagent
	err := r.db.GetContext(ctx, &s,
		`SELECT * FROM subagents WHERE parent_session_id = ? AND agent_id = ?`,
		parentSessionID, agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return Subagent{}, ErrSubagentNotFound
	}
	return s, err
}

// ListByParent returns every sub-agent for a parent session, ordered
// by started_at.
func (r *SubagentsRepo) ListByParent(ctx context.Context, parentSessionID string) ([]Subagent, error) {
	var out []Subagent
	err := r.db.SelectContext(ctx, &out,
		`SELECT * FROM subagents WHERE parent_session_id = ? ORDER BY started_at ASC`,
		parentSessionID)
	return out, err
}

// ListRunning returns every sub-agent whose status is still
// "running", across all parent sessions. Used by the reconciler's
// periodic sweep to find background agents that have gone silent.
func (r *SubagentsRepo) ListRunning(ctx context.Context) ([]Subagent, error) {
	var out []Subagent
	err := r.db.SelectContext(ctx, &out,
		`SELECT * FROM subagents WHERE status = ? ORDER BY started_at ASC`,
		string(SubagentRunning))
	return out, err
}

// SetMetadata updates the human-facing description and the
// subagent_type for a row. It is called once the reconciler has
// parsed the parent transcript's `Agent` tool_use/tool_result pair
// and knows the real values — before that point we only know the
// agent_id, so rows are inserted with a placeholder type and get
// filled in when the metadata lands.
//
// Empty values are ignored so repeated calls never blank out
// fields that another code path already populated.
func (r *SubagentsRepo) SetMetadata(ctx context.Context, id, description, subagentType string) error {
	if description == "" && subagentType == "" {
		return nil
	}
	var (
		query string
		args  []any
	)
	switch {
	case description != "" && subagentType != "":
		query = `UPDATE subagents SET description = ?, subagent_type = ? WHERE id = ?`
		args = []any{description, subagentType, id}
	case description != "":
		query = `UPDATE subagents SET description = ? WHERE id = ?`
		args = []any{description, id}
	default:
		query = `UPDATE subagents SET subagent_type = ? WHERE id = ?`
		args = []any{subagentType, id}
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// UpdateRunningTool patches the "what is this sub-agent doing right
// now?" columns. Pass an empty toolName to clear them.
//
// The WHERE clause guards against late transcript entries flipping
// a sub-agent that has already reached a terminal state back to
// "running": once MarkCompleted / MarkErrored has run, stray
// tool_use/tool_result blocks in the child transcript must not
// resurrect the row.
func (r *SubagentsRepo) UpdateRunningTool(ctx context.Context, id, toolName string, startedAt time.Time) error {
	if toolName == "" {
		_, err := r.db.ExecContext(ctx,
			`UPDATE subagents
             SET current_tool = NULL, current_tool_started_at = NULL, status = ?
             WHERE id = ? AND status IN (?, ?)`,
			string(SubagentRunning), id,
			string(SubagentSpawning), string(SubagentRunning))
		return err
	}
	_, err := r.db.ExecContext(ctx, `
        UPDATE subagents
        SET current_tool = ?, current_tool_started_at = ?, status = ?
        WHERE id = ? AND status IN (?, ?)`,
		toolName, startedAt.UTC(), string(SubagentRunning), id,
		string(SubagentSpawning), string(SubagentRunning))
	return err
}

// MarkCompleted records that a sub-agent finished.
func (r *SubagentsRepo) MarkCompleted(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE subagents
        SET status = ?, completed_at = ?, current_tool = NULL, current_tool_started_at = NULL
        WHERE id = ?`,
		string(SubagentCompleted), time.Now().UTC(), id)
	return err
}

// MarkErrored records that a sub-agent finished with an error.
func (r *SubagentsRepo) MarkErrored(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE subagents
        SET status = ?, completed_at = ?, current_tool = NULL, current_tool_started_at = NULL
        WHERE id = ?`,
		string(SubagentErrored), time.Now().UTC(), id)
	return err
}

// SetEventOffset persists the byte offset of the last event the
// watcher consumed for this sub-agent. Used to resume after restarts.
func (r *SubagentsRepo) SetEventOffset(ctx context.Context, id string, offset int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE subagents SET last_event_offset = ? WHERE id = ?`,
		offset, id)
	return err
}

// newULID is duplicated from sessions.go on purpose: keeping the
// helper unexported in each repo file makes the dependency on
// crypto/rand obvious.
func subagentNewULID() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

var _ = subagentNewULID // prevent unused warning when refactoring
