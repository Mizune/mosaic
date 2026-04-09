package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// freshModel returns a Model that has been through Init → refresh
// → WindowSize, with the cursor on the first session row. Each
// sub-test calls this independently so there is zero shared
// state between test cases.
func freshModel(t *testing.T) *Model {
	t.Helper()
	m := setupModel(t)
	cmds := m.Init()
	msg := runCmd(t, cmds)
	result, _ := m.Update(msg)
	m = result.(*Model)
	result, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = result.(*Model)
	for i, r := range m.rows {
		if !r.IsGroup && !r.IsSubagent {
			m.cursor = i
			break
		}
	}
	return m
}

func press(t *testing.T, m *Model, key string) (*Model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	case "ctrl+\\":
		msg = tea.KeyMsg{Type: tea.KeyCtrlBackslash}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	result, cmd := m.Update(msg)
	return result.(*Model), cmd
}

// TestAllKeyBindings_ReachHandler verifies a selection of
// registered key chords by pressing each one on a FRESH model
// and checking the immediate effect. Each sub-test gets its own
// Model so cascading state breaks are impossible.
func TestAllKeyBindings_ReachHandler(t *testing.T) {
	t.Run("K opens confirm", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "K")
		if !m.confirm.active {
			t.Fatalf("K on session row should pop confirm; screen=%d", m.screen)
		}
	})

	t.Run("d opens Diff View", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "d")
		if m.screen != ScreenDiff {
			t.Fatalf("d should open Diff; screen=%d", m.screen)
		}
	})

	t.Run("c opens Conductor", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "c")
		if m.screen != ScreenConductor {
			t.Fatalf("c should open Conductor; screen=%d", m.screen)
		}
	})

	t.Run("m opens MCP", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "m")
		if m.screen != ScreenMCPManager {
			t.Fatalf("m should open MCP; screen=%d", m.screen)
		}
	})

	t.Run("s opens Skill", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "s")
		if m.screen != ScreenSkillManager {
			t.Fatalf("s should open Skill; screen=%d", m.screen)
		}
	})

	t.Run(", opens Settings", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, ",")
		if m.screen != ScreenSettings {
			t.Fatalf(", should open Settings; screen=%d", m.screen)
		}
	})

	t.Run("E opens ErrorLog", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "E")
		if m.screen != ScreenErrorLog {
			t.Fatalf("E should open ErrorLog; screen=%d", m.screen)
		}
	})

	t.Run("P opens Profile", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "P")
		if m.screen != ScreenProfile {
			t.Fatalf("P should open Profile; screen=%d", m.screen)
		}
	})

	t.Run("? toggles Help", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "?")
		if m.screen != ScreenHelp {
			t.Fatalf("? should toggle Help; screen=%d", m.screen)
		}
		m, _ = press(t, m, "?")
		if m.screen != ScreenTop {
			t.Fatalf("second ? should return to Top; screen=%d", m.screen)
		}
	})

	t.Run("n opens wizard", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "n")
		if m.screen != ScreenNewSession {
			t.Fatalf("n should open wizard; screen=%d", m.screen)
		}
	})

	t.Run("Space marks a row", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, " ")
		if len(m.selected) == 0 {
			t.Fatal("Space should mark the current session")
		}
	})

	t.Run("! sets filter waiting", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "!")
		if m.statusFilter != statusFilterWaiting {
			t.Fatalf("! should set filter waiting; got %d", m.statusFilter)
		}
	})

	t.Run("v starts visual", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "v")
		if m.visualAnchor < 0 {
			t.Fatal("v should start visual mode")
		}
	})

	t.Run("Tab cycles preview", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "tab")
		if m.previewMode != previewLiveCapture {
			t.Fatalf("Tab should cycle preview; got %d", m.previewMode)
		}
	})

	t.Run("f triggers fork", func(t *testing.T) {
		m := freshModel(t)
		_, cmd := press(t, m, "f")
		if cmd == nil {
			t.Fatal("f should return a non-nil Cmd (fork)")
		}
	})

	t.Run("N jump (no attention, toast)", func(t *testing.T) {
		m := freshModel(t)
		m, _ = press(t, m, "N")
		if m.topToast == "" {
			t.Fatal("N with no attention should set a toast")
		}
	})

	t.Run("D dismiss returns Cmd", func(t *testing.T) {
		m := freshModel(t)
		_, cmd := press(t, m, "D")
		if cmd == nil {
			t.Fatal("D should return a non-nil Cmd")
		}
	})

	t.Run("R returns refresh Cmd", func(t *testing.T) {
		m := freshModel(t)
		_, cmd := press(t, m, "R")
		if cmd == nil {
			t.Fatal("R should return a non-nil Cmd")
		}
	})
}
