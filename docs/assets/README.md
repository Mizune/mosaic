# docs/assets

Assets referenced by the README and the rest of the
documentation — mostly GIFs rendered from `.tape` scripts via
[Charmbracelet's `vhs`](https://github.com/charmbracelet/vhs).

## Regenerating the GIFs

Install vhs:

```bash
brew install vhs
# or:
go install github.com/charmbracelet/vhs@latest
```

Then from the repo root:

```bash
# build the binary that vhs will drive
make build

# render every .tape into its matching .gif
for tape in docs/assets/*.tape; do
  vhs "$tape"
done
```

Each tape drops its GIF next to the script (e.g.
`docs/assets/tour.tape` → `docs/assets/tour.gif`).

## Tapes in this directory

| Tape | Output | What it shows |
|---|---|---|
| `tour.tape` | `tour.gif` | 45-second tour: doctor → new session → attach → detach → diff view → conductor panel. |
| `attention.tape` | `attention.gif` | The killer feature — 3 running sessions, one suddenly flips to `⚠ waiting`, user presses `N` to jump and `D` to dismiss. |
| `theme-switch.tape` | `theme-switch.gif` | Cycling through all 5 built-in themes from the Settings screen. |
| `diff-view.tape` | `diff-view.gif` | Opening the Diff View, filtering, drill-in, staging a hunk, committing. |

## Writing a new tape

vhs is declarative — a tape is a sequence of commands. See
[the vhs docs](https://github.com/charmbracelet/vhs#readme) for
the full grammar. A minimal mosaic tape looks like:

```tape
Output demo.gif

Set Theme "Catppuccin Mocha"
Set FontSize 14
Set Width 1200
Set Height 700

Type "mosaic doctor"
Enter
Sleep 1s

Type "mosaic"
Enter
Sleep 2s

# press n to open the wizard
Type "n"
Sleep 500ms
Type "demo-session"
Enter
Sleep 3s

# quit
Type "q"
```

Always do the recording against a clean `--home` so the GIF is
reproducible:

```bash
MOSAIC_HOME=/tmp/mosaic-demo vhs docs/assets/tour.tape
```
