# Next Codex Work

This note is a handoff for the next implementation session. It assumes
`main` includes the Codex hook/session groundwork through:

- `c672295 Generalize agent session metadata`
- `4e53dcc Add Codex launch and session binding support`
- `d69bd56 Support Codex hooks in onboarding`
- `9590d22 Document Codex integration options`

## Goals

Add two capabilities:

- Session-start integration: apply per-session MCP/Skill intent to the
  agent launch environment instead of only storing intent in SQLite.
- Codex MCP/Skill support: make the existing `m` and `s` TUI managers
  work for Codex sessions, not only Claude Code.

Do not start plugin support in the same change unless explicitly asked.
Plugin management is a separate feature because Codex plugins have
install/remove/marketplace side effects, unlike the current read-only
MCP/Skill discovery panels.

## Current State

- Hook support:
  - Claude: `mosaic hook install --tool claude` writes
    `~/.claude/settings.json` with `async: true`.
  - Codex: `mosaic hook install --tool codex` writes
    `~/.codex/hooks.json` without `async`, replacing old mosaic-owned
    async entries.
- Agent sessions:
  - `sessions.agent_session_id/source/path/cli_version` is now the
    generic binding.
  - Claude compatibility columns still exist and are mirrored for
    Claude only.
- Codex discovery:
  - `internal/codexio` discovers Codex rollout JSONL under
    `~/.codex/sessions/**/rollout-*.jsonl`.
- Existing MCP manager:
  - `internal/tui/mcp_panel.go` is wired to key `m`.
  - It discovers only Claude sources via `internal/mcp.Discover`:
    `~/.claude.json` and `~/.claude/settings.json`.
  - `session_mcps` stores attach intent by `(session_id, mcp_name)`.
- Existing Skill manager:
  - `internal/tui/skill_panel.go` is wired to key `s`.
  - It discovers only `~/.claude/skills/<name>/SKILL.md`.
  - `session_skills` stores attach intent by `(session_id, skill_name)`.

## Session-Start Integration

Implement a small launch-preparation layer in `internal/service` and call
it from `SessionService.resolveLaunchCommand` or immediately before
`buildLaunchTmuxOpts`.

Required behavior:

- For Claude sessions:
  - Preserve current launcher behavior.
  - If session MCPs/skills are attached, generate the same effective
    session-specific config that existing UI intent implies.
  - Do not delete or rewrite user-owned Claude config entries.
- For Codex sessions:
  - Attached MCPs should be made available to Codex via a session-local
    config overlay or launch flags.
  - Attached skills should be made available using Codex-compatible
    skill roots/config.
  - Prefer temporary/session-local generated files over mutating
    `~/.codex/config.toml`.

Suggested approach:

- Add a `PreparedLaunch` or `LaunchContext` helper that returns:
  - argv additions
  - env additions
  - generated config paths to clean up later if needed
- Store generated session files under the mosaic profile directory, for
  example `<profile>/sessions/<sessionID>/`.
- For Codex, prefer `--profile-v2 <name>` or `-c key=value` only if the
  current Codex CLI supports the needed config shape. Verify with
  `codex --help`, `codex mcp --help`, and a local config fixture before
  committing to a wire shape.

Acceptance criteria:

- Creating or respawning a session applies attached MCP/Skill intent.
- Generated config is deterministic and per-session.
- Existing user config files remain untouched.
- Respawn uses the same attachment intent as initial create.

## Codex MCP Support

Make MCP discovery runtime-aware.

Implementation targets:

- Extend `internal/mcp` with Codex TOML discovery.
  - Existing Claude JSON discovery must remain unchanged.
  - Add `DiscoverCodex(configPaths ...string)` or a generic
    `DiscoverForTool(tool, paths)` wrapper.
  - Codex source candidates:
    - `~/.codex/config.toml`
    - project `.codex/config.toml` if available from the selected
      session worktree
  - Parse `[mcp_servers.<name>]` tables.
  - Return the existing `mcp.Server` shape where possible:
    `Name`, `Source`, `Command`, `Args`, `Type`, `EnvKeys`.
  - Strip env values; never expose secrets in TUI.
- Update `internal/tui/mcp_panel.go`.
  - If selected session tool is `codex`, discover Codex MCP servers.
  - If selected session tool is `claude`, keep current Claude discovery.
  - Show source paths accurately in the detail pane.
  - Update empty-state copy to mention Codex paths when relevant.

Testing:

- Add `internal/mcp` tests for Codex TOML parsing.
- Add conflict/precedence tests for user config vs project config.
- Add TUI panel tests only if current test helpers make it practical;
  otherwise keep behavior-level tests in `internal/mcp`.

## Codex Skill Support

Make Skill discovery runtime-aware and support multiple roots.

Implementation targets:

- Extend `internal/skill`.
  - Existing `Discover(root string)` can remain.
  - Add `DiscoverMany(roots ...Root)` or similar, where each result
    records source/root.
  - Candidate Codex roots:
    - `<worktree>/.agents/skills`
    - `~/.agents/skills`
    - `~/.codex/skills` if present
  - Keep `SKILL.md` frontmatter parsing tolerant.
  - Name collisions should be deterministic. Recommended precedence:
    project `.agents/skills` > user `~/.agents/skills` > `~/.codex/skills`.
- Update `internal/tui/skill_panel.go`.
  - If selected session tool is `codex`, discover Codex roots.
  - If selected session tool is `claude`, keep current `~/.claude/skills`.
  - Show source path in the detail pane.
  - Update empty-state copy based on selected tool.

Testing:

- Add `internal/skill` tests for multi-root discovery and precedence.
- Verify duplicate skill names choose the higher-precedence root.
- Verify missing roots are ignored.

## Migration Considerations

The current `session_mcps` and `session_skills` tables are name-only.
That is acceptable for the next step if discovery is tool-aware and
attachments are interpreted within the selected session's tool.

If cross-tool duplicate names become ambiguous in practice, add a later
migration:

- `session_mcps.tool TEXT NOT NULL DEFAULT ''`
- `session_skills.tool TEXT NOT NULL DEFAULT ''`
- Backfill `tool` from `sessions.tool` by joining on `session_id`
- Update primary keys only in a separate migration because SQLite table
  rebuild is required.

Avoid that migration unless it is required by tests or real ambiguity.

## Manual Checks

Use the test binary pattern:

```bash
GOCACHE=/private/tmp/mosaic-go-cache go build -o /tmp/mosaic-test ./cmd/mosaic
/tmp/mosaic-test --home /tmp/mosaic-test-home --profile codex-test doctor
```

For first-run wizard:

```bash
/tmp/mosaic-test --home /tmp/mosaic-test-home --profile codex-test doctor --reset-wizard
/tmp/mosaic-test --home /tmp/mosaic-test-home --profile codex-test
```

For Codex hooks:

```bash
/tmp/mosaic-test hook install --tool codex
grep '"async"' ~/.codex/hooks.json && echo "bad: async remains"
```

For TUI managers:

- Create a Claude session and confirm `m`/`s` still show Claude sources.
- Create a Codex session and confirm `m`/`s` show Codex sources.
- Attach/detach entries and verify `session_mcps` / `session_skills`
  rows change without mutating user config files.
