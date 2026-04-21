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

// DefaultSubagentType is the placeholder subagent_type used when
// Claude Code does not specify one explicitly (i.e. the user wrote
// Agent without a subagent_type parameter). Exported so the TUI,
// Discord, and reconciler can filter it out of display labels.
const DefaultSubagentType = "general-purpose"

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
	ID                   string         `db:"id"`
	ParentSessionID      string         `db:"parent_session_id"`
	ClaudeSessionID      string         `db:"claude_session_id"`
	AgentID              string         `db:"agent_id"`
	SubagentType         string         `db:"subagent_type"`
	Name                 sql.NullString `db:"name"`
	Description          sql.NullString `db:"description"`
	Status               string         `db:"status"`
	StartedAt            time.Time      `db:"started_at"`
	CompletedAt          sql.NullTime   `db:"completed_at"`
	TranscriptPath       string         `db:"transcript_path"`
	CurrentTool          sql.NullString `db:"current_tool"`
	CurrentToolStartedAt sql.NullTime   `db:"current_tool_started_at"`
	LastEventOffset      int64          `db:"last_event_offset"`
}

// LabelParts reports whether the row carries a meaningful (non-default)
// type and a non-empty description. Callers use the results to format
// display labels without duplicating the DefaultSubagentType filter.
func (s Subagent) LabelParts() (hasType bool, typ, name, desc string) {
	hasType = s.SubagentType != "" && s.SubagentType != DefaultSubagentType
	typ = s.SubagentType
	if s.Name.Valid {
		name = s.Name.String
	}
	if s.Description.Valid {
		desc = s.Description.String
	}
	return
}

// NewSubagent is the input shape for [SubagentsRepo.Upsert].
type NewSubagent struct {
	ParentSessionID string
	ClaudeSessionID string
	AgentID         string
	SubagentType    string
	Name            string
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
		Name:            sql.NullString{String: in.Name, Valid: in.Name != ""},
		Description:     sql.NullString{String: in.Description, Valid: in.Description != ""},
		Status:          string(SubagentSpawning),
		StartedAt:       time.Now().UTC(),
		TranscriptPath:  in.TranscriptPath,
	}
	_, err = r.db.NamedExecContext(ctx, `
        INSERT INTO subagents (
            id, parent_session_id, claude_session_id, agent_id, subagent_type,
            name, description, status, started_at, transcript_path
        ) VALUES (
            :id, :parent_session_id, :claude_session_id, :agent_id, :subagent_type,
            :name, :description, :status, :started_at, :transcript_path
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

// SubagentMeta groups the human-facing metadata fields that arrive
// from the parent transcript's Agent tool_use input. Using a struct
// prevents call-site argument transposition and makes future field
// additions backward-compatible.
type SubagentMeta struct {
	Name         string
	Description  string
	SubagentType string
}

// SetMetadata updates the human-facing metadata for a row. It is
// called once the reconciler has parsed the parent transcript's
// `Agent` tool_use/tool_result pair and knows the real values —
// before that point we only know the agent_id, so rows are inserted
// with a placeholder type and get filled in when the metadata lands.
//
// Empty values are ignored so repeated calls never blank out
// fields that another code path already populated.
func (r *SubagentsRepo) SetMetadata(ctx context.Context, id string, meta SubagentMeta) error {
	if meta.Name == "" && meta.Description == "" && meta.SubagentType == "" {
		return nil
	}
	var sets []string
	var args []any
	if meta.Name != "" {
		sets = append(sets, "name = ?")
		args = append(args, meta.Name)
	}
	if meta.Description != "" {
		sets = append(sets, "description = ?")
		args = append(args, meta.Description)
	}
	if meta.SubagentType != "" {
		sets = append(sets, "subagent_type = ?")
		args = append(args, meta.SubagentType)
	}
	args = append(args, id)
	query := "UPDATE subagents SET " + strings.Join(sets, ", ") + " WHERE id = ?"
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

// PromoteSpawning transitions a sub-agent from "spawning" to
// "running" without touching current_tool. This is used when a
// transcript entry arrives that contains no tool_use block (e.g.
// plain text), so the status reflects activity even though no tool
// is active yet.
func (r *SubagentsRepo) PromoteSpawning(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE subagents SET status = ? WHERE id = ? AND status = ?`,
		string(SubagentRunning), id, string(SubagentSpawning))
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
