package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/hook"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

// firstRunStep identifies which sub-page of the onboarding
// wizard is currently in focus.
type firstRunStep int

const (
	firstRunStepWelcome firstRunStep = iota
	firstRunStepTheme
	firstRunStepHooks
	firstRunStepDone
)

// firstRunState backs the onboarding wizard shown on the very
// first mosaic launch. It is intentionally minimal: we only need
// to capture the two answers the plan highlights (theme
// preference, hook install yes/no) and then write the marker
// file so subsequent launches skip it.
type firstRunState struct {
	active bool

	step firstRunStep

	// Theme step.
	themes       []*theme.Theme
	themeCursor  int
	themeApplied string // name that the wizard committed

	// Hooks step.
	hookCursor int // 0 = yes, 1 = no
	hookDone   bool

	// Where the wizard writes its "done" marker.
	markerPath string

	// Where the hook installer targets (typically
	// ~/.claude/settings.json). Optional override for tests.
	settingsPath string

	// Error text from the most recent side effect (hook install,
	// marker write). Cleared on step change.
	err string
}

// ShowFirstRunWizard initialises the wizard state and flips the
// screen to ScreenFirstRun. cmd_root calls this during TUI
// startup when the first-run marker file does not exist yet.
func (m *Model) ShowFirstRunWizard(markerPath, settingsPath string) {
	m.firstRun = firstRunState{
		active:       true,
		step:         firstRunStepWelcome,
		themes:       theme.Available(),
		markerPath:   markerPath,
		settingsPath: settingsPath,
	}
	// Highlight the user's currently configured theme so the
	// cursor starts on the obvious default.
	for i, th := range m.firstRun.themes {
		if th.Name == m.config.UI.Theme {
			m.firstRun.themeCursor = i
			break
		}
	}
	m.screen = ScreenFirstRun
}

// previewFirstRunTheme swaps [Model.theme] to whichever theme
// the wizard cursor currently highlights. The swap is reversible
// — moving the cursor back flips the theme back — so the user
// can scan through every option and see the colours live before
// pressing Enter to commit.
func (m *Model) previewFirstRunTheme() {
	f := &m.firstRun
	if f.themeCursor < 0 || f.themeCursor >= len(f.themes) {
		return
	}
	m.theme = f.themes[f.themeCursor]
}

// updateFirstRunWizard handles keystrokes while the wizard is
// active. The wizard uses a stepper model: Enter advances, Esc
// cancels the wizard (persists the marker as "skipped"), Shift
// +Tab goes back one step.
func (m *Model) updateFirstRunWizard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.firstRun

	switch msg.String() {
	case "esc":
		// Skip the wizard entirely. Still write the marker so
		// the user does not get prompted again — they can
		// rerun with `mosaic doctor --reset-wizard`.
		_ = writeFirstRunMarker(f.markerPath)
		m.finishFirstRun()
		return m, nil

	case "shift+tab":
		if f.step > firstRunStepWelcome {
			f.step--
			f.err = ""
		}
		return m, nil

	case "enter", "tab":
		return m, m.advanceFirstRunStep()
	}

	// Per-step key handling — mostly j/k on list-shaped steps.
	switch f.step {
	case firstRunStepTheme:
		switch msg.String() {
		case "j", "down":
			if f.themeCursor < len(f.themes)-1 {
				f.themeCursor++
				m.previewFirstRunTheme()
			}
		case "k", "up":
			if f.themeCursor > 0 {
				f.themeCursor--
				m.previewFirstRunTheme()
			}
		}
	case firstRunStepHooks:
		switch msg.String() {
		case "j", "down", "k", "up":
			f.hookCursor = 1 - f.hookCursor
		}
	}
	return m, nil
}

// advanceFirstRunStep commits the current step (applying side
// effects like hook install) and moves to the next page. If
// advance fails, the error is stashed on the state and the
// wizard stays on the current step so the user can retry.
func (m *Model) advanceFirstRunStep() tea.Cmd {
	f := &m.firstRun
	f.err = ""

	switch f.step {
	case firstRunStepWelcome:
		f.step = firstRunStepTheme
		// Apply the initial preview so the user sees a theme
		// immediately — otherwise the first paint of the
		// theme step renders in the pre-wizard colours and
		// j/k moves feel laggy.
		m.previewFirstRunTheme()
		return nil

	case firstRunStepTheme:
		if f.themeCursor >= 0 && f.themeCursor < len(f.themes) {
			th := f.themes[f.themeCursor]
			m.theme = th
			f.themeApplied = th.Name
		}
		f.step = firstRunStepHooks
		return nil

	case firstRunStepHooks:
		if f.hookCursor == 0 {
			path := f.settingsPath
			if path == "" {
				if home, _ := os.UserHomeDir(); home != "" {
					path = filepath.Join(home, ".claude", "settings.json")
				}
			}
			if path != "" {
				inst := hook.NewInstaller(path)
				if err := inst.Install(); err != nil {
					f.err = "hook install failed: " + err.Error()
					return nil
				}
				f.hookDone = true
			}
		}
		f.step = firstRunStepDone
		return m.refreshCmd()

	case firstRunStepDone:
		// Persist the marker so the wizard never re-runs, and
		// drop back to the TOP screen.
		if err := writeFirstRunMarker(f.markerPath); err != nil {
			f.err = "write marker failed: " + err.Error()
			return nil
		}
		m.finishFirstRun()
		return m.refreshCmd()
	}
	return nil
}

// finishFirstRun releases wizard state and drops the user back
// on the TOP screen. Shared by the "Esc to skip" and "finished
// all steps" paths.
func (m *Model) finishFirstRun() {
	m.firstRun = firstRunState{}
	m.screen = ScreenTop
}

