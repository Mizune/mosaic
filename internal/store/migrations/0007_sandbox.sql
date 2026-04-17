-- 0007_sandbox.sql
-- Phase 7: per-session Docker sandbox metadata.
--
-- A session can optionally launch its agent inside a Docker
-- container. We persist the selected sandbox mode ("none" or
-- "docker") and, when docker is in use, the container name so
-- cleanup code can `docker rm -f` on Stop/Delete without having
-- to re-derive the name from the ULID.

ALTER TABLE sessions ADD COLUMN sandbox_mode      TEXT NOT NULL DEFAULT 'none';
ALTER TABLE sessions ADD COLUMN sandbox_container TEXT NOT NULL DEFAULT '';
