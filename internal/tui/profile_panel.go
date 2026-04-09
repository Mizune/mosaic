package tui

import (
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// profilePanelState backs the profile switcher modal (`P` key).
// Switching profiles live would require tearing down the store
// and every live-layer subscription, which is far too invasive
// for a polish feature. Instead the modal prints a one-liner
// the user can paste into the terminal on exit; cmd_root picks
// that up and restart the binary with the right flag.
type profilePanelState struct {
	active   bool
	profiles []string
	cursor   int
	current  string
}

// openProfilePanel scans ~/.mosaic/profiles/ for every directory
// that looks like a profile (contains state.db or is empty) and
// flips the screen to the picker.
func (m *Model) openProfilePanel() {
	profiles := discoverProfiles(m.configPaths.Profile)
	// Include the default profile even when the directory is
	// missing so a first-time user is never stuck.
	if len(profiles) == 0 {
		profiles = []string{"default"}
	}
	current := m.config.Mosaic.Profile
	if current == "" {
		current = "default"
	}
	// Highlight the currently-active profile.
	cursor := 0
	for i, p := range profiles {
		if p == current {
			cursor = i
			break
		}
	}
	m.profilePanel = profilePanelState{
		active:   true,
		profiles: profiles,
		cursor:   cursor,
		current:  current,
	}
	m.screen = ScreenProfile
}

// discoverProfiles lists every subdirectory of ~/.mosaic/profiles
// that plausibly looks like a mosaic profile. A subdirectory is
// eligible when it contains `state.db` (the active profile) or
// when it is empty but was explicitly created by the user.
func discoverProfiles(profileDir string) []string {
	// profileDir from configPaths is the CURRENT profile's
	// dir, e.g. .../profiles/default. Walk up one level to
	// find siblings.
	if profileDir == "" {
		return nil
	}
	parent := ""
	if idx := strings.LastIndex(profileDir, "/profiles/"); idx >= 0 {
		parent = profileDir[:idx+len("/profiles/")]
	}
	if parent == "" {
		return nil
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// updateProfilePanel handles keys inside the profile picker.
func (m *Model) updateProfilePanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.closeProfilePanel()
		return m, nil
	case "j", "down":
		if m.profilePanel.cursor < len(m.profilePanel.profiles)-1 {
			m.profilePanel.cursor++
		}
		return m, nil
	case "k", "up":
		if m.profilePanel.cursor > 0 {
			m.profilePanel.cursor--
		}
		return m, nil
	case "enter":
		return m.switchProfile()
	}
	return m, nil
}

// closeProfilePanel drops the picker state and returns to TOP.
func (m *Model) closeProfilePanel() {
	m.profilePanel = profilePanelState{}
	m.screen = ScreenTop
}

// switchProfile records the chosen profile on the model as a
// pending restart target and quits the TUI. cmd_root checks
// m.PendingProfile() after the Bubble Tea program returns and
// either prints a hint or execs a fresh mosaic.
func (m *Model) switchProfile() (tea.Model, tea.Cmd) {
	if m.profilePanel.cursor < 0 || m.profilePanel.cursor >= len(m.profilePanel.profiles) {
		return m, nil
	}
	target := m.profilePanel.profiles[m.profilePanel.cursor]
	if target == m.profilePanel.current {
		m.setTopToast("already on profile " + target)
		m.closeProfilePanel()
		return m, nil
	}
	m.pendingProfile = target
	m.closeProfilePanel()
	return m, tea.Quit
}

// renderProfilePanel draws the picker.
func (m *Model) renderProfilePanel() string {
	p := m.profilePanel
	title := m.theme.SectionTitle.Render("Switch profile")

	var lines []string
	lines = append(lines, title, "")
	for i, name := range p.profiles {
		marker := "  "
		if i == p.cursor {
			marker = m.theme.AttentionTag.Render("▶ ")
		}
		label := name
		if name == p.current {
			label += "  " + m.theme.MutedText.Render("(current)")
		}
		lines = append(lines, marker+label)
	}
	lines = append(lines, "",
		m.theme.MutedText.Render("Enter: switch (restarts mosaic)  •  q/Esc: cancel"))

	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Accent).
		Padding(1, 2).
		Width(m.width/2).
		Render(strings.Join(lines, "\n"))

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
