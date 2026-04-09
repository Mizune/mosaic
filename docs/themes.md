# Themes

mosaic ships with five built-in themes and an easy way to pick
between them. Colour is load-bearing in the TUI — the header,
section headings, preview tabs, footer hints, group headers,
session name bold weight, and the hunk highlighting in the Diff
View all read from the active theme — so switching is one of the
fastest ways to personalise the experience.

## Picking a theme

### From the first-run wizard

The first time you launch `mosaic`, step 1 of the onboarding
wizard lets you scroll through every built-in theme with `j`/`k`.
The entire panel re-paints in the highlighted theme as you
move, so you can compare them side-by-side before committing.
Press `Enter` to select.

You can re-run the wizard any time with:

```bash
mosaic doctor --reset-wizard
mosaic
```

### From `config.toml`

```toml
[ui]
theme = "tokyo-night"
```

mosaic re-reads the config on every launch, so the change takes
effect the next time you open the TUI.

### From the Settings screen

Press `,` to open Settings, arrow down to `theme`, and press
`Enter` / `Space` / `→` to cycle forward through the list (`←`
cycles backward). Press `Ctrl+S` to save the choice to
`~/.config/mosaic/config.toml`. The running TUI repaints
immediately — no restart required.

## Built-in themes

| Name | Kind | Best on | Distinguishing feature |
|---|---|---|---|
| `catppuccin-mocha` | dark | dark terminals | Default. Pastel blue accent, soft pink attention. |
| `catppuccin-latte` | **light** | light terminals | Same palette rotated to the light mode; looks washed out on dark terminals. |
| `tokyo-night` | dark | dark terminals | Cyan accent, navy selection. Crisper than Mocha. |
| `tokyo-night-storm` | dark | dark terminals | Bluer / dimmer variant of Tokyo Night. |
| `system` | inherits 16-color ANSI | any | No explicit RGB — uses the terminal's own colour scheme. Perfect if you already have a hand-tuned terminal palette. |

### Key colour slots

Every theme defines a small set of roles mosaic maps to actual
colours:

| Role | Used by |
|---|---|
| `Attention` | `⚠ waiting` icon, waiting row background pulse, dismiss marker, error toast, attention banner, filter chip border |
| `Running` | `● running` icon, running badge, Diff View `+` lines |
| `Idle` | `○ idle` icon, idle badge |
| `ErrorCol` | `✗ error` glyph, error row highlight |
| `Stopped` | `■ stopped` icon, stopped badge, stopped session name |
| `Text` | Default body text (preview sections, session names) |
| `Muted` | Low-priority text (help hints, timestamps, breadcrumbs) |
| `Highlight` | Footer keys, pill text in header row 1, sub-agent count badge |
| `Border` | BorderBox outlines |
| `Selected` | Selected row background, brand tile foreground |
| `Accent` | Section titles, group headers, brand, preview tab underline, Output bar |

If you're curious which field ends up where, grep for the role
name in `internal/tui/views.go` — every usage site explicitly
references `m.theme.Accent` / `m.theme.Muted` / etc.

## Writing a custom theme

Custom themes are currently built in Go — there is a planned
config path at `~/.config/mosaic/themes/<name>.toml` but the
loader hasn't been wired yet. For now, fork `internal/tui/theme/theme.go`:

1. Add a constructor next to `CatppuccinMocha` and friends:

```go
func SolarizedDark() *Theme {
    t := &Theme{
        Name:      "solarized-dark",
        Attention: lipgloss.Color("#dc322f"),
        Running:   lipgloss.Color("#859900"),
        Idle:      lipgloss.Color("#586e75"),
        ErrorCol:  lipgloss.Color("#dc322f"),
        Stopped:   lipgloss.Color("#657b83"),

        Text:      lipgloss.Color("#93a1a1"),
        Muted:     lipgloss.Color("#586e75"),
        Highlight: lipgloss.Color("#b58900"),
        Border:    lipgloss.Color("#073642"),
        Selected:  lipgloss.Color("#073642"),
        Accent:    lipgloss.Color("#268bd2"),
    }
    return t.build()
}
```

2. Register it in `Available()` so it shows up in the wizard
and Settings picker.

3. Add it to `ByName()` so `[ui] theme = "solarized-dark"`
resolves correctly. Pair it with the closest chroma style in
`chromaStyleForTheme` (`internal/tui/diff_highlight.go`) so the
Diff View syntax highlighter follows the rest of the UI.

4. `make test ./internal/tui/theme/...` — the theme tests
enumerate every registered theme and `ByName` must not return
the default for the new name.

5. Run `./bin/mosaic` and pick your theme via Settings to
verify the palette on a real TUI.

### Gotchas

- **Light themes on dark terminals look broken.** The
  `catppuccin-latte` text colour (`#4c4f69`) is a dark grey
  deliberately chosen for near-white backgrounds. Pair light
  themes with light terminals — or tweak the `Text` field in
  your own constructor.
- **True-color is assumed.** On 256-colour terminals the
  pastel Catppuccin hues collapse to their nearest indexed
  colour, making Mocha and Tokyo Night look suspiciously
  similar. `echo $COLORTERM` should say `truecolor`.
- **Chroma style names are not 1:1 with theme names.** The
  chroma registry has its own list (`styles.Get("...")`).
  `chromaStyleForTheme` walks a short list of candidates and
  falls back to `styles.Fallback` — not every mosaic theme
  has a perfect chroma sibling.

## Previewing a theme quickly

The first-run wizard is the best way to compare, but you can
also flip between themes without restarting:

1. `,` to open Settings
2. `j/k` to `theme`
3. `Enter` / `→` cycles through the list — the whole TUI
   repaints as each theme becomes active
4. `Ctrl+S` when you land on the one you like
5. `q` / `Esc` to close

Even when unsaved, the preview persists for the rest of the
session so you can keep working while deciding.
