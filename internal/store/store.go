// Package store wraps the SQLite database that holds mosaic state. It
// owns the connection lifecycle, schema migrations, and the
// repositories that mosaic subsystems use to read and write rows.
//
// The package uses [modernc.org/sqlite] (a pure-Go SQLite driver) so
// that mosaic can be cross-compiled without CGO.
package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/jmoiron/sqlx"

	// Register the pure-Go SQLite driver under the "sqlite" name.
	_ "modernc.org/sqlite"
)

// Store is the top-level handle owning the SQLite database. Repositories
// (Sessions, Subagents, Worktrees, ...) hang off it via methods so the
// rest of the codebase can call store.Sessions().Insert(...) etc.
type Store struct {
	db *sqlx.DB

	sessions      *SessionsRepo
	subagents     *SubagentsRepo
	notifications *NotificationsRepo
	conductors    *ConductorsRepo
	sessionMCPs   *SessionMCPsRepo
	sessionSkills *SessionSkillsRepo
	artifacts     *ArtifactsRepo
}

// Open opens the SQLite database at path with the connection settings
// mosaic requires (WAL journal, foreign keys on, busy timeout). It does
// NOT run migrations: callers must invoke [Store.Migrate] explicitly so
// that mosaic doctor can decide when migrations happen.
func Open(path string) (*Store, error) {
	dsn := buildDSN(path)
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Limit to a single writer connection. SQLite serialises writes
	// anyway and a single conn keeps WAL behaviour predictable.
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)

	db := sqlx.NewDb(raw, "sqlite")
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &Store{db: db}
	s.sessions = newSessionsRepo(db)
	s.subagents = newSubagentsRepo(db)
	s.notifications = newNotificationsRepo(db)
	s.conductors = newConductorsRepo(db)
	s.sessionMCPs = newSessionMCPsRepo(db)
	s.sessionSkills = newSessionSkillsRepo(db)
	s.artifacts = newArtifactsRepo(db)
	return s, nil
}

// Close releases the database handle. Repositories must not be used
// after Close returns.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB returns the underlying sqlx handle. It exists for tests and for
// subsystems that need to compose their own queries; production code
// should prefer the typed repositories.
func (s *Store) DB() *sqlx.DB {
	return s.db
}

// Sessions returns the sessions repository.
func (s *Store) Sessions() *SessionsRepo {
	return s.sessions
}

// Subagents returns the sub-agents repository.
func (s *Store) Subagents() *SubagentsRepo {
	return s.subagents
}

// Notifications returns the notifications repository.
func (s *Store) Notifications() *NotificationsRepo {
	return s.notifications
}

// Conductors returns the conductors / tasks / messages repository.
func (s *Store) Conductors() *ConductorsRepo {
	return s.conductors
}

// SessionMCPs returns the session ↔ MCP attachment repository.
func (s *Store) SessionMCPs() *SessionMCPsRepo {
	return s.sessionMCPs
}

// SessionSkills returns the session ↔ skill attachment repository.
func (s *Store) SessionSkills() *SessionSkillsRepo {
	return s.sessionSkills
}

// Artifacts returns the per-session rich-content (artifact) repository.
func (s *Store) Artifacts() *ArtifactsRepo {
	return s.artifacts
}

// WALCheckpoint forces a WAL checkpoint so the -wal file is truncated
// and its space reclaimed. This is useful for the `mosaic clean`
// command.
func (s *Store) WALCheckpoint() error {
	_, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// buildDSN constructs the modernc.org/sqlite connection string for the
// given on-disk path. ":memory:" is recognised and forwarded as-is so
// tests can use an in-memory database.
func buildDSN(path string) string {
	if path == ":memory:" {
		return ":memory:?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	abs := path
	if !filepath.IsAbs(path) {
		if a, err := filepath.Abs(path); err == nil {
			abs = a
		}
	}
	v := url.Values{}
	v.Add("_pragma", "journal_mode(WAL)")
	v.Add("_pragma", "foreign_keys(1)")
	v.Add("_pragma", "busy_timeout(5000)")
	v.Add("_pragma", "synchronous(NORMAL)")
	return "file:" + abs + "?" + v.Encode()
}
