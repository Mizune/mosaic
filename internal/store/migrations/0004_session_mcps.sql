-- 0004_session_mcps.sql
-- Phase 5: per-session MCP attachment state.
--
-- mosaic discovers the globally configured MCP servers from
-- ~/.claude.json (key "mcpServers") on every launch. This table
-- records which ones are currently attached to which mosaic session
-- so the MCP Manager TUI can round-trip attach/detach decisions
-- without losing them across restarts.
--
-- The attachment is INTENT — the row says "the user wants this MCP
-- active on this session". Applying that intent to the running
-- Claude Code instance is a separate concern handled by the service
-- layer when it (re)spawns the agent.

CREATE TABLE IF NOT EXISTS session_mcps (
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    mcp_name     TEXT NOT NULL,
    attached_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (session_id, mcp_name)
);

CREATE INDEX IF NOT EXISTS idx_session_mcps_session ON session_mcps(session_id);
