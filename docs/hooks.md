# Agent hooks

mosaic installs a small set of hooks so Claude Code or Codex can notify
mosaic the moment the agent's state changes. With hooks, mosaic sees
busy/idle transitions in single milliseconds; without them it still
works via transcript discovery and tmux OSC scanning where available,
just with higher latency.

Installation is opt-in, idempotent, and only adds entries that
carry the `MOSAIC_SOCKET_PATH` marker so uninstall never touches
hooks owned by other tools.

## Install / uninstall / status

```bash
mosaic hook install                  # Claude: ~/.claude/settings.json
mosaic hook install --tool codex     # Codex: ~/.codex/hooks.json
mosaic hook status --tool codex      # ✓ / ✗ indicator
mosaic hook uninstall --tool codex   # removes only the mosaic entries
```

All three commands accept `--settings <path>` to override the
target file (tests use this). The default is `~/.claude/settings.json`
for Claude and `~/.codex/hooks.json` for Codex.

The installer is a transactional JSON merge: it writes to a
`.tmp` file and renames into place, so a crashed install never
leaves you with a half-written config.

## What gets installed

Claude installs five events, each wired to a one-liner that sends to mosaic's
per-process unix socket via `nc -U`:

| Event | What mosaic does with it |
|---|---|
| `UserPromptSubmit` | flips the session to `running`, clears any stale attention |
| `Stop` | flips the session to `idle`, clears any pending attention |
| `PostToolUseFailure` | same as `Stop` |
| `SessionEnd` | same as `Stop` |
| `Notification` | blocking notifications (`permission required`, `approve`, …) flip the session to `⚠ waiting_input` |

Codex installs `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`,
and `PermissionRequest`. `PermissionRequest` is treated as blocking
attention; `PreToolUse` marks the session running; `PostToolUse` and
`Stop` mark it idle.

All of them gate on four environment variables:

```
MOSAIC_SOCKET_PATH     /tmp/mosaic-<uid>/pid-<pid>
MOSAIC_WORKTREE_ID     mosaic session id (ULID)
MOSAIC_TAB_ID          same ULID for now
MOSAIC_SURFACE_ID      "tmux"
```

Which mosaic exports into every tmux session it spawns via
`tmux new-session -e KEY=VALUE`. If you launch Claude Code
outside of mosaic the env vars are absent and the hook is a
silent no-op.

## The wire protocol

The socket format is deliberately minimal — supacode-compatible
so a future reverse-compatibility story stays on the table.

### Busy events

One line, four space-separated fields:

```
<worktreeID> <tabID> <surfaceID> <0|1>
```

`1` = busy (UserPromptSubmit), `0` = idle (Stop / friends).

### Notification events

One header line + a JSON body:

```
<worktreeID> <tabID> <surfaceID> <agent>
{"hook_event_name":"Notification","title":"Permission required",...}
```

mosaic parses the header, reads the JSON, and feeds the result
into the reconciler which classifies it via
`isBlockingNotification` (see
`internal/live/reconciler.go`) — the routine "Claude is waiting
for your input" idle notification is filtered out, only real
"permission required" messages flip the session to waiting.

## Troubleshooting

### `mosaic hook status` says installed but attention never updates

Check that `MOSAIC_SOCKET_PATH` is actually set inside the tmux
pane:

```bash
# Inside a mosaic-attached session:
env | grep MOSAIC_
```

You should see the four variables. If not, mosaic didn't spawn
this tmux session — detach
and reattach via `mosaic attach <name>`.

### The socket file is missing

mosaic puts per-process sockets at `/tmp/mosaic-<uid>/pid-<pid>`.
On a clean boot they appear when `mosaic` (the TUI) or any
command that starts the hook server runs. If there's no mosaic
TUI running, the socket is absent and the hook command
short-circuits via its env check — it writes nothing and
silently exits.

### Claude Code does not fire the expected events

- Make sure your Claude Code version supports `UserPromptSubmit`
  and `Notification` hooks (any release since 1.2 should).
- Re-install hooks with `mosaic hook install` — the installer
  is idempotent and cheap.
- Inspect `~/.claude/settings.json` for the mosaic entries and
  confirm they carry the `MOSAIC_SOCKET_PATH` marker.

### Removing hooks before uninstalling mosaic

```bash
mosaic hook uninstall
```

…strips only the mosaic-owned entries. Any hooks added by
other tools (supacode, custom scripts) are left untouched.
