-- 0002_subagents.sql
-- Phase 4: sub-agent visibility, attention detection, and the
-- notifications history that the TUI's popover renders.

ALTER TABLE sessions ADD COLUMN attention_state TEXT NOT NULL DEFAULT 'none';
ALTER TABLE sessions ADD COLUMN attention_title TEXT;
ALTER TABLE sessions ADD COLUMN attention_body TEXT;
ALTER TABLE sessions ADD COLUMN attention_since DATETIME;
ALTER TABLE sessions ADD COLUMN attention_source TEXT;

CREATE TABLE IF NOT EXISTS subagents (
    id                      TEXT PRIMARY KEY,
    parent_session_id       TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    claude_session_id       TEXT NOT NULL,
    agent_id                TEXT NOT NULL,
    subagent_type           TEXT NOT NULL,
    description             TEXT,
    status                  TEXT NOT NULL DEFAULT 'spawning',
    started_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at            DATETIME,
    transcript_path         TEXT NOT NULL,
    current_tool            TEXT,
    current_tool_started_at DATETIME,
    last_event_offset       INTEGER NOT NULL DEFAULT 0,
    UNIQUE(parent_session_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_subagents_parent ON subagents(parent_session_id, status);

CREATE TABLE IF NOT EXISTS notifications (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source       TEXT NOT NULL,        -- hook | jsonl | osc
    agent        TEXT,                  -- claude | codex | ...
    event        TEXT,                  -- Notification | Stop | UserPromptSubmit
    title        TEXT,
    body         TEXT,
    ts           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    dismissed_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_notifications_session ON notifications(session_id, dismissed_at, ts);
