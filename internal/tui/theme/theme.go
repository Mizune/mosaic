// Package theme defines mosaic's color palette and the lipgloss
// styles every TUI view consumes. The default palette is Catppuccin
// Mocha; future themes can be loaded from
// ~/.config/mosaic/themes/<name>.toml without changing call sites,
// because every view receives a *Theme rather than reaching for hard
// coded colors.
package theme

import "github.com/charmbracelet/lipgloss"

// Theme bundles every color and pre-built lipgloss style mosaic uses.
// Holding the styles on the struct makes them allocation-free at
// render time.
type Theme struct {
	Name string

	// Status colors.
	Attention lipgloss.Color
	Running   lipgloss.Color
	Idle      lipgloss.Color
	ErrorCol  lipgloss.Color
	Stopped   lipgloss.Color

	// Text colors.
	Text      lipgloss.Color
	Muted     lipgloss.Color
	Highlight lipgloss.Color
	Border    lipgloss.Color
	Selected  lipgloss.Color
	Accent    lipgloss.Color

	// Diff background colors — subtle tint for added/removed lines.
	DiffAddBg    lipgloss.Color
	DiffRemoveBg lipgloss.Color

	// Pre-built styles.
	Header         lipgloss.Style
	StatusBar      lipgloss.Style
	BorderBox      lipgloss.Style
	SelectedRow    lipgloss.Style
	AttentionRow   lipgloss.Style // dim red background for waiting sessions
	NormalRow      lipgloss.Style
	MutedText      lipgloss.Style
	AttentionTag   lipgloss.Style
	RunningTag     lipgloss.Style
	IdleTag        lipgloss.Style
	ErrorTag       lipgloss.Style
	StoppedTag     lipgloss.Style
	SectionTitle   lipgloss.Style
	DiffAddLine    lipgloss.Style // faint green background for + lines
	DiffRemoveLine lipgloss.Style // faint red background for - lines
}

// CatppuccinMocha returns mosaic's default dark theme (Catppuccin
// Mocha palette — the same colours used by the VS Code and
// Kitty defaults).
func CatppuccinMocha() *Theme {
	t := &Theme{
		Name:      "catppuccin-mocha",
		Attention: lipgloss.Color("#f38ba8"), // red
		Running:   lipgloss.Color("#a6e3a1"), // green
		Idle:      lipgloss.Color("#6c7086"), // overlay-0
		ErrorCol:  lipgloss.Color("#f38ba8"),
		Stopped:   lipgloss.Color("#9399b2"),

		Text:      lipgloss.Color("#cdd6f4"), // text
		Muted:     lipgloss.Color("#7f849c"), // overlay-1
		Highlight: lipgloss.Color("#f9e2af"), // yellow
		Border:    lipgloss.Color("#45475a"), // surface-1
		Selected:  lipgloss.Color("#313244"), // surface-0
		Accent:    lipgloss.Color("#89b4fa"), // blue

		DiffAddBg:    lipgloss.Color("#243328"), // green at ~10% over base
		DiffRemoveBg: lipgloss.Color("#3d1f2a"), // red at ~10% over base
	}
	return t.build()
}

// CatppuccinLatte is the light-mode sibling of Mocha. Same hues,
// darker text on a near-white background.
func CatppuccinLatte() *Theme {
	t := &Theme{
		Name:      "catppuccin-latte",
		Attention: lipgloss.Color("#d20f39"),
		Running:   lipgloss.Color("#40a02b"),
		Idle:      lipgloss.Color("#9ca0b0"),
		ErrorCol:  lipgloss.Color("#d20f39"),
		Stopped:   lipgloss.Color("#7c7f93"),

		Text:      lipgloss.Color("#4c4f69"),
		Muted:     lipgloss.Color("#8c8fa1"),
		Highlight: lipgloss.Color("#df8e1d"),
		Border:    lipgloss.Color("#bcc0cc"),
		Selected:  lipgloss.Color("#dce0e8"),
		Accent:    lipgloss.Color("#1e66f5"),

		DiffAddBg:    lipgloss.Color("#dceedd"), // green at ~10% over base
		DiffRemoveBg: lipgloss.Color("#f5dde1"), // red at ~10% over base
	}
	return t.build()
}

