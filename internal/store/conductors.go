package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// ConductorStatus values stored in conductors.status.
type ConductorStatus string

const (
	ConductorDraft   ConductorStatus = "draft"
	ConductorRunning ConductorStatus = "running"
	ConductorPaused  ConductorStatus = "paused"
	ConductorDone    ConductorStatus = "done"
	ConductorFailed  ConductorStatus = "failed"
)

// Conductor is the row type for the conductors table.
type Conductor struct {
	ID              string         `db:"id"`
	Name            string         `db:"name"`
	ProjectPath     string         `db:"project_path"`
	PlanPath        sql.NullString `db:"plan_path"`
	Status          string         `db:"status"`
	MasterSessionID sql.NullString `db:"master_session_id"`
	CreatedAt       time.Time      `db:"created_at"`
}

// NewConductor is the input shape for [ConductorsRepo.Insert].
type NewConductor struct {
	Name        string
	ProjectPath string
	PlanPath    string
}

// ConductorsRepo is the typed accessor for the conductors table and
// its task / message children.
type ConductorsRepo struct {
	db *sqlx.DB
}

func newConductorsRepo(db *sqlx.DB) *ConductorsRepo {
	return &ConductorsRepo{db: db}
}

// ErrConductorNotFound is returned when a lookup misses.
var ErrConductorNotFound = errors.New("conductor not found")

// Insert creates a conductor row and returns it.
func (r *ConductorsRepo) Insert(ctx context.Context, in NewConductor) (Conductor, error) {
	if in.Name == "" || in.ProjectPath == "" {
		return Conductor{}, errors.New("conductor: name and project_path are required")
	}
	id, err := newULID()
	if err != nil {
		return Conductor{}, err
	}
	row := Conductor{
		ID:          id,
		Name:        in.Name,
		ProjectPath: in.ProjectPath,
		PlanPath:    sql.NullString{String: in.PlanPath, Valid: in.PlanPath != ""},
		Status:      string(ConductorDraft),
		CreatedAt:   time.Now().UTC(),
	}
	_, err = r.db.NamedExecContext(ctx, `
        INSERT INTO conductors (id, name, project_path, plan_path, status, created_at)
        VALUES (:id, :name, :project_path, :plan_path, :status, :created_at)`,
		row)
	if err != nil {
		return Conductor{}, fmt.Errorf("insert conductor: %w", err)
	}
	return row, nil
}

// SetStatus updates the conductor lifecycle column.
func (r *ConductorsRepo) SetStatus(ctx context.Context, id string, status ConductorStatus) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE conductors SET status = ? WHERE id = ?`,
		string(status), id)
	return err
}

// GetByID fetches a conductor.
func (r *ConductorsRepo) GetByID(ctx context.Context, id string) (Conductor, error) {
	var c Conductor
	err := r.db.GetContext(ctx, &c, `SELECT * FROM conductors WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Conductor{}, ErrConductorNotFound
	}
	return c, err
}

// List returns every conductor in created_at order.
func (r *ConductorsRepo) List(ctx context.Context) ([]Conductor, error) {
	var out []Conductor
	err := r.db.SelectContext(ctx, &out,
		`SELECT * FROM conductors ORDER BY created_at DESC`)
	return out, err
}

// --- tasks ----------------------------------------------------------------

// TaskStatus values stored in tasks.status.
type TaskStatus string

const (
	TaskPending TaskStatus = "pending"
	TaskReady   TaskStatus = "ready"
	TaskRunning TaskStatus = "running"
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
)

// ConductorTask is the row type for the tasks table.
type ConductorTask struct {
	ID                string         `db:"id"`
	ConductorID       string         `db:"conductor_id"`
	Name              string         `db:"name"`
	Status            string         `db:"status"`
	DependsOn         string         `db:"depends_on"` // JSON-encoded []string
	Prompt            string         `db:"prompt"`
	AssignedSessionID sql.NullString `db:"assigned_session_id"`
	StartedAt         sql.NullTime   `db:"started_at"`
	CompletedAt       sql.NullTime   `db:"completed_at"`
	Result            sql.NullString `db:"result"`
}

// Dependencies decodes the JSON-encoded depends_on column.
func (t ConductorTask) Dependencies() []string {
	if t.DependsOn == "" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(t.DependsOn), &out)
	return out
}

// NewConductorTask is the input shape for [ConductorsRepo.InsertTask].
type NewConductorTask struct {
	ConductorID string
	Name        string
	DependsOn   []string
	Prompt      string
}

