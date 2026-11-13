# Architecture

A ~10,000ft view of how the mosaic pieces fit together.

```
                    ┌────────────────────────────┐
                    │     Bubble Tea TUI         │
                    │  (internal/tui/*.go)       │
                    └──────────────┬─────────────┘
                                   │ Model.Update(bus events + key msgs)
                                   ▼
   ┌─────────────── Event Bus (internal/bus) ─────────────────┐
   │                                                           │
   ▼            ▼              ▼              ▼              ▼
┌──────┐   ┌─────────┐   ┌──────────┐   ┌────────────┐   ┌──────────┐
│Store │   │  Tmux   │   │ Worktree │   │ Transcript │   │   Hook   │
│sqlite│   │ Manager │   │ Manager  │   │  Watcher   │   │  Server  │
└──┬───┘   └────┬────┘   └─────┬────┘   └─────┬──────┘   └─────┬────┘
   │            │              │              │              │
   ▼     tmux -L mosaic    git worktree    fsnotify on    unix socket
~/.mosaic/                  + declarative   ~/.claude/      /tmp/mosaic-<uid>/
state.db                    copy + setup    projects/      pid-<pid>
```

## Packages

### Foundation

| Package | Role |
|---|---|
| `internal/mosaicpath` | Every filesystem path mosaic touches (XDG-aware). |
| `internal/config` | TOML loader with defaults ← global ← project merge. |
| `internal/store` | sqlx + `modernc.org/sqlite`, schema migrations, typed repositories. |
| `internal/bus` | In-process pub/sub. Single-producer, fan-out to subscribers with per-sub channel + drop-on-full backpressure. |
| `internal/logging` | slog setup, file sink at `~/.mosaic/logs/mosaic.log`. |

### Session lifecycle

| Package | Role |
|---|---|
| `internal/git` | Thin wrapper around `git` CLI — worktree add/remove, diff, stage, commit. |
| `internal/worktree` | Declarative worktree creation: runs git then walks the setup recipe. |
| `internal/tmux` | tmux backend (`CLI` + `Fake`). `CLI` always uses `-L mosaic` and an isolated tmux.conf. |
| `internal/service` | Composes store + tmux + worktree + bus into the operations the CLI and TUI consume. Owns `SessionService`, `MessagingService`. |

### Agent observability