// renderFirstRunWizard draws the wizard page for the current
// step. The box is always centred so the user's eyes know
// where to look regardless of terminal size.
func (m *Model) renderFirstRunWizard() string {
	f := m.firstRun
	var body string
	switch f.step {
	case firstRunStepWelcome:
		body = m.renderFirstRunWelcome()
	case firstRunStepTheme:
		body = m.renderFirstRunTheme()
	case firstRunStepHooks:
		body = m.renderFirstRunHooks()
	case firstRunStepDone:
		body = m.renderFirstRunDone()
	}

	if f.err != "" {
		body += "\n\n" + m.theme.AttentionTag.Render("✗ "+f.err)
	}

	footer := m.theme.MutedText.Render(
		"Enter: next  •  Shift+Tab: back  •  Esc: skip")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Accent).
		Padding(1, 2).
		Width(m.width - 12).
		Render(body + "\n\n" + footer)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) renderFirstRunWelcome() string {
	lines := []string{
		m.theme.SectionTitle.Render("Welcome to mosaic"),
		"",
		"Let's set up your workspace in 2 quick steps:",
		"",
		"  1. Pick a colour theme",
		"  2. (Recommended) Install Claude Code hooks",
		"",
		m.theme.MutedText.Render("Enter to start, Esc to skip and use the defaults."),
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderFirstRunTheme() string {
	f := m.firstRun
	t := m.theme

	// swatch renders a 4-cell background block in the given
	// colour so users can see the raw palette side-by-side.
	// Foreground colours need Render on styled text to take
	// effect; background colours are the reliable "big paint
	// splash" that make two themes look unmistakably
	// different even on terminals with narrow colour profiles.
	swatch := func(c lipgloss.Color, label string) string {
		sw := lipgloss.NewStyle().Background(c).Render("    ")
		return sw + " " + t.MutedText.Render(label)
	}

	lines := []string{
		t.SectionTitle.Render("Step 1/2: Theme  —  " + t.Name),
		"",
	}
	for i, th := range f.themes {
		marker := "  "
		suffix := ""
		switch th.Name {
		case "catppuccin-latte":
			suffix = "  " + t.MutedText.Render("(light — pair with a light terminal)")
		case "system":
			suffix = "  " + t.MutedText.Render("(inherits terminal colours)")
		}
		label := lipgloss.NewStyle().Foreground(t.Text).Render(th.Name)
		if i == f.themeCursor {
			marker = t.AttentionTag.Render("▶ ")
			label = t.SectionTitle.Render(th.Name + "  (selected)")
		}
		lines = append(lines, marker+label+suffix)
	}

	lines = append(lines, "",
		t.SectionTitle.Render("Palette"),
		"  "+strings.Join([]string{
			swatch(t.Attention, "attention"),
			swatch(t.Running, "running"),
			swatch(t.Idle, "idle"),
			swatch(t.ErrorCol, "error"),
		}, "  "),
		"  "+strings.Join([]string{
			swatch(t.Accent, "accent"),
			swatch(t.Border, "border"),
			swatch(t.Highlight, "highlight"),
			swatch(t.Selected, "selected"),
		}, "  "),
	)

	// Glyph preview — same strip as the TOP status bar so
	// users can see exactly how their sessions will look.
	preview := []string{
		t.AttentionTag.Render("⚠ waiting"),
		t.RunningTag.Render("● running"),
		t.IdleTag.Render("○ idle"),
		t.ErrorTag.Render("✗ error"),
	}
	lines = append(lines, "",
		t.SectionTitle.Render("Status glyphs"),
		"  "+strings.Join(preview, "   "))

	// A sample session row against the selected-row highlight
	// style so the user can see contrast against the
	// background layer they will spend most time looking at.
	sampleBody := t.SelectedRow.Render("  ● feat-auth              s3 12s  ")
	lines = append(lines, "",
		t.SectionTitle.Render("Selected row"),
		"  "+sampleBody)

	lines = append(lines, "",
		t.MutedText.Render("j/k: preview   Enter: confirm   Shift+Tab: back"))
	return strings.Join(lines, "\n")
}

func (m *Model) renderFirstRunHooks() string {
	f := m.firstRun
	options := []string{
		"Yes, install hooks now (recommended)",
		"No, I'll install them later with `mosaic hook install`",
	}
	lines := []string{
		m.theme.SectionTitle.Render("Step 2/2: Claude Code hooks"),
		"",
		"Hooks give mosaic sub-second updates on:",
		"  · which agent is waiting for your input",
		"  · when a session becomes busy / idle",
		"  · notification events (permission prompts)",
		"",
		"Without hooks, mosaic still works via fsnotify tailing",
		"and OSC capture, just with ~100-200ms extra latency.",
		"",
	}
	for i, opt := range options {
		marker := "  "
		if i == f.hookCursor {
			marker = m.theme.AttentionTag.Render("▶ ")
		}
		lines = append(lines, marker+opt)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderFirstRunDone() string {
	f := m.firstRun
	lines := []string{
		m.theme.SectionTitle.Render("All set"),
		"",
		"mosaic is ready to go. Here is what the wizard applied:",
		"",
	}
	if f.themeApplied != "" {
		lines = append(lines, "  theme:    "+f.themeApplied)
	}
	if f.hookDone {
		lines = append(lines, "  hooks:    installed")
	} else {
		lines = append(lines, "  hooks:    skipped")
	}
	lines = append(lines, "",
		m.theme.MutedText.Render("Press Enter to open the TUI."))
	return strings.Join(lines, "\n")
}

// --- helpers ---------------------------------------------------------------

// writeFirstRunMarker creates the sentinel file that tells
// future mosaic launches to skip the wizard. Directory is
// created on demand so a brand-new install never fails at
// this step.
func writeFirstRunMarker(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(""), 0o644)
}

