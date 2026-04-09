-- 0005_session_skills.sql
-- Phase 5: per-session Claude Code skill attachment.
--
-- Skills live on disk at ~/.claude/skills/<name>/SKILL.md; mosaic
-- discovers them every time the Skill Manager modal opens. This
-- table records which skills the user has attached to which
-- session so the selection survives restarts.

CREATE TABLE IF NOT EXISTS session_skills (
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    skill_name   TEXT NOT NULL,
    attached_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (session_id, skill_name)
);

CREATE INDEX IF NOT EXISTS idx_session_skills_session ON session_skills(session_id);
