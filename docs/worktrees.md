# Worktrees

mosaic creates a fresh git worktree for every session you opt into
with `--worktree` (or the equivalent checkbox in the new-session
wizard). A worktree gives the session its own working directory
and its own branch, so parallel sessions never step on each
other's changes.

The setup is declarative — a named recipe in `config.toml`
describes what mosaic should copy into the new worktree and what
commands to run after checkout, so spinning up "a fresh feature
branch with .env, node_modules, and pnpm-generated types" is one
keystroke.

## How it works

1. `mosaic new feat-x --worktree --branch feat-x` (or the wizard
   answer) calls `SessionService.Create` with
   `CreateWorktree: true`.
2. `worktree.Manager.Create` runs `git worktree add -b feat-x
   <path> <base>`.
3. It then walks the selected setup recipe:
   - copies every file listed in `copy`
   - optionally mirrors `.gitignore`'d files (`copy_ignored`)
   - optionally mirrors untracked files (`copy_untracked`)
   - runs every entry in `commands` sequentially
   - runs the `post_create_hook` script if set
4. Only after all of that does tmux spawn the agent in the new
   directory.

If any step fails, mosaic rolls back: the worktree is removed
and the session row is deleted, so the user sees a clean error
instead of a half-initialised state.

## Defining a recipe

In `~/.config/mosaic/config.toml`:

```toml
[worktree]
default_base_branch = "main"
default_setup       = "default"

[worktree.setup.default]
copy = [
  ".env",
  ".env.local",
  ".vscode/",
  ".cursor/",
]
copy_ignored   = false
copy_untracked = false
commands       = ["pnpm install"]
post_create_hook = ""
env            = { NODE_ENV = "development" }
```

The recipe name ("default" above) is referenced by the wizard's
setup picker and by the `--setup <name>` CLI flag.

### Field reference

| Field | Type | Default | Description |
|---|---|---|---|
| `copy` | []string | `[]` | Relative paths (files or directories) copied from the source worktree into the new one. Directory entries must end with `/`. |
| `copy_extra` | []string | `[]` | Appended to `copy`. Useful for project configs that want to extend the global recipe without restating its list. |
| `copy_ignored` | bool | `false` | Copy every file that matches the repo's `.gitignore` (e.g. `.env.local`, `node_modules`). |
| `copy_untracked` | bool | `false` | Copy every untracked file in the source worktree. |
| `commands` | []string | `[]` | Shell commands run sequentially, cwd = the new worktree. |
| `commands_extra` | []string | `[]` | Appended to `commands`. |
| `post_create_hook` | string | `""` | Path (relative to the project) to a script run after `commands`. |
| `env` | map[string]string | `{}` | Environment variables exported when running `commands` and `post_create_hook`. Merged with the user's environment. |

### Project overrides

`<project>/.mosaic/config.toml` can replace or extend the
global recipe. Lists (`copy`, `commands`) replace by default;
use the `_extra` variants to append.

```toml
# <project>/.mosaic/config.toml
[worktree.setup.default]
copy_extra     = ["turbo.json", "pnpm-workspace.yaml"]
commands       = ["pnpm install --frozen-lockfile", "pnpm db:generate"]
post_create_hook = "scripts/mosaic-post-create.sh"
```

Scalars (`post_create_hook`, `copy_ignored`) always take the
project value when set.

## CLI usage

```bash
# Use the default setup recipe.
mosaic new feat-x --worktree --branch feat-x

# Pick a specific recipe.
mosaic new feat-x --worktree --branch feat-x --setup fast

# Override the base ref.
mosaic new hotfix --worktree --branch hotfix --base develop
```

The wizard (press `n` in the TUI) asks the same questions in a
4-step form.

## Cleanup

When you kill a session, mosaic **does not** automatically delete
its worktree — your changes might be unpushed. To remove both the
session row and the worktree:

```bash
# Session row only:
mosaic kill feat-x

# Worktree cleanup (manual, for now):
git -C /path/to/project worktree remove .worktrees/feat-x
```

Future polish is to add a `--remove-worktree` flag that chains
both steps.

## Troubleshooting

### "fatal: '<branch>' is already checked out at <path>"

The branch is already attached to a worktree (maybe a leftover
from a previous session). Run

```bash
git worktree list
git worktree remove <path>
```

to drop the orphan, then retry `mosaic new`.

### "No such file or directory" during copy

One of the entries in `copy` does not exist in the source repo.
mosaic logs the failing path so you can fix the recipe; absent
files are skipped silently for `copy_ignored` and `copy_untracked`
because those are fundamentally "copy everything that matches"
operations.

### `pnpm install` takes forever every time

Use `copy_ignored = true` so the existing `node_modules` is
mirrored, then set `commands = ["pnpm install"]`. pnpm will reuse
whatever is already on disk and finish in seconds.
