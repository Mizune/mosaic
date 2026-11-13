package tui

import (
	"strings"

	"github.com/charmbracelet/bubbletea"
)

// keyMap centralises every keybinding the TUI honours so the help
// overlay and the actual switch share one source of truth.
type keyMap struct {
	Up           []string
	Down         []string
	Left         []string
	Right        []string
	Back         []string
	Quit         []string
	New          []string
	Attach       []string
	Kill         []string
	Refresh      []string
	Help         []string
	Filter       []string
	MCPManager   []string
	SkillManager []string
	Settings     []string
	Conductor    []string
	Diff         []string
	Artifacts    []string
	ToggleSelect []string
	ClearSelect  []string
	SelectAll    []string
	Fork         []string
	Respawn      []string

	// Filter tabs (! @ # $).
	FilterWaiting []string
	FilterRunning []string
	FilterIdle    []string
	FilterError   []string
	FilterAll     []string

	// Attention navigation and dismissal.
	NextAttention []string
	DismissAtt    []string
	ErrorLog      []string

	// Visual mode (range select).
	VisualStart     []string
	VisualLineStart []string

	Profile []string
}

func defaultKeys() keyMap {
	return keyMap{
		Up:           []string{"up", "k"},
		Down:         []string{"down", "j"},
		Left:         []string{"left", "h"},
		Right:        []string{"right", "l"},
		Back:         []string{"esc", "h"},
		Quit:         []string{"q", "ctrl+c"},
		New:          []string{"n"},
		Attach:       []string{"enter", "a"},
		Kill:         []string{"K"},
		Refresh:      []string{"R"},
		Help:         []string{"?"},
		Filter:       []string{"/"},
		MCPManager:   []string{"m"},
		SkillManager: []string{"s"},
		Settings:     []string{","},
		Conductor:    []string{"c"},
		Diff:         []string{"d"},
		Artifacts:    []string{"o"},
		ToggleSelect: []string{" "},
		ClearSelect:  []string{"ctrl+\\"},
		SelectAll:    []string{"*"},
		Fork:         []string{"f"},
		Respawn:      []string{"r"},

		FilterWaiting: []string{"!"},
		FilterRunning: []string{"@"},
		FilterIdle:    []string{"#"},
		FilterError:   []string{"$"},
		FilterAll:     []string{"0"},

		NextAttention: []string{"N"},
		DismissAtt:    []string{"D"},
		ErrorLog:      []string{"E"},

		VisualStart:     []string{"v"},
		VisualLineStart: []string{"V"},

		Profile: []string{"P"},
	}
}

// matches reports whether msg matches any of names.
func matches(msg tea.KeyMsg, names []string) bool {
	got := msg.String()
	for _, n := range names {
		if got == n {
			return true
		}
	}
	return false
}

// applyOverrides merges a user-supplied [ui.keymap] map into the
// default key bindings. Each key in overrides is the struct field
// name (lowercased: "up", "quit", "mcp_manager") and the value is a
// comma-separated list of key chords ("up,k").
//
// Unknown field names are silently ignored so a newer mosaic
// config with bindings this build does not yet understand does not
// break the TUI. Invalid values (whitespace-only strings) fall
// through as an empty slice — we treat that as "remove this
// binding" so users can disable a default.
func (k *keyMap) applyOverrides(overrides map[string]string) {
	if len(overrides) == 0 {
		return
	}
	for key, raw := range overrides {
		chords := splitChords(raw)
		switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
		case "up":
			k.Up = chords
		case "down":
			k.Down = chords
		case "left":
			k.Left = chords
		case "right":
			k.Right = chords
		case "back":
			k.Back = chords
		case "quit":
			k.Quit = chords
		case "new":
			k.New = chords
		case "attach":
			k.Attach = chords
		case "kill":
			k.Kill = chords
		case "refresh":
			k.Refresh = chords
		case "help":
			k.Help = chords
		case "filter":
			k.Filter = chords
		case "mcp_manager":
			k.MCPManager = chords
		case "skill_manager":
			k.SkillManager = chords
		case "settings":
			k.Settings = chords
		case "conductor":
			k.Conductor = chords
		case "diff":
			k.Diff = chords
		case "artifacts":
			k.Artifacts = chords
		case "toggle_select":
			k.ToggleSelect = chords
		case "clear_select":
			k.ClearSelect = chords
		case "select_all":
			k.SelectAll = chords
		case "fork":
			k.Fork = chords
		case "respawn":
			k.Respawn = chords
		case "filter_waiting":
			k.FilterWaiting = chords
		case "filter_running":
			k.FilterRunning = chords
		case "filter_idle":
			k.FilterIdle = chords
		case "filter_error":
			k.FilterError = chords
		case "filter_all":
			k.FilterAll = chords
		case "next_attention":
			k.NextAttention = chords
		case "dismiss_attention":
			k.DismissAtt = chords
		case "error_log":
			k.ErrorLog = chords
		case "visual":
			k.VisualStart = chords
		case "visual_line":
			k.VisualLineStart = chords
		case "profile":
			k.Profile = chords
		}
	}
}

// splitChords turns "up, k" into ["up", "k"], dropping empty
// entries so trailing commas do not generate stray bindings.
func splitChords(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
