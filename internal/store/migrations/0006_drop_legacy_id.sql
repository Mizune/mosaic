-- 0006_drop_legacy_id.sql
-- Remove the legacy_id column left over from the agent-deck import.
-- The column is no longer referenced in application code.

DROP INDEX IF EXISTS idx_sessions_legacy;
ALTER TABLE sessions DROP COLUMN legacy_id;
