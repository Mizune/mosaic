-- 0010_artifacts.sql
-- Artifact viewer: rich content (Mermaid diagrams, images, HTML pages,
-- URLs) detected in agent transcripts or registered by hand, so the TUI
-- can list it per session and open it in a browser pane.
--
-- content_hash is the SHA-256 prefix of the Mermaid source (mermaid
-- kind) or of the absolute file path / URL (file and url kinds). The
-- UNIQUE constraint makes transcript replay on every mosaic start
-- idempotent: the same diagram is recorded once.

CREATE TABLE IF NOT EXISTS artifacts (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,                 -- mermaid | image | html | url
    source       TEXT NOT NULL,                 -- transcript | manual
    agent_id     TEXT NOT NULL DEFAULT '',      -- '' = parent transcript
    title        TEXT NOT NULL DEFAULT '',
    content      TEXT,                          -- mermaid source
    path         TEXT NOT NULL DEFAULT '',      -- absolute file path or URL
    content_hash TEXT NOT NULL,
    created_at   DATETIME NOT NULL,
    UNIQUE(session_id, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_artifacts_session ON artifacts(session_id, created_at DESC);
