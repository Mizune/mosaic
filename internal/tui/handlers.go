package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/store"
)

// handleKey is the Update branch for tea.KeyMsg.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Confirmation overlay swallows every key except y / n / Esc /
	// Ctrl+C. It is the highest-priority screen so even the wizard
	// or help overlay cannot leak input into it.
	if m.confirm.active {
		return m.handleConfirmKey(msg)
	}

	// Wizard owns its own key handling so the user can type the
	// session name without colliding with the global shortcuts.
	if m.screen == ScreenNewSession {
		// Allow Ctrl+C to bail out of the wizard immediately.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.updateWizard(msg)
	}

	// MCP Manager is modal: it owns every key while open so the
	// global j/k navigation does not leak into the left tree.
	if m.screen == ScreenMCPManager {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.updateMCPPanel(msg)
	}
	// Skill Manager: same modal discipline as the MCP manager.
	if m.screen == ScreenSkillManager {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.updateSkillPanel(msg)
	}
	// Settings screen: interactive field editor. Ctrl+C quits
	// mosaic; everything else is routed through updateSettings.
	if m.screen == ScreenSettings {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.updateSettings(msg)
	}
	// Conductor Panel is modal like MCP/Skill.
	if m.screen == ScreenConductor {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.updateConductorPanel(msg)
	}
	// Conductor Wizard is modal: owns all keys while open.
	if m.screen == ScreenConductorWizard {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.updateConductorWizard(msg)
	}
	// Diff View: modal, same pattern.
	if m.screen == ScreenDiff {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.updateDiffPanel(msg)
	}
	// First-run wizard owns the entire keyboard until it's
	// dismissed; the TOP screen is hidden behind it.
	if m.screen == ScreenFirstRun {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.updateFirstRunWizard(msg)
	}
	// Error log: simple viewer, q/esc closes.
	if m.screen == ScreenErrorLog {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc", "E":
			m.screen = ScreenTop
			return m, nil
		}
		return m, nil
	}
	// Profile picker.
	if m.screen == ScreenProfile {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.updateProfilePanel(msg)
	}
	// Recipe wizard: modal, routes to the recipe wizard handler
	// which manages its own key grammar.
	if m.screen == ScreenRecipeWizard {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.updateRecipeWizardScreen(msg)
	}

	if matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}
	if matches(msg, m.keys.Help) {
		if m.screen == ScreenHelp {
			m.screen = ScreenTop
		} else {
			m.screen = ScreenHelp
		}
		return m, nil
	}
	if m.screen == ScreenHelp {
		// In help, any other key returns to TOP.
		m.screen = ScreenTop
		return m, nil
	}

	switch {
	case matches(msg, m.keys.Down):
		m.moveCursor(1)
	case matches(msg, m.keys.Up):
		m.moveCursor(-1)
	case matches(msg, m.keys.Left):
		m.collapseCurrentGroup()
	case matches(msg, m.keys.Right):
		m.expandCurrentGroup()
	case matches(msg, m.keys.Refresh):
		return m, m.refreshCmd()
	case matches(msg, m.keys.Attach):
		return m.attachSelected()
	case matches(msg, m.keys.Kill):
		return m.killSelected()
	case matches(msg, m.keys.ToggleSelect):
		m.toggleMark()
		return m, nil
	case matches(msg, m.keys.ClearSelect):
		m.selected = map[string]bool{}
		return m, nil
	case matches(msg, m.keys.SelectAll):
		m.markAll()
		return m, nil
	case matches(msg, m.keys.Fork):
		return m.forkSelected()
	case matches(msg, m.keys.Respawn):
		return m.respawnSelected()

	case matches(msg, m.keys.FilterWaiting):
		m.setStatusFilter(statusFilterWaiting)
		return m, nil
	case matches(msg, m.keys.FilterRunning):
		m.setStatusFilter(statusFilterRunning)
		return m, nil
	case matches(msg, m.keys.FilterIdle):
		m.setStatusFilter(statusFilterIdle)
		return m, nil
	case matches(msg, m.keys.FilterError):
		m.setStatusFilter(statusFilterError)
		return m, nil
	case matches(msg, m.keys.FilterAll):
		m.setStatusFilter(statusFilterNone)
		return m, nil

	case matches(msg, m.keys.NextAttention):
		m.jumpToNextAttention()
		return m, nil

	case matches(msg, m.keys.DismissAtt):
		return m, m.dismissAttention()

	case matches(msg, m.keys.ErrorLog):
		m.screen = ScreenErrorLog
		return m, nil

	case matches(msg, m.keys.VisualStart):
		m.toggleVisualMode()
		return m, nil
	case matches(msg, m.keys.VisualLineStart):
		m.markCurrentGroup()
		return m, nil

	case matches(msg, m.keys.Profile):
		m.openProfilePanel()
		return m, nil

	case msg.String() == "tab":
		m.previewMode = (m.previewMode + 1) % 4
		return m, nil
	case matches(msg, m.keys.New):
		m.startWizard()
		return m, nil
	case matches(msg, m.keys.MCPManager):
		m.openMCPManager()
		return m, nil
	case matches(msg, m.keys.SkillManager):
		m.openSkillManager()
		return m, nil
	case matches(msg, m.keys.Settings):
		m.screen = ScreenSettings
		return m, nil
	case matches(msg, m.keys.Conductor):
		m.openConductorPanel()
		return m, nil
	case matches(msg, m.keys.Diff):
		m.openDiffPanel()
		return m, nil
	}
	return m, nil
}

