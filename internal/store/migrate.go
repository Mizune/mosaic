package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migration holds one parsed migration ready to apply.
type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies any unapplied migrations from the embedded migrations
// directory. It is idempotent: re-running it after every migration has
// already been applied is a no-op.
//
// Each migration is applied inside its own transaction so a failure
// leaves the database in a known state.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.ensureSchemaMigrationsTable(ctx); err != nil {
		return err
	}
	applied, err := s.appliedVersions(ctx)
	if err != nil {
		return err
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := s.applyMigration(ctx, m); err != nil {
			return fmt.Errorf("apply migration %04d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}

// AppliedVersions returns the set of migration versions currently
// recorded in schema_migrations.
func (s *Store) AppliedVersions(ctx context.Context) (map[int]bool, error) {
	if err := s.ensureSchemaMigrationsTable(ctx); err != nil {
		return nil, err
	}
	return s.appliedVersions(ctx)
}

func (s *Store) ensureSchemaMigrationsTable(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version    INTEGER PRIMARY KEY,
            applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        )`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

func (s *Store) appliedVersions(ctx context.Context) (map[int]bool, error) {
	rows, err := s.db.QueryxContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()
	out := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func (s *Store) applyMigration(ctx context.Context, m migration) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version) VALUES (?)`, m.version); err != nil {
		return err
	}
	return tx.Commit()
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	var ms []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m, err := parseMigration(e.Name())
		if err != nil {
			return nil, err
		}
		body, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		m.sql = string(body)
		ms = append(ms, m)
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	return ms, nil
}

// parseMigration extracts the version and human name from a file name
// like "0001_init.sql".
func parseMigration(filename string) (migration, error) {
	base := strings.TrimSuffix(filename, ".sql")
	idx := strings.IndexByte(base, '_')
	if idx <= 0 {
		return migration{}, fmt.Errorf("invalid migration name %q", filename)
	}
	v, err := strconv.Atoi(base[:idx])
	if err != nil {
		return migration{}, fmt.Errorf("invalid version in %q: %w", filename, err)
	}
	return migration{version: v, name: base[idx+1:]}, nil
}
