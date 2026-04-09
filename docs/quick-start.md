# Quick start

This guide walks you from "nothing installed" to "10 parallel Claude
sessions, one of them waiting for your approval, the TUI showing it
to you with a red ⚠ row" in about fifteen minutes.

## 1. Install

```bash
git clone https://github.com/Mizune/mosaic.git
cd mosaic
make build
./bin/mosaic doctor
```

See the README for the `gh release download` path if you just
want the pre-built binary on another machine you own.

## 2. Verify the environment

```bash
mosaic doctor
```

Expected output (colours added):

```
  ✓ data dir   /Users/you/.mosaic
  ✓ config     defaults only (no global or project file)
  ✓ sqlite     /Users/you/.mosaic/profiles/default/state.db (migrations: 5)
  ✓ tmux       /opt/homebrew/bin/tmux
  ✓ git        /opt/homebrew/bin/git
  ✓ tmux conf  /Users/you/.mosaic/tmux.conf
  ! hooks      not installed — run `mosaic hook install` for sub-second attention updates
  ✓ logs       /Users/you/.mosaic/logs/mosaic.log
  ✓ ready      mosaic v0.1.0
```

`doctor` is idempotent — re-run it any time something feels off. It
will recreate missing directories, re-apply SQL migrations, and
regenerate `~/.mosaic/tmux.conf`.

## 3. Install Claude Code hooks (recommended)

Hooks give mosaic sub-second updates on busy / idle transitions and
permission prompts. Without them mosaic still works via fsnotify +
OSC scanning, but with ~100ms extra latency.

```bash
mosaic hook install
mosaic hook status   # should now print "✓ mosaic hooks installed"
```

The installer modifies `~/.claude/settings.json` idempotently — it
only adds mosaic-owned entries and leaves any existing hooks alone.
Running `mosaic hook uninstall` reverses only the mosaic additions.

## 4. Launch the TUI

```bash
mosaic
```

On the very first launch you get a 2-step onboarding wizard:

1. **Theme** — live preview of 5 built-in themes (arrow keys move,
   Enter commits). See [configuration.md](configuration.md#ui) for
   the full list.
2. **Hooks** — offers to install Claude Code hooks (skip if you
   already did step 3). Press Enter to finish and land on the TOP screen.

The wizard only runs once; you can re-run it any time with
`mosaic doctor --reset-wizard`.

## 5. Create your first session

From the TOP screen, press `n` to open the new-session wizard:

1. **Name** — fuzzy-autocompletes recent names.
2. **Project** — existing projects in a list, or "+ new" to pick
   the current directory.
3. **Tool** — claude / codex / gemini / shell.
4. **Worktree & setup** — optionally create a git worktree on a
   fresh branch, with a setup recipe (`.env` copy, `pnpm install`,
   post-create hook).

Press `Enter` (or `a`) on a session row to attach. Inside the tmux
pane you can use Claude Code normally; press `Ctrl+Q` to detach
back to mosaic.

## 6. Watch attention in real time

Have Claude run a command that asks for permission (e.g. `Bash(rm …)`):

- The hook fires → mosaic's reconciler sees the Notification event
  → the session row turns into `⚠ wait` with a pulsing red background.
- `⚠ 1 waiting` appears in the header badge row.
- Press `N` (capital) to jump to the next waiting session.
- Press `Enter` to attach and approve / deny.
- Press `D` to dismiss a false-positive attention.

This is the killer feature — once you're running 10+ sessions you'll
rely on this every minute.

## 7. Try the conductor

Create a plan file:

```bash
cat > /tmp/demo.toml <<'EOF'
[conductor]
name    = "demo"
project = "/tmp/demo"

[[task]]
name   = "scaffold"
tool   = "claude"
prompt = "Create src/hello.go with a function that prints 'hello from scaffold'"

[[task]]
name       = "tests"
tool       = "claude"
depends_on = ["scaffold"]
prompt     = "Read src/hello.go and add a unit test in src/hello_test.go"
EOF

mkdir -p /tmp/demo && cd /tmp/demo && git init -q --initial-branch=main
git commit -q --allow-empty -m init

mosaic conductor run /tmp/demo.toml
```

Watch mosaic spawn two worker sessions (`demo-scaffold`, `demo-tests`)
in their own worktrees, dispatch the prompts to Claude, and track
them through to completion. Press `c` in the TUI to open the DAG
panel.

See [conductor.md](conductor.md) for the full reference.

## 8. Explore the polish

- `,` — Settings (theme / log level / notify_os, interactively
  editable with `Ctrl+S` to save).
- `m` — MCP Manager (discover and attach Claude Code MCP servers).
- `s` — Skill Manager (discover and attach `~/.claude/skills/`).
- `d` — Diff View (lazygit-style, supports hunk staging and inline
  commit).
- `E` — Error log history.
- `P` — Profile switcher.
- `?` — Full help.

## Where to go next

- [Configuration reference](configuration.md) — every knob in `config.toml`
- [Keybindings](keybindings.md) — cheatsheet
- [Conductor plans](conductor.md) — multi-agent DAGs