// handleConfirmKey implements the tiny state machine behind the
// confirmation overlay: y runs the callback, n/Esc/Ctrl+C cancel,
// everything else is ignored so an errant keystroke cannot dismiss
// a destructive prompt.
func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		cb := m.confirm.onConfirm
		m.confirm = confirmState{}
		if cb != nil {
			return m, cb()
		}
		return m, nil
	case "n", "N", "esc", "ctrl+[":
		m.confirm = confirmState{}
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor += delta
	// Skip sub-agent rows — they are display-only and selecting
	// them blocks session navigation without showing useful info.
	for m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].IsSubagent {
		m.cursor += delta
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	// After clamping, the cursor can still land on a sub-agent
	// row (first/last row is a sub-agent, or overshoot hit the
	// edge). Walk backwards from the clamped position to find
	// the nearest non-sub-agent row. If none exist, stay put.
	if m.rows[m.cursor].IsSubagent {
		for i := m.cursor; i >= 0; i-- {
			if !m.rows[i].IsSubagent {
				m.cursor = i
				break
			}
		}
	}
	if m.rows[m.cursor].IsSubagent {
		// No non-sub-agent row at or before cursor — try forward.
		for i := m.cursor; i < len(m.rows); i++ {
			if !m.rows[i].IsSubagent {
				m.cursor = i
				break
			}
		}
	}
	// Extend the visual range whenever cursor moves while
	// visual mode is on. The set is recomputed from the
	// anchor so crossing back over the starting row shrinks
	// the selection instead of orphaning previously-marked
	// rows.
	if m.visualAnchor >= 0 {
		m.applyVisualSelection()
	}
}

// toggleVisualMode flips visual selection on / off. Turning it
// on anchors at the cursor and marks the row under the cursor;
// turning it off keeps the current selection so the user can
// run a batch action on it.
func (m *Model) toggleVisualMode() {
	if m.visualAnchor >= 0 {
		m.visualAnchor = -1
		return
	}
	if len(m.rows) == 0 {
		return
	}
	row := m.rows[m.cursor]
	if row.IsGroup || row.IsSubagent {
		return
	}
	m.visualAnchor = m.cursor
	m.applyVisualSelection()
}

// applyVisualSelection walks the inclusive range between the
// visual anchor and the current cursor and marks every session
// row in that span. Existing marks from Space / * are preserved
// so visual mode stacks on top of prior selections.
func (m *Model) applyVisualSelection() {
	if m.visualAnchor < 0 {
		return
	}
	lo, hi := m.visualAnchor, m.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	if m.selected == nil {
		m.selected = map[string]bool{}
	}
	for i := lo; i <= hi && i < len(m.rows); i++ {
		r := m.rows[i]
		if r.IsGroup || r.IsSubagent {
			continue
		}
		m.selected[r.Session.ID] = true
	}
}

// markCurrentGroup marks every session underneath the group the
// cursor is currently on (or every session of the parent group
// when the cursor is on a session row). It is the `V` key
// equivalent of "select this project".
func (m *Model) markCurrentGroup() {
	if len(m.rows) == 0 {
		return
	}
	// Walk upward to find the group header the cursor sits
	// under, then forward to mark every session before the
	// next group row.
	start := m.cursor
	for start >= 0 && !m.rows[start].IsGroup {
		start--
	}
	if start < 0 {
		return
	}
	if m.selected == nil {
		m.selected = map[string]bool{}
	}
	for i := start + 1; i < len(m.rows); i++ {
		r := m.rows[i]
		if r.IsGroup {
			break
		}
		if r.IsSubagent {
			continue
		}
		m.selected[r.Session.ID] = true
	}
}

func (m *Model) collapseCurrentGroup() {
	if len(m.rows) == 0 {
		return
	}
	row := m.rows[m.cursor]
	switch {
	case row.IsGroup:
		m.groupExpanded[row.GroupName] = false
	default:
		// On a session or sub-agent row, "left" jumps to the parent
		// group header and collapses it.
		for i := m.cursor; i >= 0; i-- {
			if m.rows[i].IsGroup {
				m.cursor = i
				m.groupExpanded[m.rows[i].GroupName] = false
				break
			}
		}
	}
	// Re-render from the cached authoritative list — not from
	// m.rows, which contains sub-agent entries whose .Session
	// value duplicates the parent.
	m.rebuildRows()
}

