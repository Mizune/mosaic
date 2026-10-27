package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/artifact"
	"github.com/Mizune/mosaic/internal/store"
)

// artifactPanelState is the payload of ScreenArtifacts: the picker that
// lists one session's artifacts newest first.
type artifactPanelState struct {
	sessionID   string
	sessionName string
	items       []store.Artifact
	cursor      int
	input       artifactInput
	loadErr     string
	inline      artifactInlineState
}

// artifactInput is the hand-rolled text field behind `+`: open a path
// or URL that was not detected automatically (the only route for
// Codex / Gemini sessions, whose transcripts mosaic does not parse).
type artifactInput struct {
	active     bool
	value      string
	candidates []string
	err        string
}

const emptyArtifactsMessage = "No artifacts yet.\n" +
	"Mermaid blocks and Write/Edit of .png/.jpg/.svg/.html in Claude Code\n" +
	"transcripts are picked up automatically; press + to open a path or URL by hand."

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
	m.artifactPanel = artifactPanelState{
		sessionID:   row.Session.ID,
		sessionName: row.Session.Name,
	}
	m.reloadArtifactPanel()
	m.initInlineState()
	m.screen = ScreenArtifacts
	return m.scheduleInlinePreview()
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

// reloadArtifactPanel re-reads the session's artifacts from the store
// (not the refresh cache, so a just-added manual row shows at once)
// and clamps the cursor into range.
func (m *Model) reloadArtifactPanel() {
	p := &m.artifactPanel
	p.loadErr = ""
	if m.store == nil {
		p.items = m.artifacts[p.sessionID]
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rows, err := m.store.Artifacts().ListBySession(ctx, p.sessionID)
		if err != nil {
			p.loadErr = err.Error()
			return
		}
		p.items = rows
	}
	if p.cursor >= len(p.items) {
		p.cursor = len(p.items) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (m *Model) closeArtifactPanel() tea.Cmd {
	cmd := m.releaseInlinePreview()
	m.artifactPanel = artifactPanelState{}
	m.screen = ScreenTop
	return cmd
}

// updateArtifactPanel owns every key while the picker is open.
func (m *Model) updateArtifactPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.artifactPanel
	if p.input.active {
		return m.updateArtifactInput(msg)
	}
	switch msg.String() {
	case "q", "esc":
		return m, m.closeArtifactPanel()
	case "j", "down":
		if p.cursor < len(p.items)-1 {
			p.cursor++
		}
		return m, m.scheduleInlinePreview()
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
		return m, m.scheduleInlinePreview()
	case "R":
		m.reloadArtifactPanel()
		return m, m.scheduleInlinePreview()
	case "+":
		p.input = artifactInput{active: true}
	case "enter":
		if p.cursor >= 0 && p.cursor < len(p.items) {
			return m, m.openArtifactCmd(p.items[p.cursor])
		}
	}
	return m, nil
}

// updateArtifactInput handles keystrokes inside the `+` text field.
// Tab completes against the filesystem like the wizard's project
// field; Ctrl+U clears like the Settings editor.
func (m *Model) updateArtifactInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	in := &m.artifactPanel.input
	switch msg.String() {
	case "esc":
		*in = artifactInput{}
	case "enter":
		return m, m.submitManualArtifact()
	case "backspace":
		if runes := []rune(in.value); len(runes) > 0 {
			in.value = string(runes[:len(runes)-1])
		}
		in.candidates, in.err = nil, ""
	case "ctrl+u":
		in.value, in.candidates, in.err = "", nil, ""
	case "tab":
		completed, candidates := completePath(in.value)
		in.value = completed
		in.candidates = nil
		if len(candidates) > 1 {
			in.candidates = candidates
		}
	case " ":
		in.value += " "
	default:
		if msg.Type == tea.KeyRunes && !msg.Alt {
			in.value += string(msg.Runes)
			in.candidates, in.err = nil, ""
		}
	}
	return m, nil
}

