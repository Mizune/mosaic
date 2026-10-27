# Keybindings

Every binding listed here is overridable via `[ui.keymap]` in
`config.toml`. See [configuration.md](configuration.md#ui-keymap).

## TOP screen

### Navigation

| Key | Action |
|---|---|
| `j` / `↓` | move cursor down |
| `k` / `↑` | move cursor up |
| `h` / `←` | collapse group (or jump to parent group) |
| `l` / `→` | expand group |
| `Tab` | cycle right-pane preview mode (structured / live / sub-agents / diff) |
| `1`–`9` | jump to group N (planned) |
| `R` | refresh immediately |
| `?` | toggle help overlay |
| `q` / `Ctrl+C` | quit |

### Session actions

| Key | Action |
|---|---|
| `Enter` / `a` | attach to the selected session |
| `n` | new session wizard |
| `f` | quick fork (auto-name + same setup) |
| `K` | kill (single row, or whole selection) |
| `d` | open Diff View |
| `o` | open artifact picker (mermaid / images / html → browser pane) |
| `c` | open Conductor Panel |
| `m` | open MCP Manager |
| `s` | open Skill Manager |
| `,` | open Settings |
| `P` | profile switcher |
| `E` | error log history |

### Multi-select

| Key | Action |
|---|---|
| `Space` | mark / unmark current session |
| `v` | toggle visual-range mode (j/k extends the selection) |
| `V` | mark every session in the current group |
| `*` | mark all visible sessions |
| `Ctrl+\` | clear the selection |

### Attention & filters

| Key | Action |
|---|---|
| `!` | filter: waiting |
| `@` | filter: running |
| `#` | filter: idle |
| `$` | filter: error |
| `0` | filter: all (or press the same tab key twice) |
| `N` | jump to next `⚠ waiting` session |
| `D` | dismiss attention on current row |

## Attached tmux session

| Key | Action |
|---|---|
| `Ctrl+Q` | detach back to mosaic |
| `Ctrl+B` … | standard tmux prefix — mosaic never touches it |

The `Ctrl+Q` binding lives in the mosaic-managed tmux config at
`~/.mosaic/tmux.conf`; `mosaic doctor` regenerates it on every run
so upgrades pick up the latest defaults automatically.

## New session wizard

| Key | Action |
|---|---|
| `Tab` / `Enter` | advance to the next step |
| `Shift+Tab` | go back |
| `Esc` | cancel |
| `/` | fuzzy filter (step 2–4) |
| `Ctrl+Enter` | create immediately with current answers |

## Diff View

### Browse mode

| Key | Action |
|---|---|
| `j` / `k` | move file cursor |
| `J` / `K` | scroll the right-pane diff body |
| `l` / `Enter` | drill into the full-file view |
| `[` / `]` | previous / next hunk |
| `a` | filter: all |
| `M` | filter: modified only |
| `N` | filter: new only |
| `S` | filter: staged only |
| `/` | filename fuzzy search |
| `s` | stage / unstage the whole file |
| `H` | stage just the current hunk |
| `c` | commit staged changes (inline modal) |
| `C` | commit amend (pre-fills with HEAD message) |
| `y` | yank current file's diff to clipboard |
| `Y` | yank every filtered file's diff |
| `R` | refresh from disk |
| `q` / `Esc` | close Diff View |

### Full-file mode (reached with `l` / `Enter`)

| Key | Action |
|---|---|
| `J` / `K` | scroll |
| `y` | yank |
| `s` | stage |
| `h` / `q` / `Esc` | back to browse |

## Conductor Panel (`c`)

| Key | Action |
|---|---|
| `j` / `k` | move cursor (conductor headers + tasks) |
| `Enter` / `l` | jump to the task's worker session |
| `p` | pause / resume the selected conductor |
| `r` | retry a failed task (flips it back to pending) |
| `M` | send a message to the task's worker |
| `+` | open the "new conductor" plan file input |
| `Y` | replay the selected conductor from its stored plan path |
| `R` | refresh |
| `q` / `Esc` | close |

## Artifact picker (`o`)

Lists the rich content detected for the focused session — Mermaid
blocks in the agent's replies and image / HTML files it wrote — and
opens the selected item in a cmux browser pane (or the OS browser
outside cmux). See [artifact-viewer.md](artifact-viewer.md).

| Key | Action |
|---|---|
| `j` / `k` | move cursor |
| `Enter` | open the selected artifact in the viewer |
| `j` / `k` (moving) | render the selected artifact inline in the right column when the terminal supports Kitty graphics |
| `+` | open a path or URL by hand (the only route for Codex / Gemini sessions) |
| `R` | refresh the list |
| `q` / `Esc` | close |

Inside the `+` input:

| Key | Action |
|---|---|
| `Tab` | complete the path against the filesystem |
| `Ctrl+U` | clear the input |
| `Enter` | validate, remember as a manual artifact, and open |
| `Esc` | cancel the input (the picker stays open) |

## MCP / Skill Manager (`m`, `s`)

| Key | Action |
|---|---|
| `j` / `k` | move cursor |
| `Space` / `Enter` | toggle attach / detach |
| `q` / `Esc` | close |

## Settings (`,`)

| Key | Action |
|---|---|
| `j` / `k` | move cursor between editable fields |
| `Enter` / `Space` / `l` / `→` | cycle enum value or toggle bool forward |
| `h` / `←` | cycle enum value backward |
| `Ctrl+S` | save to `~/.config/mosaic/config.toml` |
| `q` / `Esc` | close |

## Error log (`E`)

| Key | Action |
|---|---|
| `q` / `Esc` / `E` | close |

## Profile picker (`P`)

| Key | Action |
|---|---|
| `j` / `k` | move cursor |
| `Enter` | commit the switch (quits mosaic with a restart hint) |
| `q` / `Esc` | cancel |

## Confirmation modals

Any action that could lose work (kill, batch kill, delete) pops a
centred modal:

| Key | Action |
|---|---|
| `y` | confirm |
| `n` / `Esc` | cancel |
| `Ctrl+C` | quit mosaic entirely |
