package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestKill_PopsConfirmOnSessionRow verifies that pressing K on
// a session row opens the confirmation modal. This is the
// minimal reproduction for the "K doesn't work" report.
func TestKill_PopsConfirmOnSessionRow(t *testing.T) {
	m := setupModel(t)
	cmds := m.Init()
	msg := runCmd(t, cmds)
	result, _ := m.Update(msg)
	m = result.(*Model)
	result, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = result.(*Model)

	t.Logf("screen before cursor move: %d", m.screen)
	t.Logf("rows count: %d", len(m.rows))

	// Move cursor to a session row (not a group header).
	for i, r := range m.rows {
		if !r.IsGroup && !r.IsSubagent {
			m.cursor = i
			t.Logf("cursor set to row %d (session: %q)", i, r.Session.Name)
			break
		}
	}

	t.Logf("screen before K: %d", m.screen)

	// Press K.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = result.(*Model)

	t.Logf("screen after K: %d, confirm.active: %v", m.screen, m.confirm.active)

	if !m.confirm.active {
		t.Fatalf("expected confirm modal after K; screen=%d confirm=%v rows=%d cursor=%d",
			m.screen, m.confirm.active, len(m.rows), m.cursor)
	}
}

// TestScreenStaysTOP_DuringNavigation verifies that j/k/h/l
// do not accidentally flip the screen away from TOP.
func TestScreenStaysTOP_DuringNavigation(t *testing.T) {
	m := setupModel(t)
	cmds := m.Init()
	msg := runCmd(t, cmds)
	result, _ := m.Update(msg)
	m = result.(*Model)
	result, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = result.(*Model)

	keys := []string{"j", "k", "h", "l", "j", "j"}
	for _, k := range keys {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		m = result.(*Model)
		if m.screen != ScreenTop {
			t.Fatalf("after pressing %q: screen=%d, expected ScreenTop (0)", k, m.screen)
		}
	}
}
