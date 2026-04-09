# Diff View

The Diff View (`d` from the TOP screen) is a lazygit-inspired
file-level diff browser that works directly on a mosaic session's
worktree. It supports browsing, filtering, searching, hunk-level
staging, inline commit + amend, clipboard yank, and chroma
syntax highlighting.

## Opening it

From the TOP screen, put the cursor on any session row and
press `d`. The Diff View replaces the TOP screen until you
close it.

## What it compares

By default the base ref is the `[worktree] default_base_branch`
value from `config.toml` (which itself defaults to `main`).
The git client falls back through a short list of candidates if
that ref does not resolve:

```
<configured base> → main → master → origin/HEAD → HEAD
```

So Diff View Just Works even in fresh clones that do not have
a `main` branch. The actually-used ref is displayed in the
footer so you know what you are comparing against.

The diff includes **every change between the base ref and the
current worktree state**, meaning:

- commits on the current branch that the base does not have
- unstaged modifications
- staged modifications
- untracked files (shown with a `?` status and a synthetic
  `/dev/null → file` patch)

Binary files (detected by a NUL byte in the first 8 KB) and
files larger than 256 KB get a stub preview (`(binary file, N
bytes — preview suppressed)`) so the right pane never dumps
garbled bytes.

## Layout

```
┌─ Diff: feat-auth ───────────────────────────────────────────┐
│ Files (5)  +47/-12     │ src/api/auth.ts                    │
│ ●▶ M src/api/auth.ts    │ @@ -12,7 +12,12 @@                │
│    +18/-3  ●           │  function loginHandler(req, res) { │
│  M src/api/router.ts    │ -  const token = req.body.token   │
│    +15/-2               │ +  const token = extractToken(req) │
│  M tests/auth_test.ts   │ +  if (!token) {                  │
│    +14/-7               │ +    return res.status(401).send() │
│  A pkg/auth/oauth.go    │ +  }                              │
│    new                  │   const user = await verify(token)│
│  ? .env.local           │   ...                             │
│    new                  │                                    │
├─────────────────────────┴────────────────────────────────────┤
│  base main  ·  all  ·  a/M/N/S filter · /:search · l:full … │
└──────────────────────────────────────────────────────────────┘
```

- **Left pane** — one row per changed file. Columns:
  1. cursor indicator (`▶ `)
  2. status glyph (`M` modified, `A` added, `D` deleted, `R`
     renamed, `?` untracked)
  3. stage marker (`●` when any changes are staged)
  4. path (middle-ellipsised so long monorepo paths keep the
     filename visible)
  5. `+N/-M` line counts
- **Right pane** — the unified diff of the highlighted file,
  coloured with green `+` / red `-` / muted headers, and
  chroma-highlighted code content where a lexer matches the
  file extension.
- **Footer** — the base ref + active filter + key cheat sheet.

## Keybindings

### Browse mode

| Key | Action |
|---|---|
| `j` / `k` / `↓` / `↑` | Move the file cursor |
| `J` / `K` / `pgdown` / `pgup` | Scroll the right-pane body |
| `l` / `Enter` | Drill into the full-file view |
| `[` / `]` | Previous / next hunk inside the current file |
| `a` | Filter: all |
| `M` | Filter: modified only |
| `N` | Filter: new / untracked only |
| `S` | Filter: staged only |
| `/` | Filename fuzzy search |
| `s` | Stage / unstage the whole file |
| `H` | Stage just the currently focused hunk |
| `c` | Commit staged changes (inline modal) |
| `C` | Commit amend (pre-fills HEAD message) |
| `y` | Yank the current file's diff to the clipboard |
| `Y` | Yank every filtered file's diff |
| `R` | Refresh from disk |
| `q` / `Esc` | Close Diff View |

### Full-file mode

Reached via `l` or `Enter` on any file. The left pane collapses
and the diff takes the whole modal.

| Key | Action |
|---|---|
| `J` / `K` | Scroll |
| `y` | Yank |
| `s` | Stage |
| `h` / `q` / `Esc` | Back to browse |

### Commit / amend modal

Triggered by `c` (new commit) or `C` (amend HEAD).

