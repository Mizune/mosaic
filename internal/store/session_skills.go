package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// SessionSkill is one row of the session_skills table.
type SessionSkill struct {
	SessionID  string    `db:"session_id"`
	SkillName  string    `db:"skill_name"`
	AttachedAt time.Time `db:"attached_at"`
}

// SessionSkillsRepo is the typed accessor for session_skills.
// Intentionally identical in shape to [SessionMCPsRepo]: the two
// tables have the same model (attachment intent keyed by a name) so
// they share the same CRUD verbs.
type SessionSkillsRepo struct {
	db *sqlx.DB
}

func newSessionSkillsRepo(db *sqlx.DB) *SessionSkillsRepo {
	return &SessionSkillsRepo{db: db}
}

// Attach records that sessionID should have skillName active.
// Duplicate attaches are turned into no-ops so the MODIFY path can
// blindly run it without an explicit "is attached" check.
func (r *SessionSkillsRepo) Attach(ctx context.Context, sessionID, skillName string) error {
	if sessionID == "" || skillName == "" {
		return errors.New("session_skills: session_id and skill_name are required")
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO session_skills (session_id, skill_name) VALUES (?, ?)
        ON CONFLICT(session_id, skill_name) DO NOTHING`,
		sessionID, skillName)
	return err
}

// Detach removes an attachment.
func (r *SessionSkillsRepo) Detach(ctx context.Context, sessionID, skillName string) error {
	if sessionID == "" || skillName == "" {
		return errors.New("session_skills: session_id and skill_name are required")
	}
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM session_skills WHERE session_id = ? AND skill_name = ?`,
		sessionID, skillName)
	return err
}

// IsAttached reports whether a (session, skill) pair is attached.
func (r *SessionSkillsRepo) IsAttached(ctx context.Context, sessionID, skillName string) (bool, error) {
	var dummy int
	err := r.db.GetContext(ctx, &dummy,
		`SELECT 1 FROM session_skills WHERE session_id = ? AND skill_name = ?`,
		sessionID, skillName)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// ForSession lists every skill currently attached to sessionID.
func (r *SessionSkillsRepo) ForSession(ctx context.Context, sessionID string) ([]SessionSkill, error) {
	var rows []SessionSkill
	err := r.db.SelectContext(ctx, &rows, `
        SELECT session_id, skill_name, attached_at
          FROM session_skills
         WHERE session_id = ?
         ORDER BY attached_at DESC`,
		sessionID)
	return rows, err
}

// AttachedNames returns a set of skill names attached to sessionID,
// shaped for the TUI modal so it can check membership in O(1) when
// rendering the ☑/☐ column.
func (r *SessionSkillsRepo) AttachedNames(ctx context.Context, sessionID string) (map[string]bool, error) {
	rows, err := r.ForSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.SkillName] = true
	}
	return out, nil
}