// TokyoNight is the classic dark Tokyo Night palette — deep navy
// background, bright cyan accent, purple selection.
func TokyoNight() *Theme {
	t := &Theme{
		Name:      "tokyo-night",
		Attention: lipgloss.Color("#f7768e"),
		Running:   lipgloss.Color("#9ece6a"),
		Idle:      lipgloss.Color("#565f89"),
		ErrorCol:  lipgloss.Color("#f7768e"),
		Stopped:   lipgloss.Color("#545c7e"),

		Text:      lipgloss.Color("#c0caf5"),
		Muted:     lipgloss.Color("#9aa5ce"),
		Highlight: lipgloss.Color("#e0af68"),
		Border:    lipgloss.Color("#3b4261"),
		Selected:  lipgloss.Color("#283457"),
		Accent:    lipgloss.Color("#7dcfff"),

		DiffAddBg:    lipgloss.Color("#252e22"), // green at ~10% over base
		DiffRemoveBg: lipgloss.Color("#301f28"), // red at ~10% over base
	}
	return t.build()
}

// TokyoNightStorm is the slightly bluer / dimmer "storm" variant of
// the Tokyo Night theme.
func TokyoNightStorm() *Theme {
	t := &Theme{
		Name:      "tokyo-night-storm",
		Attention: lipgloss.Color("#f7768e"),
		Running:   lipgloss.Color("#9ece6a"),
		Idle:      lipgloss.Color("#565f89"),
		ErrorCol:  lipgloss.Color("#db4b4b"),
		Stopped:   lipgloss.Color("#545c7e"),

		Text:      lipgloss.Color("#a9b1d6"),
		Muted:     lipgloss.Color("#7982a9"),
		Highlight: lipgloss.Color("#e0af68"),
		Border:    lipgloss.Color("#3b4261"),
		Selected:  lipgloss.Color("#292e42"),
		Accent:    lipgloss.Color("#7aa2f7"),

		DiffAddBg:    lipgloss.Color("#2d3530"), // green at ~10% over base
		DiffRemoveBg: lipgloss.Color("#382635"), // red at ~10% over base
	}
	return t.build()
}

// System returns a theme that relies on the terminal's own 16-colour
// palette via ANSI codes ("1" = red, "2" = green, etc.). It is the
// right default for users whose terminal already renders a custom
// scheme they are happy with.
func System() *Theme {
	t := &Theme{
		Name:      "system",
		Attention: lipgloss.Color("1"), // red
		Running:   lipgloss.Color("2"), // green
		Idle:      lipgloss.Color("8"), // bright black
		ErrorCol:  lipgloss.Color("1"),
		Stopped:   lipgloss.Color("7"), // white

		Text:      lipgloss.Color("15"),
		Muted:     lipgloss.Color("8"),
		Highlight: lipgloss.Color("3"),
		Border:    lipgloss.Color("8"),
		Selected:  lipgloss.Color("0"),
		Accent:    lipgloss.Color("4"), // blue
	}
	return t.build()
}

// Available lists every built-in theme mosaic knows about. The
// order here is the one the wizard shows in its theme picker.
func Available() []*Theme {
	return []*Theme{
		CatppuccinMocha(),
		CatppuccinLatte(),
		TokyoNight(),
		TokyoNightStorm(),
		System(),
	}
}

