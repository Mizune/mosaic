-- 0009_agent_sessions.sql
-- Generalise persisted agent-session metadata beyond Claude Code.
--
-- The legacy claude_session_id columns remain for backward
-- compatibility. New code should prefer agent_session_id plus the
-- runtime/source fields and only use claude_session_id as a Claude
-- compatibility mirror.

ALTER TABLE sessions ADD COLUMN agent_session_id     TEXT;
ALTER TABLE sessions ADD COLUMN agent_session_source TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN agent_session_path   TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN agent_cli_version    TEXT NOT NULL DEFAULT '';

UPDATE sessions
SET agent_session_id = claude_session_id,
    agent_session_source = 'claude'
WHERE claude_session_id IS NOT NULL
  AND claude_session_id != ''
  AND (agent_session_id IS NULL OR agent_session_id = '');

CREATE INDEX IF NOT EXISTS idx_sessions_agent_session
ON sessions(tool, agent_session_id);

ALTER TABLE subagents ADD COLUMN agent_session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE subagents ADD COLUMN agent_runtime    TEXT NOT NULL DEFAULT 'claude';

UPDATE subagents
SET agent_session_id = claude_session_id,
    agent_runtime = 'claude'
WHERE claude_session_id IS NOT NULL
  AND claude_session_id != ''
  AND agent_session_id = '';

CREATE INDEX IF NOT EXISTS idx_subagents_agent_session
ON subagents(agent_runtime, agent_session_id, agent_id);
