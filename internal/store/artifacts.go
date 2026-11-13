package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Artifact is the row type for the artifacts table: a piece of rich
// content (Mermaid diagram, image, HTML page or URL) that belongs to a
// session and that the viewer can open.
type Artifact struct {
	ID          string         `db:"id"`
	SessionID   string         `db:"session_id"`
	Kind        string         `db:"kind"`
	Source      string         `db:"source"`
	AgentID     string         `db:"agent_id"`
	Title       string         `db:"title"`
	Content     sql.NullString `db:"content"`
	Path        string         `db:"path"`
	ContentHash string         `db:"content_hash"`
	CreatedAt   time.Time      `db:"created_at"`
}

// NewArtifact is the input shape for [ArtifactsRepo.Upsert]. CreatedAt
// should be the transcript timestamp so a replay after restart keeps
// the original time; zero means now.
type NewArtifact struct {
	SessionID   string
	Kind        string
	Source      string
	AgentID     string
	Title       string
	Content     string
	Path        string
	ContentHash string
	CreatedAt   time.Time
}

// ArtifactsRepo is the typed accessor for the artifacts table.
type ArtifactsRepo struct {
	db *sqlx.DB
}

// ErrArtifactNotFound is returned when a lookup misses.
var ErrArtifactNotFound = errors.New("artifact not found")

func newArtifactsRepo(db *sqlx.DB) *ArtifactsRepo { return &ArtifactsRepo{db: db} }

// Upsert inserts the artifact unless a row with the same
// (session_id, content_hash) already exists, in which case the existing
// row is returned unchanged and created is false. The lookup-then-insert
// shape mirrors [SubagentsRepo.Upsert]; the UNIQUE constraint is the
// backstop for concurrent writers.
func (r *ArtifactsRepo) Upsert(ctx context.Context, in NewArtifact) (Artifact, bool, error) {
	if in.SessionID == "" || in.Kind == "" || in.ContentHash == "" {
		return Artifact{}, false, errors.New("artifact: session_id, kind and content_hash are required")
	}
	existing, err := r.GetByHash(ctx, in.SessionID, in.ContentHash)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrArtifactNotFound) {
		return Artifact{}, false, err
	}

	id, err := newULID()
	if err != nil {
		return Artifact{}, false, err
	}
	created := in.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	row := Artifact{
		ID:          id,
		SessionID:   in.SessionID,
		Kind:        in.Kind,
		Source:      in.Source,
		AgentID:     in.AgentID,
		Title:       in.Title,
		Content:     sql.NullString{String: in.Content, Valid: in.Content != ""},
		Path:        in.Path,
		ContentHash: in.ContentHash,
		CreatedAt:   created.UTC(),
	}
	_, err = r.db.NamedExecContext(ctx, `
        INSERT INTO artifacts (
            id, session_id, kind, source, agent_id, title, content, path, content_hash, created_at
        ) VALUES (
            :id, :session_id, :kind, :source, :agent_id, :title, :content, :path, :content_hash, :created_at
        )`, row)
	if err != nil {
		return Artifact{}, false, fmt.Errorf("insert artifact: %w", err)
	}
	return row, true, nil
}

// GetByHash looks up an artifact by (session, content hash).
func (r *ArtifactsRepo) GetByHash(ctx context.Context, sessionID, hash string) (Artifact, error) {
	var row Artifact
	err := r.db.GetContext(ctx, &row,
		`SELECT * FROM artifacts WHERE session_id = ? AND content_hash = ?`, sessionID, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, ErrArtifactNotFound
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("get artifact: %w", err)
	}
	return row, nil
}

// ListBySession returns a session's artifacts, newest first.
func (r *ArtifactsRepo) ListBySession(ctx context.Context, sessionID string) ([]Artifact, error) {
	var rows []Artifact
	err := r.db.SelectContext(ctx, &rows,
		`SELECT * FROM artifacts WHERE session_id = ? ORDER BY created_at DESC, id DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	return rows, nil
}

// ListAll returns every artifact grouped by session id, newest first
// within each session. The TUI calls it once per refresh instead of
// issuing one query per session.
func (r *ArtifactsRepo) ListAll(ctx context.Context) (map[string][]Artifact, error) {
	var rows []Artifact
	err := r.db.SelectContext(ctx, &rows,
		`SELECT * FROM artifacts ORDER BY session_id, created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list all artifacts: %w", err)
	}
	out := make(map[string][]Artifact, 8)
	for _, row := range rows {
		out[row.SessionID] = append(out[row.SessionID], row)
	}
	return out, nil
}

// Delete removes one artifact row. Deleting a missing id is not an error.
func (r *ArtifactsRepo) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return nil
}
