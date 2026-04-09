package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/hook"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

// settingsField enumerates every row the Settings screen lets
// the user edit. Read-only sections (Paths, Hooks) are rendered
// alongside but cannot be selected.
type settingsField int

const (
	settingDefaultTool settingsField = iota
	settingLogLevel
	settingTheme
	settingNotifyOS
	settingSound
	settingCount
)

// editableSettings is the static list of (label, kind) the
// Settings screen cycles through with j/k. Rendering and key
// handling both drive off this.
type editableSetting struct {
	field   settingsField
	label   string
	kind    string   // "enum" or "bool"
	options []string // for enum kind
}

var settingsSchema = []editableSetting{
	{settingDefaultTool, "default_tool", "enum",
		[]string{"claude", "codex", "gemini", "shell"}},
	{settingLogLevel, "log_level", "enum",
		[]string{"debug", "info", "warn", "error"}},
	{settingTheme, "theme", "enum",
		[]string{"catppuccin-mocha", "catppuccin-latte", "tokyo-night", "tokyo-night-storm", "system"}},
	{settingNotifyOS, "attention.notify_os", "bool", nil},
	{settingSound, "attention.sound", "bool", nil},
}

// settingsCursor is the row index in settingsSchema that is
// currently highlighted in the Settings screen.
func (m *Model) settingsCursor() int {
	if m.settingsCursorIdx < 0 {
		return 0
	}
	if m.settingsCursorIdx >= len(settingsSchema) {
		return len(settingsSchema) - 1
	}
	return m.settingsCursorIdx
}

// updateSettings handles keystrokes on the Settings screen.
func (m *Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.settingsUnsaved = false
		m.settingsError = ""
		m.screen = ScreenTop
		return m, nil
	case "j", "down":
		if m.settingsCursorIdx < len(settingsSchema)-1 {
			m.settingsCursorIdx++
		}
		return m, nil
	case "k", "up":
		if m.settingsCursorIdx > 0 {
			m.settingsCursorIdx--
		}
		return m, nil
	case "enter", " ", "l", "right":
		m.cycleSettingForward()
		return m, nil
	case "h", "left":
		m.cycleSettingBackward()
		return m, nil
	case "ctrl+s":
		return m, m.saveSettings()
	}
	return m, nil
}

// cycleSettingForward advances the highlighted field to its
// next value (next enum option, or toggling a bool).
func (m *Model) cycleSettingForward() {
	s := settingsSchema[m.settingsCursor()]
	switch s.kind {
	case "bool":
		switch s.field {
		case settingNotifyOS:
			m.config.UI.Attention.NotifyOS = !m.config.UI.Attention.NotifyOS
		case settingSound:
			m.config.UI.Attention.Sound = !m.config.UI.Attention.Sound
		}
	case "enum":
		current := m.currentSettingValue(s.field)
		next := nextInList(s.options, current)
		m.applySettingValue(s.field, next)
	}
	m.settingsUnsaved = true
}

// cycleSettingBackward is the mirror of cycleSettingForward for
// the left-arrow path.
func (m *Model) cycleSettingBackward() {
	s := settingsSchema[m.settingsCursor()]
	switch s.kind {
	case "bool":
		m.cycleSettingForward() // bool toggles are symmetric
	case "enum":
		current := m.currentSettingValue(s.field)
		next := prevInList(s.options, current)
		m.applySettingValue(s.field, next)
		m.settingsUnsaved = true
	}
}

// currentSettingValue returns the string value of an enum
// setting so the cycle helpers can find its place in the list.
func (m *Model) currentSettingValue(f settingsField) string {
	switch f {
	case settingDefaultTool:
		return m.config.Mosaic.DefaultTool
	case settingLogLevel:
		return m.config.Mosaic.LogLevel
	case settingTheme:
		return m.config.UI.Theme
	}
	return ""
}

// applySettingValue writes a new enum value back into the
// running Model.config and, for theme, repoints the live
// theme pointer so the change is visible immediately.
func (m *Model) applySettingValue(f settingsField, value string) {
	switch f {
	case settingDefaultTool:
		m.config.Mosaic.DefaultTool = value
	case settingLogLevel:
		m.config.Mosaic.LogLevel = value
	case settingTheme:
		m.config.UI.Theme = value
		m.theme = theme.ByName(value)
	}
}

