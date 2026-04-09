package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/mcp"
)

// mcpPanelState carries everything the MCP Manager modal needs to
// render itself and respond to keys. It is zeroed out on close so
// reopening the modal always starts from a fresh snapshot of disk
// state.
type mcpPanelState struct {
	// sessionID is the mosaic session this panel is editing. The
	// modal always opens for the row the cursor was on when `m`
	// was pressed; changing the outer cursor closes + reopens.
	sessionID   string
	sessionName string

	// servers is every MCP discovered on disk, sorted by name.
	servers []mcp.Server

	// attached is the set of MCP names currently attached to
	// sessionID according to the store.
	attached map[string]bool

	// cursor is the index into servers that the keyboard selection
	// highlights.
	cursor int

	// loadErr captures any fatal error during the initial load so
	// the modal can explain why it is empty instead of silently
	// hiding the failure.
	loadErr string
}

// openMCPManager populates mcpPanel and switches to the MCP screen.
// It is invoked from the main key handler when the user presses the
// MCPManager key on a session row (group headers and sub-agent
// rows are ignored because they don't have an owning session).
func (m *Model) openMCPManager() {
	if len(m.rows) == 0 {
		return
	}
	row := m.rows[m.cursor]
	if row.IsGroup || row.IsSubagent {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	panel := mcpPanelState{
		sessionID:   row.Session.ID,
		sessionName: row.Session.Name,
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		servers, err := mcp.Discover(
			filepath.Join(home, ".claude.json"),
			filepath.Join(home, ".claude", "settings.json"),
		)
		if err != nil {
			panel.loadErr = err.Error()
		} else {
			panel.servers = servers
		}
	}

	if m.store != nil {
		attached, err := m.store.SessionMCPs().AttachedNames(ctx, panel.sessionID)
		if err == nil {
			panel.attached = attached
		}
	}
	if panel.attached == nil {
		panel.attached = map[string]bool{}
	}

	m.mcpPanel = panel
	m.screen = ScreenMCPManager
}

// closeMCPManager returns the TUI to the TOP screen and releases the
// panel state.
func (m *Model) closeMCPManager() {
	m.mcpPanel = mcpPanelState{}
	m.screen = ScreenTop
}

// updateMCPPanel handles a single key inside the MCP Manager modal.
// Supported keys:
//
//	j/↓  k/↑     move cursor
//	space/enter  toggle attach state for the selected server
//	q / esc      close and return to TOP
func (m *Model) updateMCPPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.closeMCPManager()
		return m, nil

	case "j", "down":
		if m.mcpPanel.cursor < len(m.mcpPanel.servers)-1 {
			m.mcpPanel.cursor++
		}
		return m, nil

	case "k", "up":
		if m.mcpPanel.cursor > 0 {
			m.mcpPanel.cursor--
		}
		return m, nil

	case " ", "enter":
		return m.mcpPanelToggle()
	}
	return m, nil
}

// mcpPanelToggle flips the attach state of the currently highlighted
// MCP server, persists the change, and updates the in-memory set
// so the next render reflects it immediately.
func (m *Model) mcpPanelToggle() (tea.Model, tea.Cmd) {
	p := &m.mcpPanel
	if p.cursor < 0 || p.cursor >= len(p.servers) {
		return m, nil
	}
	name := p.servers[p.cursor].Name
	if m.store == nil {
		return m, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if p.attached[name] {
		if err := m.store.SessionMCPs().Detach(ctx, p.sessionID, name); err != nil {
			m.flagError("detach mcp failed", err.Error())
			return m, nil
		}
		delete(p.attached, name)
	} else {
		if err := m.store.SessionMCPs().Attach(ctx, p.sessionID, name); err != nil {
			m.flagError("attach mcp failed", err.Error())
			return m, nil
		}
		p.attached[name] = true
	}
	return m, nil
}

// renderMCPPanel draws the MCP Manager full-screen modal. The
// layout is a two-column split: the left column lists every
// discovered server with an attach checkbox, the right column
// shows the details of the highlighted entry.
func (m *Model) renderMCPPanel() string {
	p := m.mcpPanel

	title := m.theme.SectionTitle.Render("MCP Manager: " + p.sessionName)

	if p.loadErr != "" {
		body := m.theme.AttentionTag.Render("✗ discover failed: "+p.loadErr) + "\n\n" +
			m.theme.MutedText.Render("q / Esc to return")
		return m.renderMCPBox(title + "\n\n" + body)
	}
	if len(p.servers) == 0 {
		body := m.theme.MutedText.Render("No MCP servers discovered in ~/.claude.json or ~/.claude/settings.json.") +
			"\n\n" + m.theme.MutedText.Render("q / Esc to return")
		return m.renderMCPBox(title + "\n\n" + body)
	}

	// Left column: checkbox + name, one per line, sized so both
	// columns fit inside half the screen width without wrapping.
	leftWidth := m.width/2 - 6
	if leftWidth < 20 {
		leftWidth = 20
	}
	lines := make([]string, 0, len(p.servers))
	for i, s := range p.servers {
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

	// Right column: details of the highlighted server.
	right := m.renderMCPDetail(p.servers[p.cursor], p.attached)

	leftBox := lipgloss.NewStyle().Width(leftWidth).Render(left)
	rightBox := lipgloss.NewStyle().Width(m.width - leftWidth - 8).Render(right)
	cols := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	footer := m.theme.MutedText.Render(
		"j/k: move   space/enter: toggle   q/Esc: close")

	return m.renderMCPBox(title + "\n\n" + cols + "\n\n" + footer)
}

// renderMCPBox wraps body in the shared modal frame. Kept for
// backwards compatibility — every panel now goes through
// [Model.renderModalBox] which uses the Accent colour and
// renders a thicker border.
func (m *Model) renderMCPBox(body string) string {
	return m.renderModalBox(body)
}

// renderModalBox is the canonical full-screen modal frame used
// by every panel (MCP / Skill / Conductor / Diff / Settings /
// Profile / FirstRun / ErrorLog). Centralising the style here
// guarantees a consistent look — same border, padding, accent
// colour, centred placement — even as new panels are added.
func (m *Model) renderModalBox(body string) string {
	width := m.width - 8
	if width < 60 {
		width = 60
	}
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Accent).
		Padding(1, 2).
		Width(width).
		Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// renderMCPDetail formats the right-hand details column for a single
// discovered MCP server.
func (m *Model) renderMCPDetail(s mcp.Server, attached map[string]bool) string {
	status := "○ detached"
	if attached[s.Name] {
		status = "● attached"
	}
	lines := []string{
		m.theme.SectionTitle.Render(s.Name),
		"",
		"status:  " + status,
		"command: " + ifEmpty(s.Command, "(empty)"),
	}
	if len(s.Args) > 0 {
		lines = append(lines, "args:    "+strings.Join(s.Args, " "))
	}
	if s.Type != "" {
		lines = append(lines, "type:    "+s.Type)
	}
	if len(s.EnvKeys) > 0 {
		lines = append(lines,
			"env:     "+strings.Join(s.EnvKeys, ", ")+
				m.theme.MutedText.Render("  (values hidden)"))
	}
	lines = append(lines, "", m.theme.MutedText.Render("source: "+s.Source))
	return strings.Join(lines, "\n")
}

