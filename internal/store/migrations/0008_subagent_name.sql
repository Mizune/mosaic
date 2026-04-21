-- 0008_subagent_name.sql
-- Add name column for addressable sub-agents (Claude Code SendMessage).
ALTER TABLE subagents ADD COLUMN name TEXT;