// saveSettings writes the current config back to its global
// TOML file. BurntSushi/toml re-encodes the whole struct so
// comments in the existing file are lost — the trade-off for
// round-trip fidelity vs. simplicity lands on "simple".
func (m *Model) saveSettings() tea.Cmd {
	path := m.configPaths.Global
	if path == "" {
		m.settingsError = "no config path configured"
		return nil
	}
	return func() tea.Msg {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return settingsSaveResult{err: err.Error()}
		}
		tmp := path + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			return settingsSaveResult{err: err.Error()}
		}
		enc := toml.NewEncoder(f)
		enc.Indent = "  "
		if err := enc.Encode(m.config); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return settingsSaveResult{err: err.Error()}
		}
		if err := f.Close(); err != nil {
			return settingsSaveResult{err: err.Error()}
		}
		if err := os.Rename(tmp, path); err != nil {
			return settingsSaveResult{err: err.Error()}
		}
		return settingsSaveResult{success: true}
	}
}

// settingsSaveResult is the Cmd result type for saveSettings.
type settingsSaveResult struct {
	success bool
	err     string
}

// nextInList returns the element after current in the slice,
// wrapping around at the end. Unknown currents return the first
// element.
func nextInList(list []string, current string) string {
	for i, v := range list {
		if v == current {
			return list[(i+1)%len(list)]
		}
	}
	if len(list) > 0 {
		return list[0]
	}
	return ""
}

// prevInList returns the element before current, wrapping.
func prevInList(list []string, current string) string {
	for i, v := range list {
		if v == current {
			return list[(i-1+len(list))%len(list)]
		}
	}
	if len(list) > 0 {
		return list[len(list)-1]
	}
	return ""
}

// renderSettings draws the Settings screen. Editable fields live
// at the top with a cursor; read-only diagnostics (paths, hooks)
// sit below.
func (m *Model) renderSettings() string {
	width := m.width - 8
	if width < 60 {
		width = 60
	}

	var sections []string

	// --- Editable ----------------------------------------------------
	cursor := m.settingsCursor()
	editLines := []string{m.theme.SectionTitle.Render("General (editable)")}
	for i, s := range settingsSchema {
		marker := "  "
		if i == cursor {
			marker = m.theme.AttentionTag.Render("▶ ")
		}
		value := m.settingValueAsString(s)
		editLines = append(editLines,
			fmt.Sprintf("%s%-20s : %s", marker, s.label, value))
	}
	sections = append(sections, strings.Join(editLines, "\n"))

	// --- Hooks -------------------------------------------------------
	hookPath := ""
	if home, _ := os.UserHomeDir(); home != "" {
		hookPath = home + "/.claude/settings.json"
	}
	hookStatus := "unknown"
	if hookPath != "" {
		inst := hook.NewInstaller(hookPath)
		if installed, err := inst.IsInstalled(); err == nil {
			if installed {
				hookStatus = "✓ installed"
			} else {
				hookStatus = "✗ not installed"
			}
		} else {
			hookStatus = "error: " + err.Error()
		}
	}
	sections = append(sections, strings.Join([]string{
		m.theme.SectionTitle.Render("Hooks"),
		"  settings.json : " + ifEmpty(hookPath, "(no home directory)"),
		"  status        : " + hookStatus,
	}, "\n"))

	// --- Paths -------------------------------------------------------
	p := m.configPaths
	sections = append(sections, strings.Join([]string{
		m.theme.SectionTitle.Render("Paths"),
		"  global config : " + ifEmpty(p.Global, "(unknown)"),
		"  profile dir   : " + ifEmpty(p.Profile, "(unknown)"),
		"  state.db      : " + ifEmpty(p.Database, "(unknown)"),
		"  log file      : " + ifEmpty(p.LogFile, "(unknown)"),
	}, "\n"))

	// --- Footer ------------------------------------------------------
	hint := "j/k: move  •  Enter/␣/h/l: cycle  •  Ctrl+S: save  •  q/Esc: close"
	if m.settingsUnsaved {
		hint = m.theme.AttentionTag.Render("unsaved") + "  •  " + hint
	}
	if m.settingsError != "" {
		hint = m.theme.AttentionTag.Render("✗ "+m.settingsError) + "  •  " + hint
	}

	body := strings.Join(sections, "\n\n") + "\n\n" + m.theme.MutedText.Render(hint)

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Accent).
		Padding(1, 2).
		Width(width).
		Render(m.theme.SectionTitle.Render("Settings") + "\n\n" + body)
}

// settingValueAsString returns the human-readable current value
// for each editable setting, used by the renderer.
func (m *Model) settingValueAsString(s editableSetting) string {
	switch s.field {
	case settingDefaultTool:
		return ifEmpty(m.config.Mosaic.DefaultTool, "claude")
	case settingLogLevel:
		return ifEmpty(m.config.Mosaic.LogLevel, "info")
	case settingTheme:
		return ifEmpty(m.config.UI.Theme, "catppuccin-mocha")
	case settingNotifyOS:
		return boolStr(m.config.UI.Attention.NotifyOS)
	case settingSound:
		return boolStr(m.config.UI.Attention.Sound)
	}
	return ""
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
