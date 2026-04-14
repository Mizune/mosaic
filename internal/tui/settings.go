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
	// Discord section.
	settingDiscordEnabled
	settingDiscordChannelID
	settingDiscordGuildID
	settingDiscordNotifyStop
	settingDiscordNotifyAttention
	settingCount
)

// editableSettings is the static list of (label, kind) the
// Settings screen cycles through with j/k. Rendering and key
// handling both drive off this.
type editableSetting struct {
	field   settingsField
	label   string
	kind    string   // "enum", "bool", or "text"
	options []string // for enum kind
	section string   // section header to render before this field
}

var settingsSchema = []editableSetting{
	{settingDefaultTool, "default_tool", "enum",
		[]string{"claude", "codex", "gemini", "shell"}, "General"},
	{settingLogLevel, "log_level", "enum",
		[]string{"debug", "info", "warn", "error"}, ""},
	{settingTheme, "theme", "enum",
		[]string{"catppuccin-mocha", "catppuccin-latte", "tokyo-night", "tokyo-night-storm", "system"}, ""},
	{settingNotifyOS, "attention.notify_os", "bool", nil, ""},
	{settingSound, "attention.sound", "bool", nil, ""},
	// Discord section.
	{settingDiscordEnabled, "discord.enabled", "bool", nil, "Discord"},
	{settingDiscordChannelID, "discord.channel_id", "text", nil, ""},
	{settingDiscordGuildID, "discord.guild_id", "text", nil, ""},
	{settingDiscordNotifyStop, "discord.notify_on_stop", "bool", nil, ""},
	{settingDiscordNotifyAttention, "discord.notify_on_attention", "bool", nil, ""},
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
	// Text editing mode: capture all keys for the text buffer.
	if m.settingsTextEditing {
		return m.updateSettingsTextInput(msg)
	}

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
		s := settingsSchema[m.settingsCursor()]
		if s.kind == "text" {
			m.settingsTextEditing = true
			m.settingsTextBuf = m.currentSettingValue(s.field)
			return m, nil
		}
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

// updateSettingsTextInput handles keystrokes while editing a text
// field in settings.
func (m *Model) updateSettingsTextInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Commit the text value.
		s := settingsSchema[m.settingsCursor()]
		m.applySettingValue(s.field, m.settingsTextBuf)
		m.settingsTextEditing = false
		m.settingsUnsaved = true
		return m, nil
	case "esc":
		// Cancel editing.
		m.settingsTextEditing = false
		return m, nil
	case "backspace":
		if runes := []rune(m.settingsTextBuf); len(runes) > 0 {
			m.settingsTextBuf = string(runes[:len(runes)-1])
		}
		return m, nil
	case "ctrl+u":
		m.settingsTextBuf = ""
		return m, nil
	default:
		// Append printable characters.
		r := msg.String()
		if len(r) == 1 && r[0] >= 32 && r[0] < 127 {
			m.settingsTextBuf += r
		}
		return m, nil
	}
}

// cycleSettingForward advances the highlighted field to its
// next value (next enum option, or toggling a bool).
func (m *Model) cycleSettingForward() {
	s := settingsSchema[m.settingsCursor()]
	switch s.kind {
	case "bool":
		m.toggleBoolSetting(s.field)
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

// toggleBoolSetting flips a bool config value.
func (m *Model) toggleBoolSetting(f settingsField) {
	switch f {
	case settingNotifyOS:
		m.config.UI.Attention.NotifyOS = !m.config.UI.Attention.NotifyOS
	case settingSound:
		m.config.UI.Attention.Sound = !m.config.UI.Attention.Sound
	case settingDiscordEnabled:
		m.config.Discord.Enabled = !m.config.Discord.Enabled
	case settingDiscordNotifyStop:
		m.config.Discord.NotifyOnStop = !m.config.Discord.NotifyOnStop
	case settingDiscordNotifyAttention:
		m.config.Discord.NotifyOnAttention = !m.config.Discord.NotifyOnAttention
	}
}

// currentSettingValue returns the string value of a setting so
// the cycle/text helpers can read it.
func (m *Model) currentSettingValue(f settingsField) string {
	switch f {
	case settingDefaultTool:
		return m.config.Mosaic.DefaultTool
	case settingLogLevel:
		return m.config.Mosaic.LogLevel
	case settingTheme:
		return m.config.UI.Theme
	case settingDiscordChannelID:
		return m.config.Discord.ChannelID
	case settingDiscordGuildID:
		return m.config.Discord.GuildID
	}
	return ""
}

// applySettingValue writes a value back into the running config.
func (m *Model) applySettingValue(f settingsField, value string) {
	switch f {
	case settingDefaultTool:
		m.config.Mosaic.DefaultTool = value
	case settingLogLevel:
		m.config.Mosaic.LogLevel = value
	case settingTheme:
		m.config.UI.Theme = value
		m.theme = theme.ByName(value)
	case settingDiscordChannelID:
		m.config.Discord.ChannelID = value
	case settingDiscordGuildID:
		m.config.Discord.GuildID = value
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

	// --- Editable fields (grouped by section) ---
	cursor := m.settingsCursor()
	var editLines []string
	lastSection := ""
	for i, s := range settingsSchema {
		// Render section header when it changes.
		if s.section != "" && s.section != lastSection {
			if len(editLines) > 0 {
				editLines = append(editLines, "")
			}
			editLines = append(editLines, m.theme.SectionTitle.Render(s.section))
			lastSection = s.section
		}

		marker := "  "
		if i == cursor {
			marker = m.theme.AttentionTag.Render("> ")
		}

		value := m.settingValueAsString(s)
		// Show text editing state.
		if i == cursor && m.settingsTextEditing {
			value = m.settingsTextBuf + m.theme.AttentionTag.Render("|")
		}

		editLines = append(editLines,
			fmt.Sprintf("%s%-28s : %s", marker, s.label, value))
	}

	// Token hint (not editable in TUI for security).
	editLines = append(editLines, "")
	editLines = append(editLines,
		m.theme.MutedText.Render("  token: set MOSAIC_DISCORD_TOKEN env var"))
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
				hookStatus = "installed"
			} else {
				hookStatus = "not installed"
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
	hint := "j/k: move  Enter: edit  h/l: cycle  Ctrl+S: save  q: close"
	if m.settingsTextEditing {
		hint = m.theme.AttentionTag.Render("EDITING") + "  Enter: confirm  Esc: cancel  Ctrl+U: clear"
	} else if m.settingsUnsaved {
		hint = m.theme.AttentionTag.Render("unsaved") + "  " + hint
	}
	if m.settingsError != "" {
		hint = m.theme.AttentionTag.Render("! "+m.settingsError) + "  " + hint
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
	case settingDiscordEnabled:
		return boolStr(m.config.Discord.Enabled)
	case settingDiscordChannelID:
		return ifEmpty(m.config.Discord.ChannelID, "(not set)")
	case settingDiscordGuildID:
		return ifEmpty(m.config.Discord.GuildID, "(not set)")
	case settingDiscordNotifyStop:
		return boolStr(m.config.Discord.NotifyOnStop)
	case settingDiscordNotifyAttention:
		return boolStr(m.config.Discord.NotifyOnAttention)
	}
	return ""
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
