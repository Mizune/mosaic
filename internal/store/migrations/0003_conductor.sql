-- 0003_conductor.sql
-- Phase 6: conductors, tasks, inter-agent messaging.

CREATE TABLE IF NOT EXISTS conductors (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    project_path      TEXT NOT NULL,
    plan_path         TEXT,
    status            TEXT NOT NULL DEFAULT 'draft',
    master_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_conductors_status ON conductors(status);

CREATE TABLE IF NOT EXISTS tasks (
    id                  TEXT PRIMARY KEY,
    conductor_id        TEXT NOT NULL REFERENCES conductors(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending',
    depends_on          TEXT NOT NULL DEFAULT '[]',
    prompt              TEXT NOT NULL,
    assigned_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    started_at          DATETIME,
    completed_at        DATETIME,
    result              TEXT,
    UNIQUE(conductor_id, name)
);

CREATE INDEX IF NOT EXISTS idx_tasks_conductor ON tasks(conductor_id, status);

CREATE TABLE IF NOT EXISTS messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conductor_id    TEXT REFERENCES conductors(id) ON DELETE CASCADE,
    from_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    to_session_id   TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    kind            TEXT NOT NULL,
    body            TEXT NOT NULL,
    ts              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at    DATETIME
);

CREATE INDEX IF NOT EXISTS idx_messages_to ON messages(to_session_id, delivered_at, ts);