| Package | Role |
|---|---|
| `internal/claudeio` | Claude Code JSONL paths, transcript watcher (fsnotify + byte-offset tail), sub-agent state machine, prompt detector, OSC 777 / 9;4 scanner. |
| `internal/hook` | `~/.claude/settings.json` installer, unix socket server, wire protocol decoder, blocking-notification classifier. |
| `internal/live` | The glue: subscribes to the bus, runs sub-agent state machines, binds mosaic sessions to Claude session ids (via `live.Binder`), and persists attention / status into the store via `live.Reconciler`. |
| `internal/artifact` | Detects rich content in transcript entries: ```mermaid fences in assistant text and image / HTML files named by `Write` / `Edit` tool calls. Content-hashed so transcript replay is idempotent. |

### Conductor

| Package | Role |
|---|---|
| `internal/conductor` | TOML plan loader, DAG scheduler (Kahn + failure cascade), runner that dispatches tasks to `SessionService`. |

### Add-ons

| Package | Role |
|---|---|
| `internal/mcp` | MCP server discovery from `~/.claude.json`. Env values are dropped at the package boundary so secrets never leak. |
| `internal/skill` | Claude Code skill discovery from `~/.claude/skills/*/SKILL.md` (YAML frontmatter parser). |
| `internal/viewer` | Opens artifacts outside the TUI (cmux browser pane reused via `cmux browser goto`, or the OS browser) and prepares inline previews: headless-Chrome rasterization to PNG plus Kitty graphics encoding with Unicode placeholders (tmux passthrough aware). Writes pages and cached PNGs under the user cache dir. |

### TUI

| Package | Role |
|---|---|
| `internal/tui/theme` | 5 built-in colour themes + `ByName` lookup + chroma-aware sibling. |
| `internal/tui` | Bubble Tea Model / Update / View, plus all the modals (wizard, confirm, MCP, Skill, Diff, Conductor, Settings, Profile, ErrorLog, FirstRun). |

### Public API

| Package | Role |
|---|---|
| `pkg/transcript` | Claude Code JSONL types + decoder. Public so external tools can consume the same schema. |

## Data flow for a single Claude tool_use

```
 Claude writes a tool_use entry to ~/.claude/projects/<sanitized>/<session>.jsonl
                    │
                    ▼ fsnotify (inode watch)
 claudeio.Watcher reads the tail bytes and decodes one transcript.Entry
                    │
                    ▼ bus.Publish(TranscriptEntry)
 live.Reconciler receives the event
                    │
                    ├──→ updates the sub-agent state machine
                    │     → computes current_tool + started_at
                    │
                    ├──→ writes to subagents row via store.Subagents().UpdateRunningTool
                    │
                    ├──→ (if parent) flips sessions.status to running via store.Sessions().UpdateStatus
                    │
                    └──→ bus event ultimately signals the TUI
                                      │
                                      ▼ Model.busEvents channel
                    tui.Model.waitForBusEvent() returns busEventMsg
                                      │
                                      ▼
                    Model.Update(busEventMsg) schedules refreshCmd()
                                      │
                                      ▼
                    fetchRefreshed reads the store, rebuilds rows, renders
```

Typical latency end-to-end: 30–80ms. The tick loop runs at 1Hz as
a safety net for clock-only updates (elapsed timers, idle capture
refresh), but active changes never wait for it thanks to the bus
subscription in `NewModel`.

## Data flow for a hook event

```
 Claude fires a hook (UserPromptSubmit / Stop / Notification)
                    │
                    ▼
 echo "<worktreeID> <tabID> <surfaceID> <0|1|agent>" | nc -U $MOSAIC_SOCKET_PATH
                    │
                    ▼
 hook.Server.acceptLoop → handle → parseMessage
                    │
                    ▼ bus.Publish(BusyEvent | NotificationEvent)
 live.Reconciler.handleHookBusy / handleHookNotification
                    │
                    ├──→ clears attention + flips status on BusyEvent
                    ├──→ classifies notification via isBlockingNotification
                    └──→ sets AttentionWaitingInput only for real blocking prompts
                                      │
                                      ▼ (same bus → TUI refresh path as above)
```

Latency: typically 5–15ms end-to-end. `nc -U` does a single
round-trip to a unix socket; there is no JSON parsing for busy
events and a single `json.Unmarshal` for notifications.

## Data flow for an artifact

```
 Claude writes an assistant entry with a ```mermaid block, or a Write of report.html
                    │
                    ▼ bus.Publish(TranscriptEntry)   (same watcher path as above)
 live.Reconciler.recordArtifacts → artifact.DetectFromEntry
                    │
                    ├──→ store.Artifacts().Upsert   UNIQUE(session_id, content_hash)
                    │     → replayed entries insert nothing
                    └──→ bus.Publish(live.ArtifactDetected)   only for NEW rows
                                      │
                                      ▼ TUI refresh → m.artifacts[sessionID]
                    "◆ 2 artifacts" badge · `o` picker · [viewer] auto_open
                                      │
                                      ▼ viewer.TargetURL → file:// page or URL
                    viewer.Launcher.Open: Cmux (new-pane --type browser / goto) or System (open)
```

## Screen graph

```
                           ScreenTop
                               │
  ┌──────────┬───────────┬─────┴─────┬───────────┬──────────┐
  │          │           │           │           │          │
 Help      Wizard      Confirm    NewSession    Diff     Conductor
                                                 │
                                               (hunk
                                              navigation
                                              inside mode)
  │          │           │           │           │          │
  └──── q / esc returns to ScreenTop ────┘           │          │
                                                    │          │
                                             ScreenMCP      ScreenSettings
                                             ScreenSkill    ScreenErrorLog
                                             ScreenProfile  ScreenFirstRun
                                             ScreenArtifacts
```

Each modal is a single tea.Model state machine routed through
`handleKey` in `internal/tui/handlers.go`. The dispatch runs a
stack check ("is a modal active?") before touching the TOP-screen
handlers, so modals never leak keys.

## Performance budget

| Metric | Budget | Current |
|---|---|---|
| `mosaic ls` cold start | < 50 ms | ~20 ms |
| TUI first frame | < 200 ms | ~150 ms |
| `sessions.List` (100 rows) | < 5 ms | ~0.3 ms |
| `subagents.ListByParent` (x sessions) | < 10 ms | ~0.1 ms |
| `sessions.SetAttention` | < 1 ms | ~0.3 ms |
| bus dispatch latency | < 100 µs | ~15 µs |
| Memory at idle (100 sessions) | < 50 MB | ~25 MB |
| Binary size (stripped) | < 15 MB | ~13 MB |

Run `mosaic doctor --bench` to measure on your system. Run with
`--pprof :6060` for profiling.

## Testing strategy

- **Unit tests** (`go test ./...`) — pure functions (config merge,
  parsers, state machines, DAG).
- **Ginkgo BDD** (`internal/**/*_test.go`) — repositories, live
  reconciler, services. In-memory SQLite + fake tmux.
- **Godog e2e** (`features/*.feature`) — user journeys through
  the real CLI binary.
- **Manual** — the TUI is driven by a headless Bubble Tea model
  in tests, and the full experience is verified against
  `docs/quick-start.md`.

See `/internal/*/README.md` (when they exist) for
package-specific notes.