func (m *Model) expandCurrentGroup() {
	if len(m.rows) == 0 {
		return
	}
	row := m.rows[m.cursor]
	if row.IsGroup {
		m.groupExpanded[row.GroupName] = true
		m.rebuildRows()
	}
}

func (m *Model) attachSelected() (tea.Model, tea.Cmd) {
	if len(m.rows) == 0 {
		return m, nil
	}
	row := m.rows[m.cursor]
	if row.IsGroup {
		return m, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd, err := m.sessions.AttachCommand(ctx, row.Session.Name)
	if err != nil {
		m.flagError("attach failed", err.Error())
		return m, nil
	}
	// After the user detaches from tmux, fetch a fresh snapshot so
	// status columns reflect whatever changed while they were
	// inside the pane. Returning an empty refreshedMsg here would
	// briefly blank the list.
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return m.fetchRefreshed(ctx)
	})
}

// setStatusFilter switches the TOP filter tab and rebuilds the
// row list. The cursor is clamped into range after the filter
// may have shrunk the visible set.
func (m *Model) setStatusFilter(mode statusFilterMode) {
	if m.statusFilter == mode {
		// Toggling the same filter clears it — quick way back
		// to "show all" without needing to remember `0`.
		mode = statusFilterNone
	}
	m.statusFilter = mode
	m.rebuildRows()
	m.setTopToast("filter: " + mode.label())
}

// jumpToNextAttention moves the cursor to the next session row
// whose attention state is waiting_input, wrapping around if the
// cursor is already past the last one. When no attention is
// pending the call is a no-op.
func (m *Model) jumpToNextAttention() {
	if len(m.rows) == 0 {
		return
	}
	start := m.cursor
	n := len(m.rows)
	for i := 1; i <= n; i++ {
		idx := (start + i) % n
		row := m.rows[idx]
		if row.IsGroup || row.IsSubagent {
			continue
		}
		if row.Session.AttentionState == string(store.AttentionWaitingInput) {
			m.cursor = idx
			m.setTopToast("jumped to " + row.Session.Name)
			return
		}
	}
	m.setTopToast("no attention pending")
}

// dismissAttention clears the attention state for the session
// currently under the cursor. Useful when the detector flagged
// a false positive the user wants to move past without
// interacting with the pane.
func (m *Model) dismissAttention() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	row := m.rows[m.cursor]
	if row.IsGroup || row.IsSubagent {
		return nil
	}
	id := row.Session.ID
	name := row.Session.Name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if m.store == nil {
			return nil
		}
		_ = m.store.Sessions().SetAttention(ctx, id,
			store.AttentionNone, "", "", "")
		return dismissToastMsg{name: name}
	}
}

// dismissToastMsg lands in Update when a dismiss completes.
type dismissToastMsg struct {
	name string
}

// forkSelected triggers a quick fork of the session under the
// cursor. The heavy lifting is done by [SessionService.Fork],
// which creates the new worktree, branch, and tmux session. We
// surface the outcome via the usual toast / error path.
func (m *Model) forkSelected() (tea.Model, tea.Cmd) {
	if len(m.rows) == 0 {
		return m, nil
	}
	row := m.rows[m.cursor]
	if row.IsGroup || row.IsSubagent {
		return m, nil
	}
	name := row.Session.Name
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := m.sessions.Fork(ctx, name); err != nil {
			return forkResultMsg{err: err.Error()}
		}
		return forkResultMsg{success: true, source: name}
	}
}

// forkResultMsg carries the outcome of a fork action back to the
// Update loop so it can refresh and surface a status message.
type forkResultMsg struct {
	success bool
	source  string
	err     string
}

// respawnSelected respawns the stopped session(s) under the cursor
// or all Space-marked stopped sessions. Sessions that are already
// alive are silently skipped.
func (m *Model) respawnSelected() (tea.Model, tea.Cmd) {
	if len(m.rows) == 0 {
		return m, nil
	}

	// Collect target names: marked sessions take precedence over cursor.
	// Only stopped sessions are included — running ones are skipped.
	var names []string
	if len(m.selected) > 0 {
		for _, row := range m.rows {
			if row.IsGroup || row.IsSubagent {
				continue
			}
			if m.selected[row.Session.ID] && row.Session.Status == string(store.StatusStopped) {
				names = append(names, row.Session.Name)
			}
		}
		if len(names) == 0 {
			m.setTopToast("no stopped sessions in selection")
			return m, nil
		}
	} else {
		row := m.rows[m.cursor]
		if row.IsGroup || row.IsSubagent {
			return m, nil
		}
		if row.Session.Status != string(store.StatusStopped) {
			m.setTopToast("session already running")
			return m, nil
		}
		names = []string{row.Session.Name}
	}

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var total, success int
		var failed []string
		for _, n := range names {
			total++
			if err := m.sessions.Respawn(ctx, n); err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", n, err))
			} else {
				success++
			}
		}
		return respawnResultMsg{total: total, success: success, failed: failed}
	}
}

