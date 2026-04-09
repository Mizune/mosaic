package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/skill"
)

// skillPanelState is the Skill Manager sibling of
// [mcpPanelState]. Keeping them side-by-side (instead of collapsing
// into a shared checkbox modal) preserves detail-column freedom:
// MCPs care about command/args, skills care about description and
// body file path, and the two will diverge further.
type skillPanelState struct {
	sessionID   string
	sessionName string
	skills      []skill.Skill
	attached    map[string]bool
	cursor      int
	loadErr     string
}

// openSkillManager gathers state for the modal and flips the screen.
func (m *Model) openSkillManager() {
	if len(m.rows) == 0 {
		return
	}
	row := m.rows[m.cursor]
	if row.IsGroup || row.IsSubagent {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	panel := skillPanelState{
		sessionID:   row.Session.ID,
		sessionName: row.Session.Name,
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		skills, err := skill.Discover(filepath.Join(home, ".claude", "skills"))
		if err != nil {
			panel.loadErr = err.Error()
		} else {
			panel.skills = skills
		}
	}

	if m.store != nil {
		attached, err := m.store.SessionSkills().AttachedNames(ctx, panel.sessionID)
		if err == nil {
			panel.attached = attached
		}
	}
	if panel.attached == nil {
		panel.attached = map[string]bool{}
	}

	m.skillPanel = panel
	m.screen = ScreenSkillManager
}

// closeSkillManager returns to TOP and releases panel state.
func (m *Model) closeSkillManager() {
	m.skillPanel = skillPanelState{}
	m.screen = ScreenTop
}

func (m *Model) updateSkillPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.closeSkillManager()
		return m, nil

	case "j", "down":
		if m.skillPanel.cursor < len(m.skillPanel.skills)-1 {
			m.skillPanel.cursor++
		}
		return m, nil

	case "k", "up":
		if m.skillPanel.cursor > 0 {
			m.skillPanel.cursor--
		}
		return m, nil

	case " ", "enter":
		return m.skillPanelToggle()
	}
	return m, nil
}

func (m *Model) skillPanelToggle() (tea.Model, tea.Cmd) {
	p := &m.skillPanel
	if p.cursor < 0 || p.cursor >= len(p.skills) {
		return m, nil
	}
	name := p.skills[p.cursor].Name
	if m.store == nil {
		return m, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if p.attached[name] {
		if err := m.store.SessionSkills().Detach(ctx, p.sessionID, name); err != nil {
			m.flagError("detach skill failed", err.Error())
			return m, nil
		}
		delete(p.attached, name)
	} else {
		if err := m.store.SessionSkills().Attach(ctx, p.sessionID, name); err != nil {
			m.flagError("attach skill failed", err.Error())
			return m, nil
		}
		p.attached[name] = true
	}
	return m, nil
}

// renderSkillPanel draws the Skill Manager modal. Layout mirrors
// the MCP panel for muscle-memory reasons: checkbox list on the
// left, details on the right.
func (m *Model) renderSkillPanel() string {
	p := m.skillPanel

	title := m.theme.SectionTitle.Render("Skill Manager: " + p.sessionName)

	if p.loadErr != "" {
		body := m.theme.AttentionTag.Render("✗ discover failed: "+p.loadErr) + "\n\n" +
			m.theme.MutedText.Render("q / Esc to return")
		return m.renderMCPBox(title + "\n\n" + body)
	}
	if len(p.skills) == 0 {
		body := m.theme.MutedText.Render("No skills discovered in ~/.claude/skills/.") +
			"\n\n" + m.theme.MutedText.Render("q / Esc to return")
		return m.renderMCPBox(title + "\n\n" + body)
	}

	leftWidth := m.width/2 - 6
	if leftWidth < 20 {
		leftWidth = 20
	}
	lines := make([]string, 0, len(p.skills))
	for i, s := range p.skills {
		box := "☐"
		if p.attached[s.Name] {
			box = "☑"
		}
		indicator := "  "
		if i == p.cursor {
			indicator = "▶ "
		}
		name := s.Name
		if visibleLen(name) > leftWidth-6 {
			name = truncate(name, leftWidth-6)
		}
		line := indicator + box + " " + name
		if i == p.cursor {
			line = m.theme.AttentionTag.Render(line)
		}
		lines = append(lines, line)
	}
	left := strings.Join(lines, "\n")

	right := m.renderSkillDetail(p.skills[p.cursor], p.attached)

	leftBox := lipgloss.NewStyle().Width(leftWidth).Render(left)
	rightBox := lipgloss.NewStyle().Width(m.width - leftWidth - 8).Render(right)
	cols := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	footer := m.theme.MutedText.Render(
		"j/k: move   space/enter: toggle   q/Esc: close")

	return m.renderMCPBox(title + "\n\n" + cols + "\n\n" + footer)
}

func (m *Model) renderSkillDetail(s skill.Skill, attached map[string]bool) string {
	status := "○ detached"
	if attached[s.Name] {
		status = "● attached"
	}
	lines := []string{
		m.theme.SectionTitle.Render(s.Name),
		"",
		"status: " + status,
	}
	if s.Description != "" {
		lines = append(lines, "", wrapText(s.Description, 60))
	}
	lines = append(lines, "", m.theme.MutedText.Render("source: "+s.Path))
	return strings.Join(lines, "\n")
}

// wrapText is a minimal word wrapper for the Skill detail column.
// It splits on spaces and folds the text to at most width columns
// per line, leaving a single blank after hard wraps.
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	var out strings.Builder
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			out.WriteString(line)
			out.WriteByte('\n')
			line = w
			continue
		}
		line += " " + w
	}
	out.WriteString(line)
	return out.String()
}
