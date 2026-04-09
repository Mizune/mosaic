# Configuration reference

mosaic reads configuration from TOML files, merged in this order
(later layers override earlier ones):

1. **Built-in defaults** (see `internal/config/config.go :: Default`).
2. **Global user config** — `~/.config/mosaic/config.toml` (or
   `$XDG_CONFIG_HOME/mosaic/config.toml`).
3. **Project config** — `<cwd>/.mosaic/config.toml`.

Every subcommand honours `--home` (override `$HOME`, used by tests)
and `--profile` (switch profile from the CLI).

Inspect the merged result with:

```bash
mosaic doctor --print-config
```

## Top-level tables

```toml
[mosaic]
# ...

[ui]
# ...

[ui.attention]
# ...

[ui.keymap]
# ...

[mosaic.tools.claude]
# ...

[tmux]
# ...

[worktree]
# ...

[worktree.setup.default]
# ...

[hooks]
# ...
```

## `[mosaic]` — global runtime

| Key | Type | Default | Description |
|---|---|---|---|
| `profile` | string | `"default"` | Profile subdirectory under `~/.mosaic/profiles/`. Each profile has its own SQLite database. |
| `data_dir` | string | `~/.mosaic` | Override the top-level data directory. |
| `log_level` | string | `"info"` | `debug`, `info`, `warn`, `error` |
| `default_tool` | string | `"claude"` | Which tool the new-session wizard selects by default. |

## `[mosaic.tools.<tool>]` — per-tool launch flags

Each agent can have its own flag set appended after the standard
launcher. Only `claude` honours the typed booleans today — `codex`
and `gemini` accept `extra_args` only.

```toml
[mosaic.tools.claude]
bypass_permissions = true        # adds --dangerously-skip-permissions
auto_mode          = true        # adds --auto
extra_args         = ["--model", "opus-4"]
```

mosaic never enables `--dangerously-skip-permissions` implicitly —
you must opt in via this table.

## `[ui]` — TUI appearance and behaviour

| Key | Type | Default | Description |
|---|---|---|---|
| `theme` | string | `"catppuccin-mocha"` | One of `catppuccin-mocha`, `catppuccin-latte`, `tokyo-night`, `tokyo-night-storm`, `system`, or a custom theme name. |
| `preview_refresh_ms` | int | `250` | How often the tmux capture-pane preview refreshes. |
| `list_width_ratio` | float | `0.35` | Fraction of the screen width reserved for the session list. |
| `auto_expand_subagents` | bool | `false` | Whether sub-agent rows are expanded by default on TUI open. |

## `[ui.attention]` — notifications

```toml
[ui.attention]
notify_os = true   # macOS osascript / Linux notify-send
sound     = false  # terminal BEL on new attention
```

Both default to `false` so fresh installs stay silent.

## `[ui.keymap]` — keybinding overrides

Every TUI binding is overridable. Keys are the internal binding
name (see `internal/tui/keys.go`), values are comma-separated key
chord lists. Empty value disables the binding entirely.

```toml
[ui.keymap]
quit          = "Q, ctrl+c"
kill          = "x, X"
mcp_manager   = "ctrl+m"
conductor     = "g"
diff          = "D"
filter_waiting = ""        # disable `!`
```

Supported binding names (aliases with `-` or `_` both accepted):

```
up, down, left, right, back,
quit, new, attach, kill, refresh, help, filter,
mcp_manager, skill_manager, settings, conductor, diff,
toggle_select, clear_select, select_all, fork,
filter_waiting, filter_running, filter_idle, filter_error, filter_all,
next_attention, dismiss_attention, error_log,
visual, visual_line, profile
```

See [keybindings.md](keybindings.md) for the default map.

## `[tmux]` — tmux backend

```toml
[tmux]
socket_name    = "mosaic"   # tmux -L <socket_name>
session_prefix = "mos_"     # session names become mos_<ulid>
```

mosaic always runs its own tmux server to avoid conflicting with
your regular sessions.

## `[worktree]` — declarative worktree setup

```toml
[worktree]
root                = "~/worktrees"  # (reserved; not wired yet)
default_base_branch = "main"
auto_worktree       = false
default_setup       = "default"
```

### Named setup recipes

Multiple named recipes let projects apply different copy / install
strategies. The wizard defaults to the recipe in `default_setup`.

```toml
[worktree.setup.default]
copy = [
  ".env",
  ".env.local",
  ".vscode/",
  ".cursor/",
]
copy_extra     = []       # appended to `copy` (useful for project overrides)
copy_ignored   = true     # mirror every .gitignore'd file
copy_untracked = true     # mirror every untracked file in the source repo
commands       = ["pnpm install"]
commands_extra = []
post_create_hook = "scripts/mosaic-post-create.sh"  # run after commands
env            = { NODE_ENV = "development" }
```

### Merge semantics

When both a global and project config set the same recipe, the
project overrides it **list-by-list** — `copy` replaces (use
`copy_extra` to append), `commands` replaces, `env` merges
key-by-key.

## `[hooks]` — Claude Code hook integration

```toml
[hooks]
suggested = true            # wizard suggests install on first run
events    = []              # reserved; not wired yet
```

Hooks themselves live in `~/.claude/settings.json` and are managed
by `mosaic hook install` / `uninstall`. See [hooks.md](hooks.md).

## Example `config.toml`

A complete example a power user might write:

```toml
[mosaic]
profile     = "work"
log_level   = "debug"
default_tool = "claude"

[mosaic.tools.claude]
bypass_permissions = true
auto_mode          = false
extra_args         = ["--model", "claude-opus-4"]

[ui]
theme = "tokyo-night"

[ui.attention]
notify_os = true
sound     = false

[ui.keymap]
kill     = "x, X"
new      = "c"
conductor = "C"

[tmux]
socket_name = "mosaic"

[worktree]
default_base_branch = "develop"
auto_worktree       = true

[worktree.setup.default]
copy           = [".env", ".env.local", ".vscode/", "turbo.json"]
copy_ignored   = true
commands       = ["pnpm install --frozen-lockfile", "pnpm db:generate"]
post_create_hook = "scripts/mosaic-setup.sh"
env            = { NODE_ENV = "development" }

[worktree.setup.fast]
copy     = [".env", ".env.local"]
commands = []
```
