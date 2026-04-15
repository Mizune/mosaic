-- 0006_drop_legacy_id.sql
-- Remove the legacy_id column left over from the agent-deck import.
-- The column only exists in databases imported from agent-deck;
-- fresh installs never had it.
--
-- DROP INDEX is safe with IF EXISTS. The DROP COLUMN is handled in
-- the Go post-migration hook because SQLite lacks IF EXISTS for
-- ALTER TABLE DROP COLUMN.

DROP INDEX IF EXISTS idx_sessions_legacy;