// ByName returns the named theme, or the default (Catppuccin Mocha)
// when name is empty or unknown. Lookup is case-insensitive and
// forgives dashes/underscores so "tokyo_night" and "tokyo-night"
// both resolve.
func ByName(name string) *Theme {
	switch normaliseThemeName(name) {
	case "catppuccin-latte":
		return CatppuccinLatte()
	case "tokyo-night":
		return TokyoNight()
	case "tokyo-night-storm":
		return TokyoNightStorm()
	case "system":
		return System()
	case "catppuccin-mocha", "":
		return CatppuccinMocha()
	default:
		return CatppuccinMocha()
	}
}

func normaliseThemeName(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
		case c == '_':
			out = append(out, '-')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

// build instantiates the lipgloss styles from the raw colors.
func (t *Theme) build() *Theme {
	t.Header = lipgloss.NewStyle().
		Foreground(t.Text).
		Bold(true).
		Padding(0, 1)

	t.StatusBar = lipgloss.NewStyle().
		Foreground(t.Muted).
		Padding(0, 1)

	t.BorderBox = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Border).
		Padding(0, 1)

	// SelectedRow gets a saturated background AND a bright
	// left edge ("│" coloured with the accent) so the focused
	// row pops out even at a glance. The dim Selected colour
	// alone was easy to miss in long lists.
	t.SelectedRow = lipgloss.NewStyle().
		Background(t.Selected).
		Foreground(t.Text).
		Bold(true).
		Border(lipgloss.Border{Left: "▎"}, false, false, false, true).
		BorderForeground(t.Accent).
		BorderBackground(t.Selected)

	// AttentionRow uses a dim red background so sessions waiting
	// for input jump out without being painful to look at. The
	// foreground stays on the main text colour so the row stays
	// legible against the tinted background.
	t.AttentionRow = lipgloss.NewStyle().
		Background(lipgloss.Color("#3d1f2a")). // very dark desaturated red
		Foreground(t.Text).
		Bold(true)

	t.NormalRow = lipgloss.NewStyle().
		Foreground(t.Text)

	t.MutedText = lipgloss.NewStyle().
		Foreground(t.Muted)

	t.AttentionTag = lipgloss.NewStyle().
		Foreground(t.Attention).
		Bold(true)

	t.RunningTag = lipgloss.NewStyle().
		Foreground(t.Running)

	t.IdleTag = lipgloss.NewStyle().
		Foreground(t.Idle)

	t.ErrorTag = lipgloss.NewStyle().
		Foreground(t.ErrorCol).
		Bold(true)

	t.StoppedTag = lipgloss.NewStyle().
		Foreground(t.Stopped)

	t.SectionTitle = lipgloss.NewStyle().
		Foreground(t.Accent).
		Bold(true)

	if t.DiffAddBg != "" {
		t.DiffAddLine = lipgloss.NewStyle().Background(t.DiffAddBg)
	} else {
		t.DiffAddLine = lipgloss.NewStyle()
	}
	if t.DiffRemoveBg != "" {
		t.DiffRemoveLine = lipgloss.NewStyle().Background(t.DiffRemoveBg)
	} else {
		t.DiffRemoveLine = lipgloss.NewStyle()
	}

	return t
}

// StatusGlyph returns the icon + label combination used in session
// rows. The icon is colored to match the status; the label is
// rendered in the muted text color so the row is scannable.
func (t *Theme) StatusGlyph(status string) string {
	switch status {
	case "running":
		return t.RunningTag.Render("●") + " " + t.MutedText.Render("running")
	case "waiting", "waiting_input":
		return t.AttentionTag.Render("⚠") + " " + t.AttentionTag.Render("wait")
	case "error":
		return t.ErrorTag.Render("✗") + " " + t.ErrorTag.Render("error")
	case "stopped":
		return t.StoppedTag.Render("■") + " " + t.MutedText.Render("stopped")
	case "spawning":
		return t.MutedText.Render("…") + " " + t.MutedText.Render("spawning")
	default:
		return t.IdleTag.Render("○") + " " + t.MutedText.Render("idle")
	}
}
