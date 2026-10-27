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

[viewer]
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
launcher. `claude` and `codex` both have typed options; other tools
accept `extra_args`.

```toml
[mosaic.tools.claude]
bypass_permissions = true        # adds --dangerously-skip-permissions
auto_mode          = true        # adds --auto
extra_args         = ["--model", "opus-4"]

[mosaic.tools.codex]
model           = "gpt-5-codex"
sandbox         = "workspace-write"  # read-only | workspace-write | danger-full-access
approval_policy = "on-request"       # untrusted | on-request | never
web_search      = true               # adds --search
no_alt_screen   = true               # adds --no-alt-screen
auto_continue   = true               # new sessions use `codex resume --last`
extra_args      = ["--strict-config"]
```

mosaic never enables Claude `--dangerously-skip-permissions` or Codex
`--dangerously-bypass-approvals-and-sandbox` implicitly. You must opt in
via this table.

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
mcp_manager, skill_manager, settings, conductor, diff, artifacts,
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

## `[hooks]` — agent hook integration

```toml
[hooks]
suggested = true            # wizard suggests install on first run
events    = []              # reserved; not wired yet
```

Claude hooks live in `~/.claude/settings.json`; Codex hooks live in
`~/.codex/hooks.json`. Manage them with `mosaic hook install --tool
claude` or `mosaic hook install --tool codex`. See [hooks.md](hooks.md).

## `[viewer]` — artifact viewer

Controls how the artifact picker (`o`) shows Mermaid diagrams, images
and HTML files produced by an agent. See
[artifact-viewer.md](artifact-viewer.md) for the feature guide.

```toml
[viewer]
enabled        = true
auto_open      = false
placement      = "dock"
focus          = false
mermaid_js_url = "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"
inline         = "auto"
chrome_path    = ""
```

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | When `false`, the picker still lists artifacts but `Enter` only shows a "viewer disabled" toast. |
| `auto_open` | bool | `false` | Open every newly detected artifact as soon as it appears. Artifacts replayed from a transcript at startup never auto-open. |
| `placement` | string | `"dock"` | Where cmux opens the browser pane: `dock` (right-hand sidebar Dock), `right` or `down` (a split of the current workspace). Ignored outside cmux. |
| `focus` | bool | `false` | Move keyboard focus to the browser pane when it opens. |
| `mermaid_js_url` | string | jsdelivr `mermaid@11` | Script URL loaded by generated Mermaid pages. Point it at a local `file://` copy of `mermaid.min.js` (the UMD build, not the ESM one) to render offline. |
| `inline` | string | `"auto"` | Inline preview in the picker via the Kitty graphics protocol. `auto` enables it when the terminal advertises support (Ghostty, kitty, WezTerm) and, inside tmux, `allow-passthrough` is `on`; `on` skips the terminal check; `off` disables it. |
| `chrome_path` | string | `""` | Chrome / Chromium binary used to rasterize Mermaid, HTML, SVG and URL artifacts for the inline preview. Empty auto-detects the macOS app bundles and `google-chrome` / `chromium` on `PATH`. |

mosaic uses the cmux browser pane when `CMUX_SOCKET_PATH` is set and
the `cmux` CLI is on `PATH`; the pane is reused for later artifacts.
Everywhere else it falls back to `open` (macOS) or `xdg-open` (Linux).
Generated pages live under `~/Library/Caches/mosaic/artifacts/<session>/`
(or `$XDG_CACHE_HOME/mosaic/artifacts/`) and are removed when the
session is killed.

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
