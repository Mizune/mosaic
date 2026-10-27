# Changelog

All notable changes to mosaic are recorded here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## Unreleased

### Added

- Artifact viewer: Mermaid blocks and image / HTML files written by
  an agent are detected from Claude Code transcripts, listed per
  session (`o`, badge in the preview tab strip) and opened in a cmux
  browser pane or the OS browser; `[viewer]` config with `auto_open`
- Inline artifact preview in the picker through the Kitty graphics
  protocol (Ghostty, kitty, WezTerm; tmux with `allow-passthrough on`),
  rasterizing Mermaid / HTML / SVG with a headless Chrome
- Full lazygit-style TUI with structured preview, sub-agent
  inline rows, attention pulse, and 4-mode right pane (Tab
  cycles structured / live / sub-agents / diff)
- Conductor: TOML plans, DAG scheduler, CLI + TUI panel, inline
  message send, new plan run + replay from the TUI
- Diff View: hunk-level staging, commit amend, chroma syntax
  highlighting, filename fuzzy search, yank to clipboard
- Multi-select with Space / v / V + batch kill
- Session fork (`f`)
- 5 built-in themes (Catppuccin Mocha/Latte, Tokyo Night + Storm,
  System) with live preview in the first-run wizard
- OS notifications on new attention events (macOS osascript,
  Linux notify-send) + optional terminal bell
- First-run onboarding wizard (theme / hooks)
- Profile switcher TUI modal
- Settings editor (in-TUI field toggle with Ctrl+S save)
- `mosaic doctor --bench` performance budget runner
- `--pprof :6060` flag for runtime profiling
- `mosaic.tools.<tool>` config table for per-tool launch flags
  (`bypass_permissions`, `auto_mode`, `extra_args`)
- Full Claude Code hook protocol installer (install / uninstall /
  status), unix socket server, blocking-notification classifier

### Changed

- Session status defaults to `idle`; reconciler flips to
  `running` / back to `idle` based on real activity instead of
  marking every new session as running
- Diff View base ref falls back through
  `<preferred> → main → master → origin/HEAD → HEAD` so
  `ambiguous argument` errors no longer surface
- Attach auto-resumes Claude Code via `--continue` when a prior
  transcript exists for the worktree
- Header redesigned with two-row layout showing brand, profile,
  session counts, sub-agents, filter / visual chips, status
  badges, and clock

### Fixed

- OSC suppression: the same OSC sequence sitting in the tmux
  visible buffer no longer re-fires attention after the user
  dismisses it via a hook
- `Notification` hook classifier: Claude's routine "waiting for
  your input" cue is filtered out so attention does not stick
  permanently
- `tmux.HasSession` treats both `no server running` and
  `no session` as "not found", letting fresh mosaic installs
  attach to imported rows without crashing
- Diff View no longer dumps binary file contents to the right
  pane (detects NUL bytes like git does)
- Long monorepo paths in the Diff View use middle ellipsis so
  filenames stay visible

## 0.1.0 — unreleased (first public tag)

First public tag once the above items ship.