// renderArtifactPanel draws the picker inside the shared modal frame.
func (m *Model) renderArtifactPanel() string {
	p := m.artifactPanel
	title := m.theme.SectionTitle.Render(fmt.Sprintf("Artifacts: %s (%d)", p.sessionName, len(p.items)))

	var body string
	switch {
	case p.loadErr != "":
		body = m.theme.AttentionTag.Render("✗ load failed: " + p.loadErr)
	case len(p.items) == 0:
		body = m.theme.MutedText.Render(emptyArtifactsMessage)
	default:
		_, leftWidth, rightWidth := m.artifactPanelColumns()
		left := lipgloss.NewStyle().Width(leftWidth).Render(m.renderArtifactRows(p, leftWidth))
		right := m.renderArtifactInlinePane(rightWidth)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	}

	sections := []string{title, "", body}
	if p.input.active {
		sections = append(sections, "", m.renderArtifactInput())
	}
	footer := m.theme.MutedText.Render(
		"j/k: move   Enter: open   +: path/URL   R: refresh   q/Esc: close")
	sections = append(sections, "", footer)
	return m.renderModalBox(strings.Join(sections, "\n"))
}

// renderArtifactRows lists the items with a scroll window around the
// cursor: glyph + title on the left, source / age on the right, fitted
// into inner cells.
func (m *Model) renderArtifactRows(p artifactPanelState, inner int) string {
	if inner < 30 {
		inner = 30
	}
	maxRows := m.height - 12
	if maxRows < 5 {
		maxRows = 5
	}
	start, end := scrollWindow(p.cursor, len(p.items), maxRows)
	lines := make([]string, 0, end-start+2)
	if start > 0 {
		lines = append(lines, m.theme.MutedText.Render(fmt.Sprintf("  … %d above", start)))
	}
	for i := start; i < end; i++ {
		a := p.items[i]
		indicator := "  "
		if i == p.cursor {
			indicator = "▶ "
		}
		meta := artifactMeta(a)
		title := a.Title
		if title == "" {
			title = a.Path
		}
		avail := inner - 4 - visibleLen(meta) - 2
		if avail < 8 {
			avail = 8
		}
		title = clipRunes(title, avail)
		pad := inner - 4 - visibleLen(title) - visibleLen(meta)
		if pad < 1 {
			pad = 1
		}
		plain := indicator + artifactGlyph(a.Kind) + " " + title + strings.Repeat(" ", pad)
		if i == p.cursor {
			lines = append(lines, m.theme.AttentionTag.Render(plain+meta))
			continue
		}
		lines = append(lines, plain+m.theme.MutedText.Render(meta))
	}
	if end < len(p.items) {
		lines = append(lines, m.theme.MutedText.Render(fmt.Sprintf("  … %d more", len(p.items)-end)))
	}
	return strings.Join(lines, "\n")
}

// renderArtifactInput draws the `+` field under the list, mirroring
// the Conductor panel's plan-path modal.
func (m *Model) renderArtifactInput() string {
	in := m.artifactPanel.input
	title := m.theme.SectionTitle.Render("Open path or URL")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Accent).
		Padding(0, 1).
		Width(m.width / 2).
		Render("> " + in.value + "_")
	lines := []string{
		title,
		m.theme.MutedText.Render("Enter: open · Tab: complete path · Ctrl+U: clear · Esc: cancel"),
		box,
	}
	if len(in.candidates) > 0 {
		shown := in.candidates
		if len(shown) > 8 {
			shown = append(shown[:8:8], fmt.Sprintf("… %d more", len(in.candidates)-8))
		}
		lines = append(lines, m.theme.MutedText.Render(strings.Join(shown, "  ")))
	}
	if in.err != "" {
		lines = append(lines, m.theme.AttentionTag.Render("✗ "+in.err))
	}
	return strings.Join(lines, "\n")
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

// artifactMeta is the right-hand column of a picker row.
func artifactMeta(a store.Artifact) string {
	age := humanAge(a.CreatedAt)
	switch {
	case a.Source == string(artifact.SourceManual):
		return "manual · " + age
	case a.AgentID != "":
		return "sub-agent · " + age
	default:
		return age
	}
}

// artifactGlyph is the one-cell kind marker used in lists.
func artifactGlyph(kind string) string {
	switch artifact.Kind(kind) {
	case artifact.KindMermaid:
		return "◇"
	case artifact.KindImage:
		return "▣"
	case artifact.KindHTML:
		return "▤"
	case artifact.KindURL:
		return "↗"
	default:
		return "•"
	}
}

// scrollWindow returns the [start, end) slice of total rows that keeps
// cursor visible within max rows.
func scrollWindow(cursor, total, max int) (int, int) {
	if total <= max {
		return 0, total
	}
	start := cursor - max/2
	if start < 0 {
		start = 0
	}
	if start+max > total {
		start = total - max
	}
	return start, start + max
}

// clipRunes shortens s to at most n display runes, ending in an
// ellipsis when something was cut. Unlike truncate it never splits a
// multi-byte character.
func clipRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}
