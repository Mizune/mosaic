-- 0001_init.sql
-- Initial schema for mosaic Phase 0.
-- Only the sessions table is required for Phase 0 + Phase 1; later
-- migrations add subagents, worktrees, conductors, etc.

CREATE TABLE IF NOT EXISTS sessions (
    id                 TEXT PRIMARY KEY,                       -- ULID
    name               TEXT NOT NULL UNIQUE,
    project_path       TEXT NOT NULL,
    worktree_path      TEXT NOT NULL,
    branch             TEXT,
    tmux_session       TEXT NOT NULL UNIQUE,                   -- mos_<id>
    tool               TEXT NOT NULL DEFAULT 'claude',
    status             TEXT NOT NULL DEFAULT 'idle',
    claude_session_id  TEXT,
    conductor_id       TEXT,
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);
CREATE INDEX IF NOT EXISTS idx_sessions_claude ON sessions(claude_session_id);