// InsertTask inserts a task row.
func (r *ConductorsRepo) InsertTask(ctx context.Context, in NewConductorTask) (ConductorTask, error) {
	if in.ConductorID == "" || in.Name == "" {
		return ConductorTask{}, errors.New("task: conductor_id and name are required")
	}
	id, err := newULID()
	if err != nil {
		return ConductorTask{}, err
	}
	depJSON, _ := json.Marshal(in.DependsOn)
	row := ConductorTask{
		ID:          id,
		ConductorID: in.ConductorID,
		Name:        in.Name,
		Status:      string(TaskPending),
		DependsOn:   string(depJSON),
		Prompt:      in.Prompt,
	}
	_, err = r.db.NamedExecContext(ctx, `
        INSERT INTO tasks (id, conductor_id, name, status, depends_on, prompt)
        VALUES (:id, :conductor_id, :name, :status, :depends_on, :prompt)`,
		row)
	if err != nil {
		return ConductorTask{}, err
	}
	return row, nil
}

// SetTaskStatus updates the lifecycle column on one task.
func (r *ConductorsRepo) SetTaskStatus(ctx context.Context, id string, status TaskStatus) error {
	now := time.Now().UTC()
	switch status {
	case TaskRunning:
		_, err := r.db.ExecContext(ctx,
			`UPDATE tasks SET status = ?, started_at = ? WHERE id = ?`,
			string(status), now, id)
		return err
	case TaskDone, TaskFailed:
		_, err := r.db.ExecContext(ctx,
			`UPDATE tasks SET status = ?, completed_at = ? WHERE id = ?`,
			string(status), now, id)
		return err
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET status = ? WHERE id = ?`,
		string(status), id)
	return err
}

// AssignTask binds a mosaic session id to a task.
func (r *ConductorsRepo) AssignTask(ctx context.Context, id, sessionID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET assigned_session_id = ? WHERE id = ?`,
		sql.NullString{String: sessionID, Valid: sessionID != ""}, id)
	return err
}

// ListTasks returns every task for a conductor in insertion order.
func (r *ConductorsRepo) ListTasks(ctx context.Context, conductorID string) ([]ConductorTask, error) {
	var out []ConductorTask
	err := r.db.SelectContext(ctx, &out,
		`SELECT * FROM tasks WHERE conductor_id = ? ORDER BY id ASC`,
		conductorID)
	return out, err
}

// --- messages -------------------------------------------------------------

// Message is the row type for the messages table.
type Message struct {
	ID            int64          `db:"id"`
	ConductorID   sql.NullString `db:"conductor_id"`
	FromSessionID sql.NullString `db:"from_session_id"`
	ToSessionID   sql.NullString `db:"to_session_id"`
	Kind          string         `db:"kind"`
	Body          string         `db:"body"`
	TS            time.Time      `db:"ts"`
	DeliveredAt   sql.NullTime   `db:"delivered_at"`
}

// NewMessage is the input shape for [ConductorsRepo.PostMessage].
type NewMessage struct {
	ConductorID   string
	FromSessionID string
	ToSessionID   string
	Kind          string
	Body          string
}

// PostMessage inserts a message row.
func (r *ConductorsRepo) PostMessage(ctx context.Context, in NewMessage) (Message, error) {
	if in.Body == "" {
		return Message{}, errors.New("message: body required")
	}
	res, err := r.db.ExecContext(ctx, `
        INSERT INTO messages (conductor_id, from_session_id, to_session_id, kind, body)
        VALUES (?, ?, ?, ?, ?)`,
		nullString(in.ConductorID), nullString(in.FromSessionID),
		nullString(in.ToSessionID), in.Kind, in.Body)
	if err != nil {
		return Message{}, err
	}
	id, _ := res.LastInsertId()
	return r.getMessage(ctx, id)
}

func (r *ConductorsRepo) getMessage(ctx context.Context, id int64) (Message, error) {
	var m Message
	err := r.db.GetContext(ctx, &m, `SELECT * FROM messages WHERE id = ?`, id)
	return m, err
}

// Inbox returns undelivered messages for the given session.
func (r *ConductorsRepo) Inbox(ctx context.Context, sessionID string) ([]Message, error) {
	var out []Message
	err := r.db.SelectContext(ctx, &out, `
        SELECT * FROM messages
        WHERE to_session_id = ? AND delivered_at IS NULL
        ORDER BY ts ASC`, sessionID)
	return out, err
}

// MarkDelivered marks every undelivered message for a session as
// delivered now.
func (r *ConductorsRepo) MarkDelivered(ctx context.Context, sessionID string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE messages SET delivered_at = ?
        WHERE to_session_id = ? AND delivered_at IS NULL`,
		time.Now().UTC(), sessionID)
	return err
}
