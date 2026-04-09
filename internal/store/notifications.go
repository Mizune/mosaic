package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// NotificationSource categorises where a notification was observed.
type NotificationSource string

const (
	NotificationSourceHook NotificationSource = "hook"
	NotificationSourceJSONL NotificationSource = "jsonl"
	NotificationSourceOSC  NotificationSource = "osc"
)

// Notification is the row type for the notifications table.
type Notification struct {
	ID          int64          `db:"id"`
	SessionID   string         `db:"session_id"`
	Source      string         `db:"source"`
	Agent       sql.NullString `db:"agent"`
	Event       sql.NullString `db:"event"`
	Title       sql.NullString `db:"title"`
	Body        sql.NullString `db:"body"`
	TS          time.Time      `db:"ts"`
	DismissedAt sql.NullTime   `db:"dismissed_at"`
}

// NewNotification is the input shape for [NotificationsRepo.Insert].
type NewNotification struct {
	SessionID string
	Source    NotificationSource
	Agent     string
	Event     string
	Title     string
	Body      string
}

// NotificationsRepo is the typed accessor for the notifications table.
type NotificationsRepo struct {
	db *sqlx.DB
}

func newNotificationsRepo(db *sqlx.DB) *NotificationsRepo {
	return &NotificationsRepo{db: db}
}

// Insert records a new notification.
func (r *NotificationsRepo) Insert(ctx context.Context, in NewNotification) (Notification, error) {
	if in.SessionID == "" || in.Source == "" {
		return Notification{}, errors.New("notification: session_id and source are required")
	}
	res, err := r.db.ExecContext(ctx, `
        INSERT INTO notifications (session_id, source, agent, event, title, body)
        VALUES (?, ?, ?, ?, ?, ?)`,
		in.SessionID, string(in.Source),
		nullString(in.Agent), nullString(in.Event),
		nullString(in.Title), nullString(in.Body),
	)
	if err != nil {
		return Notification{}, err
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, id)
}

// GetByID fetches a single notification.
func (r *NotificationsRepo) GetByID(ctx context.Context, id int64) (Notification, error) {
	var n Notification
	err := r.db.GetContext(ctx, &n, `SELECT * FROM notifications WHERE id = ?`, id)
	return n, err
}

// ListUnseen returns notifications without a dismissed_at, ordered
// most-recent first.
func (r *NotificationsRepo) ListUnseen(ctx context.Context, sessionID string) ([]Notification, error) {
	var out []Notification
	err := r.db.SelectContext(ctx, &out, `
        SELECT * FROM notifications
        WHERE session_id = ? AND dismissed_at IS NULL
        ORDER BY ts DESC`, sessionID)
	return out, err
}

// Dismiss marks every unseen notification for the session as
// dismissed at the current timestamp.
func (r *NotificationsRepo) Dismiss(ctx context.Context, sessionID string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE notifications SET dismissed_at = ?
        WHERE session_id = ? AND dismissed_at IS NULL`,
		time.Now().UTC(), sessionID)
	return err
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