| Key | Action |
|---|---|
| printable keys | Type into the message buffer |
| `backspace` | Delete last rune |
| `Enter` | Run `git commit -m <message>` (or `--amend` for `C`) |
| `Esc` | Cancel |

Commits are never run with `--no-verify` or `--no-gpg-sign` — if
your pre-commit hook rejects the commit, the error is surfaced
verbatim and the modal stays open so you can fix the message
and retry.

## Filters

- **all** (`a`): no filter. Everything shows up.
- **modified** (`M`): only rows with status `M` (tracked files
  with content changes).
- **new** (`N`): only untracked files and tracked additions.
- **staged** (`S`): only rows where the staged marker (`●`)
  is set.

Filters are exclusive — switching tabs does not reset the
`/` substring search. Pressing the same tab key twice toggles
back to "all".

## Filename search

Press `/` to open the input line at the bottom. Typing narrows
the file list by substring (case-insensitive) on every
keystroke. `Enter` commits the search (the filter persists
until you press `/` again or `Esc`). `Esc` cancels and wipes
the query.

The active query is shown in the footer as `search: foo` until
cleared so you never forget a filter is running.

## Hunk-level staging

`s` stages whole files. `H` stages the single hunk currently
focused in the right pane:

1. Navigate files with `j`/`k` until the right one is
   highlighted.
2. Use `[`/`]` to step between hunks within that file.
3. Press `H` to stage just that hunk.

Under the hood mosaic calls `ParseDiffHunks` on the cached
unified diff for the file, builds a mini-patch with the file's
original `--- a/... +++ b/...` preamble plus only the selected
hunk, and pipes it to `git apply --cached`. If the hunk no
longer applies cleanly (e.g. the working tree changed after the
Diff View loaded), git's error is surfaced as a toast and the
Diff View refreshes so you can try again.

Unstaging a single hunk is not yet supported — use `s` on the
whole file to unstage everything, then re-stage the hunks you
want to keep.

## Clipboard yank

- `y` copies the **currently highlighted file's** unified diff
  (as plain text) to the system clipboard. The copy goes
  through `github.com/atotto/clipboard`, which shells out to
  `pbcopy` / `xclip` / `wl-copy` depending on the platform.
- `Y` copies the **concatenated diff of every filtered file**.
  Files that have not been visited yet are loaded from disk
  synchronously before the copy so `Y` always produces a
  complete patch.
- The toast footer confirms the yank with the file name (or
  total file count for `Y`).
- Failed clipboard writes turn into a visible toast error.

## Syntax highlighting

Diff content lines (the actual code, minus the leading `+` /
`-`) are run through `github.com/alecthomas/chroma/v2` with a
lexer chosen from the file's extension. The style picked
tracks the active mosaic theme via
`chromaStyleForTheme`:

| mosaic theme | chroma style |
|---|---|
| `catppuccin-mocha` | `catppuccin-mocha` → `dracula` (fallback) |
| `catppuccin-latte` | `catppuccin-latte` → `github` |
| `tokyo-night` | `tokyonight-night` → `dracula` |
| `tokyo-night-storm` | `tokyonight-storm` → `tokyonight-night` |
| `system` | `bw` (black & white) |

If chroma has no lexer for the file extension, the line is
printed unchanged (still coloured green / red by the diff
marker). Languages currently mapped: Go, TypeScript, JavaScript,
Python, Rust, Markdown, JSON, YAML, TOML, Bash, SQL, HTML, CSS.
See `lexerName` in `internal/tui/diff_panel.go` to add more.

## Troubleshooting

### "git diff: no usable base ref in <repo>"

The worktree has no `main`, `master`, or `origin/HEAD`. Either
add one (`git branch main`) or set `[worktree] default_base_branch`
to something that exists.

### `(binary file, 4123 bytes — preview suppressed)`

Expected. mosaic detects binaries by the same NUL-byte
heuristic git uses. If you want to inspect the raw bytes, open
the file in an external tool.

### The Diff View is empty after I yanked

Yanking does not filter. If the file list went empty after `y`
you probably also pressed a filter key — try `a` to return to
"all".

### Commit fails with "commit message is required"

Even with the modal showing text, whitespace-only messages get
rejected client-side so you can fix typos without losing the
commit state. Add a real subject and retry.
