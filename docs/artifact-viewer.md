# Artifact viewer

mosaic is a text UI, but agents keep producing things that only make
sense rendered: Mermaid diagrams in their replies, screenshots they
take, HTML reports they write. The artifact viewer collects those per
session and opens them at full fidelity next to the terminal.

## What gets detected

mosaic already tails every Claude Code transcript for sub-agent and
attention state. The same stream now feeds `internal/artifact`:

| Artifact | Detected from | Opens as |
|---|---|---|
| Mermaid diagram | a ```` ```mermaid ```` fenced block in an assistant message | a generated HTML page that renders the diagram with mermaid.js |
| Image (`.png .jpg .jpeg .gif .webp .svg`) | the `file_path` of a `Write` / `Edit` / `MultiEdit` tool call | the file itself |
| HTML (`.html .htm`) | same | the file itself |
| URL / path you typed | the `+` input in the picker | the URL, or the file by the rules above (`.mmd` / `.mermaid` files render as diagrams) |

Each artifact is keyed by a content hash (the Mermaid source, or the
absolute path), so a transcript replayed on the next mosaic start is
recorded once, and repeated edits of one file collapse into one row.
Rows live in the `artifacts` table and go away with the session.

Codex and Gemini transcripts are not parsed, so nothing is detected
automatically for those sessions; the `+` input works everywhere.

## Using it

- The preview tab strip shows `◆ 3 artifacts · o` when the focused
  session has any; the structured preview lists the newest three.
- `o` opens the picker. `j` / `k` move, `Enter` opens, `+` opens a
  path or URL by hand (`Tab` completes paths), `R` refreshes,
  `q` / `Esc` closes. On a project group header, `o` drops to the
  group's first session and opens its picker.
- A toast confirms `opened graph TD (cmux)`; failures land in the
  error log (`E`) and the picker stays open.
- With `[viewer] auto_open = true`, each newly detected artifact opens
  as soon as it appears. The backlog replayed at startup never does.

## Inline preview (Ghostty, kitty, WezTerm)

When the terminal implements the Kitty graphics protocol, the picker
draws the selected artifact in its right-hand column, inside the TUI.
mosaic transmits the image with Unicode placeholders, so it survives
Bubble Tea redraws and works through tmux once passthrough is on:

```bash
tmux set -g allow-passthrough on   # add to ~/.tmux.conf to keep it
```

Without it the column explains what to enable. PNG files are shown as
they are; JPEG and GIF are converted; Mermaid pages, HTML, SVG, WebP and
URLs are rasterized with a headless Chrome / Chromium (`chrome_path`
overrides the auto-detected binary) and cached next to the generated
pages. The first preview of a diagram takes about two seconds; later
ones are instant. `Enter` still opens the full-fidelity version in a
browser pane or browser. Set `[viewer] inline = "off"` to keep the
picker text-only.

## While attached

Detection keeps running while you are inside a session, but the TUI's
badge is out of sight. Two things bridge that gap:

- **`prefix+O`** opens the same picker for the current session in a
  tmux popup (`mosaic artifacts`), so you can look at a diagram without
  detaching. `[viewer] tmux_viewer = "split"` opens a right-hand split
  instead. The popup polls the store every two seconds, so artifacts
  that land while it is open appear without losing your selection.
- **A status-line notice** (`◆ graph TD — prefix+O to view`) shows for
  five seconds in the session when a new artifact is detected. Turn it
  off with `[viewer] notify_tmux = false`.

Inline preview works there too, through both tmux servers (your own
tmux, if you run mosaic in one, and mosaic's): mosaic's server already
allows passthrough; your own needs `allow-passthrough on` as above.
The session is resolved from the pane you are in, so `mosaic artifacts`
needs no arguments inside a session and takes a session name elsewhere.
Like `mosaic diff`, the popup reads the global config only, not a
project's `.mosaic/config.toml`.

## Where it opens

Inside **cmux** (`CMUX_SOCKET_PATH` set, `cmux` CLI on `PATH`) mosaic
opens a browser pane in the Dock:

```bash
cmux new-pane --type browser --placement dock --url file:///… --focus false
```

and reuses that pane for the next artifact with `cmux browser --surface
<ref> goto <url>`. If the pane was closed by hand, a new one is created.
`placement = "right" | "down"` splits the current workspace instead.

Everywhere else the URL goes to the default browser via `open` (macOS)
or `xdg-open` (Linux).

## Mermaid pages

Generated pages are written to `~/Library/Caches/mosaic/artifacts/<session>/<hash>.html`
(`$XDG_CACHE_HOME/mosaic/artifacts/` on Linux), mode `0600`, and
removed when the session is killed. The page embeds the diagram source,
loads `mermaid.min.js` from jsdelivr by default and keeps the raw source
in a collapsible block so something useful shows even when the CDN is
unreachable. For offline use set `mermaid_js_url` to a local
`file://…/mermaid.min.js` (the UMD build; browsers refuse ES-module
imports from `file://` pages).

## Configuration

See [configuration.md](configuration.md#viewer--artifact-viewer) for the
`[viewer]` table and `artifacts` in `[ui.keymap]` to rebind `o`.
