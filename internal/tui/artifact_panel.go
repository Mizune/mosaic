package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/store"
)

// This file is the TUI host of the artifact picker: it decides which
// session the picker shows, switches ScreenArtifacts on and off, and
// turns the picker's notices into toasts and error-log entries. The
// picker itself lives in artifact_picker.go and is shared with the
// `mosaic artifacts` popup program.

// openArtifactPanel opens the picker for the session under the cursor
// and arms the inline preview of the newest artifact. Sub-agent rows
// carry their parent session, so they work too. On a group header the
// cursor first drops to the group's first session: the TUI starts with
// the cursor on a header, and a silent no-op there reads as "o does
// nothing".
func (m *Model) openArtifactPanel() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	row := m.rows[m.cursor]
	if row.IsGroup {
		if !m.focusFirstSessionInGroup() {
			m.setTopToast("no session in this group")
			return nil
		}
		row = m.rows[m.cursor]
	}
	picker, cmd := newArtifactPicker(m.pickerDeps(row.Session.ID), row.Session.ID, row.Session.Name)
	picker.setSize(m.width, m.height)
	m.artifactPanel = picker
	m.screen = ScreenArtifacts
	return cmd
}

// pickerDeps wires the picker to this Model's store, viewer, theme and
// terminal-sequence queue.
func (m *Model) pickerDeps(sessionID string) artifactPickerDeps {
	return artifactPickerDeps{
		theme:    m.theme,
		store:    m.store,
		fallback: m.artifacts[sessionID],
		viewer:   m.artifactViewer,
		queueSeq: m.queue,
	}
}

// focusFirstSessionInGroup expands the group header under the cursor
// when it is collapsed and moves the cursor to the first session row
// beneath it. It reports false when the group holds no session.
func (m *Model) focusFirstSessionInGroup() bool {
	header := m.rows[m.cursor]
	if !header.GroupExpanded {
		m.groupExpanded[header.GroupName] = true
		m.rebuildRows()
	}
	for i := m.cursor + 1; i < len(m.rows); i++ {
		r := m.rows[i]
		if r.IsGroup {
			break
		}
		if !r.IsSubagent {
			m.cursor = i
			return true
		}
	}
	return false
}

func (m *Model) closeArtifactPanel() tea.Cmd {
	cmd := m.artifactPanel.close()
	m.artifactPanel = artifactPicker{}
	m.screen = ScreenTop
	return cmd
}

// updateArtifactPanel owns every key while the picker is open.
func (m *Model) updateArtifactPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action, notice, cmd := m.artifactPanel.updateKey(msg)
	m.applyPickerNotice(notice)
	if action == pickerActionClose {
		m.artifactPanel = artifactPicker{}
		m.screen = ScreenTop
	}
	return m, cmd
}

// applyPickerNotice routes a picker notice to the TOP toast or the
// error log.
func (m *Model) applyPickerNotice(n pickerNotice) {
	switch n.level {
	case noticeInfo:
		m.setTopToast(n.title)
	case noticeError:
		m.flagError(n.title, n.body)
	}
}

// renderArtifactPanel draws the picker inside the shared modal frame.
func (m *Model) renderArtifactPanel() string {
	return renderModalFrame(m.theme, m.width, m.height, m.artifactPanel.view())
}

// renderArtifactBadge is the "◆ 3 artifacts" chip shown in the preview
// tab strip; empty when the session has none.
func (m *Model) renderArtifactBadge(n int) string {
	if n <= 0 {
		return ""
	}
	label := fmt.Sprintf("◆ %d artifacts", n)
	if n == 1 {
		label = "◆ 1 artifact"
	}
	return lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Render(label)
}

// formatArtifactLines renders the newest max artifacts for the
// structured preview, plus a "… n more" hint pointing at the picker.
func (m *Model) formatArtifactLines(arts []store.Artifact, max int) []string {
	lines := make([]string, 0, max+1)
	for i, a := range arts {
		if i >= max {
			lines = append(lines, m.theme.MutedText.Render(
				fmt.Sprintf("… %d more (press o)", len(arts)-i)))
			break
		}
		title := a.Title
		if title == "" {
			title = a.Path
		}
		lines = append(lines, fmt.Sprintf("%s %s · %s", artifactGlyph(a.Kind), clipRunes(title, 48), humanAge(a.CreatedAt)))
	}
	return lines
}