// respawnResultMsg carries the outcome of a respawn action back to
// the Update loop.
type respawnResultMsg struct {
	total   int
	success int
	failed  []string
}

// toggleMark flips the selection state for the session under the
// cursor. Group headers and sub-agent rows are ignored — only
// real sessions can be multi-selected.
func (m *Model) toggleMark() {
	if len(m.rows) == 0 {
		return
	}
	row := m.rows[m.cursor]
	if row.IsGroup || row.IsSubagent {
		return
	}
	if m.selected == nil {
		m.selected = map[string]bool{}
	}
	if m.selected[row.Session.ID] {
		delete(m.selected, row.Session.ID)
	} else {
		m.selected[row.Session.ID] = true
	}
}

// markAll marks every session row currently visible. Collapsed
// groups are NOT included — the "visible" definition matches
// what Space would toggle.
func (m *Model) markAll() {
	if m.selected == nil {
		m.selected = map[string]bool{}
	}
	for _, row := range m.rows {
		if row.IsGroup || row.IsSubagent {
			continue
		}
		m.selected[row.Session.ID] = true
	}
}

// killSelected pops a confirmation overlay instead of immediately
// tearing the session down. When one or more rows have been
// Space-marked, the confirmation lists ALL of them and the
// callback runs the kills in sequence. Otherwise the single
// session under the cursor is killed.
func (m *Model) killSelected() (tea.Model, tea.Cmd) {
	names := m.markedSessionNames()
	if len(names) == 0 {
		if len(m.rows) == 0 {
			return m, nil
		}
		row := m.rows[m.cursor]
		if row.IsGroup || row.IsSubagent {
			return m, nil
		}
		names = []string{row.Session.Name}
	}

	title := "Kill session?"
	body := "This ends the tmux session and removes its row:\n\n  " + names[0]
	if len(names) > 1 {
		title = fmt.Sprintf("Kill %d sessions?", len(names))
		body = "This ends the tmux sessions and removes the rows:\n\n  " +
			strings.Join(names, "\n  ")
	}
	m.confirm = confirmState{
		active:   true,
		title:    title,
		body:     body,
		yesLabel: "kill",
		onConfirm: func() tea.Cmd {
			return m.performBatchKill(names)
		},
	}
	return m, nil
}

// markedSessionNames returns the names of every currently marked
// session in the same order they appear in the row list. An
// empty return value means "no multi-selection; act on the
// cursor row instead".
func (m *Model) markedSessionNames() []string {
	if len(m.selected) == 0 {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for _, row := range m.rows {
		if row.IsGroup || row.IsSubagent {
			continue
		}
		if m.selected[row.Session.ID] && !seen[row.Session.ID] {
			names = append(names, row.Session.Name)
			seen[row.Session.ID] = true
		}
	}
	return names
}

// performBatchKill is the multi-session variant of performKill.
// Each kill runs sequentially — the goroutine bails on the first
// hard error so the user can retry.
func (m *Model) performBatchKill(names []string) tea.Cmd {
	// Optimistic update: drop killed sessions from the cached
	// list and clear the selection immediately.
	deleted := map[string]bool{}
	for _, n := range names {
		deleted[n] = true
	}
	filtered := make([]store.Session, 0, len(m.lastSessions))
	for _, s := range m.lastSessions {
		if deleted[s.Name] {
			continue
		}
		filtered = append(filtered, s)
	}
	m.lastSessions = filtered
	m.selected = map[string]bool{}
	m.visualAnchor = -1
	m.rebuildRows()

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, n := range names {
			_ = m.sessions.Kill(ctx, n)
		}
		return m.fetchRefreshed(ctx)
	}
}

// performKill does the optimistic-update + background kill dance
// that the old killSelected inlined. It is reachable only via the
// confirmation modal now.
func (m *Model) performKill(name string) tea.Cmd {
	// Optimistic update: drop the killed session from the cached
	// list *immediately* so the UI feels snappy. The background
	// goroutine still runs Kill and does a real refresh; if it
	// finds a discrepancy the subsequent refreshedMsg corrects it.
	filtered := make([]store.Session, 0, len(m.lastSessions))
	for _, s := range m.lastSessions {
		if s.Name == name {
			continue
		}
		filtered = append(filtered, s)
	}
	m.lastSessions = filtered
	m.rebuildRows()

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = m.sessions.Kill(ctx, name)
		return m.fetchRefreshed(ctx)
	}
}
