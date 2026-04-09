package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/conductor"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
)

// conductorPanelState backs the Conductor Panel modal (`c` key).
//
// The panel is a two-pane layout:
//
//	left  — list of conductors + their task tree for the selected
//	        conductor, rendered as an ASCII DAG
//	right — details for the highlighted row: either a conductor
//	        summary or a specific task's status / dependencies /
//	        prompt / result
//
// State is re-fetched from the store on open and on refresh; the
// panel does not subscribe to the bus because the conductor's
// lifecycle is slow-moving enough that a manual `R` press is all
// the user needs.
type conductorPanelState struct {
	conductors []store.Conductor

	// tasks is keyed by conductor id.
	tasks map[string][]store.ConductorTask

	// selection identifies which row in the flattened left tree is
	// currently focused.
	selection conductorSelection

	// cursor is the index into the flattened left tree.
	cursor int

	// rows is the rendered list — rebuilt every time the panel
	// data or cursor changes so selection math stays in sync.
	rows []conductorRow

	loadErr string

	// sendModal is the inline "send a message to this worker"
	// modal. When active, key dispatch in updateConductorPanel is
	// routed to updateConductorSendModal instead of the browse
	// switch so keys type into the input buffer.
	sendModal conductorSendModal

	// planModal is the "start a new conductor from a plan.toml
	// path" input. It is much smaller than sendModal — the user
	// types a path, Enter kicks Prepare + Run.
	planModal conductorPlanModal

	// toast is a short transient status message shown in the
	// footer (pause / resume / retry / send confirmation).
	toast        string
	toastExpires time.Time
}

// conductorPlanModal backs the "+ new conductor" input.
type conductorPlanModal struct {
	active bool
	path   string
	err    string
}

// conductorSendModal backs the `!` send-message overlay.
type conductorSendModal struct {
	active      bool
	targetID    string // mosaic session id being addressed
	targetName  string // display name
	conductorID string // optional: the conductor this task belongs to
	body        string
	err         string
}

// conductorRow is one rendered line in the left pane. It is either
// a conductor header or a task indented under its conductor.
type conductorRow struct {
	IsTask      bool
	ConductorID string
	Conductor   store.Conductor
	Task        store.ConductorTask
	Indent      string
}

// conductorSelection captures the current cursor target — either a
// conductor header or a specific task under a conductor. This lives
// next to the flat rows so the right pane can look it up without
// re-traversing state.
type conductorSelection struct {
	ConductorID string
	TaskID      string // empty when the cursor is on the header
}

// openConductorPanel builds the panel state from store and switches
// to the Conductor screen.
func (m *Model) openConductorPanel() {
	state := conductorPanelState{
		tasks: map[string][]store.ConductorTask{},
	}
	if m.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		rows, err := m.store.Conductors().List(ctx)
		if err != nil {
			state.loadErr = err.Error()
		} else {
			state.conductors = rows
			for _, c := range rows {
				tasks, terr := m.store.Conductors().ListTasks(ctx, c.ID)
				if terr != nil {
					state.loadErr = terr.Error()
					continue
				}
				state.tasks[c.ID] = tasks
			}
		}
	}
	state.rebuild()
	m.conductorPanel = state
	m.screen = ScreenConductor
}

// closeConductorPanel drops panel state and returns to the TOP screen.
func (m *Model) closeConductorPanel() {
	m.conductorPanel = conductorPanelState{}
	m.screen = ScreenTop
}

func (m *Model) updateConductorPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Modal overlays own the keyboard while they are active.
	if m.conductorPanel.sendModal.active {
		return m.updateConductorSendModal(msg)
	}
	if m.conductorPanel.planModal.active {
		return m.updateConductorPlanModal(msg)
	}

	switch msg.String() {
	case "q", "esc":
		m.closeConductorPanel()
		return m, nil

	case "j", "down":
		if m.conductorPanel.cursor < len(m.conductorPanel.rows)-1 {
			m.conductorPanel.cursor++
			m.conductorPanel.updateSelection()
		}
		return m, nil

	case "k", "up":
		if m.conductorPanel.cursor > 0 {
			m.conductorPanel.cursor--
			m.conductorPanel.updateSelection()
		}
		return m, nil

	case "R":
		m.openConductorPanel() // re-fetch
		return m, nil

	case "enter", "l":
		// Jump to the worker session assigned to the selected
		// task. The panel closes and the TOP cursor moves onto
		// the matching row.
		return m.jumpToConductorWorker()

	case "p":
		// Toggle pause/resume on the selected conductor.
		return m, m.toggleConductorPause()

	case "r":
		// Retry the selected failed task by flipping it back
		// to pending. The runner picks it up on its next
		// dispatch round.
		return m, m.retryConductorTask()

	case "M":
		// Open the send-message modal if the cursor is on a
		// task that has an assigned worker session. Using
		// capital M leaves "!" free as a global filter
		// shortcut on the TOP screen.
		m.openConductorSendModal()
		return m, nil

	case "+":
		// Open the conductor creation wizard so the user can
		// build and run a plan entirely from the TUI.
		m.openConductorWizard()
		return m, nil

	case "Y":
		// Replay the plan from the currently selected
		// conductor row. Needs a PlanPath on the row — draft
		// conductors without a plan file cannot be replayed.
		return m, m.replayConductor()
	}
	return m, nil
}

// updateConductorPlanModal handles keystrokes inside the "+ new
// conductor" input overlay.
func (m *Model) updateConductorPlanModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pm := &m.conductorPanel.planModal
	switch msg.String() {
	case "esc":
		m.conductorPanel.planModal = conductorPlanModal{}
		return m, nil
	case "enter":
		return m, m.runConductorPlan()
	case "backspace":
		if n := len(pm.path); n > 0 {
			pm.path = pm.path[:n-1]
		}
		return m, nil
	}
	if r := msg.String(); len(r) == 1 {
		pm.path += r
	}
	return m, nil
}

// runConductorPlan loads the plan.toml at the modal's path and
// kicks off a Prepare + Run cycle in the background. On success
// the panel refreshes so the new conductor shows up in the list;
// on failure the error lands in the modal body and stays up.
func (m *Model) runConductorPlan() tea.Cmd {
	pm := &m.conductorPanel.planModal
	path := strings.TrimSpace(pm.path)
	if path == "" {
		pm.err = "plan path is required"
		return nil
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err == nil {
			path = abs
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		plan, err := conductor.LoadPlan(path)
		if err != nil {
			return conductorPanelToastMsg{text: "load plan: " + err.Error()}
		}
		project := plan.Conductor.Project
		if project == "" {
			project = filepath.Dir(path)
		}
		if abs, absErr := filepath.Abs(project); absErr == nil {
			project = abs
		}
		runner := conductor.NewRunner(m.store, m.sessions)
		if err := runner.Prepare(ctx, plan, project, path); err != nil {
			return conductorPanelToastMsg{text: "prepare: " + err.Error()}
		}
		// Run in a detached goroutine — the TUI stays
		// responsive while the DAG churns. The runner writes
		// everything to the store so the panel will reflect
		// progress on subsequent refreshes.
		go func() {
			bg, bgCancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer bgCancel()
			_ = runner.Run(bg, conductor.SessionDispatch(m.sessions))
		}()
		return conductorPanelRefreshMsg{toast: "running " + plan.Conductor.Name}
	}
}

// replayConductor re-runs the plan file attached to the currently
// selected conductor. Draft / manual-entry conductors without a
// stored PlanPath are reported back to the user via a toast.
func (m *Model) replayConductor() tea.Cmd {
	row := m.conductorPanelCurrentRow()
	if row == nil {
		return nil
	}
	if !row.Conductor.PlanPath.Valid || row.Conductor.PlanPath.String == "" {
		m.setConductorToast("conductor has no plan file to replay")
		return nil
	}
	path := row.Conductor.PlanPath.String
	m.conductorPanel.planModal = conductorPlanModal{active: true, path: path}
	return m.runConductorPlan()
}

// updateConductorSendModal owns keyboard input while the send
// overlay is active: printable keys append to the body, backspace
// removes the last rune, Enter submits, Esc cancels.
func (m *Model) updateConductorSendModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sm := &m.conductorPanel.sendModal
	switch msg.String() {
	case "esc":
		m.conductorPanel.sendModal = conductorSendModal{}
		return m, nil
	case "enter":
		return m, m.submitConductorSendModal()
	case "backspace":
		if n := len(sm.body); n > 0 {
			sm.body = sm.body[:n-1]
		}
		return m, nil
	}
	if r := msg.String(); len(r) == 1 {
		sm.body += r
	}
	return m, nil
}

// rebuild flattens the conductors map into a linear row list and
// resets the cursor / selection.
func (s *conductorPanelState) rebuild() {
	s.rows = nil
	// Newest conductor first so the most recent run is at the top
	// of the list.
	sorted := make([]store.Conductor, len(s.conductors))
	copy(sorted, s.conductors)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
	})
	for _, c := range sorted {
		s.rows = append(s.rows, conductorRow{
			ConductorID: c.ID,
			Conductor:   c,
		})
		tasks := s.tasks[c.ID]
		for i, t := range tasks {
			indent := "├─ "
			if i == len(tasks)-1 {
				indent = "└─ "
			}
			s.rows = append(s.rows, conductorRow{
				IsTask:      true,
				ConductorID: c.ID,
				Conductor:   c,
				Task:        t,
				Indent:      indent,
			})
		}
	}
	if s.cursor >= len(s.rows) {
		s.cursor = 0
	}
	s.updateSelection()
}

func (s *conductorPanelState) updateSelection() {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		s.selection = conductorSelection{}
		return
	}
	row := s.rows[s.cursor]
	s.selection = conductorSelection{ConductorID: row.ConductorID}
	if row.IsTask {
		s.selection.TaskID = row.Task.ID
	}
}

// renderConductorPanel draws the two-pane layout.
func (m *Model) renderConductorPanel() string {
	p := m.conductorPanel

	title := m.theme.SectionTitle.Render("Conductors")

	if p.loadErr != "" {
		body := m.theme.AttentionTag.Render("✗ load failed: "+p.loadErr) + "\n\n" +
			m.theme.MutedText.Render("q / Esc to return")
		return m.renderMCPBox(title + "\n\n" + body)
	}
	if len(p.conductors) == 0 {
		body := m.theme.MutedText.Render(
			"No conductors yet. Create one with `mosaic conductor run plan.toml`.") +
			"\n\n" + m.theme.MutedText.Render("q / Esc to return")
		return m.renderMCPBox(title + "\n\n" + body)
	}

	leftWidth := m.width/2 - 6
	if leftWidth < 24 {
		leftWidth = 24
	}
	left := m.renderConductorTree(p, leftWidth)
	right := m.renderConductorDetail(p)

	leftBox := lipgloss.NewStyle().Width(leftWidth).Render(left)
	rightBox := lipgloss.NewStyle().Width(m.width - leftWidth - 8).Render(right)
	cols := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	footer := m.theme.MutedText.Render(
		"j/k: move   Enter/l: jump   p: pause/resume   r: retry   M: send   +: new   Y: replay   R: refresh   q/Esc: close")
	body := title + "\n\n" + cols + "\n\n" + footer

	if t := m.renderConductorToast(); t != "" {
		body += "\n" + t
	}

	if p.sendModal.active {
		body += "\n\n" + m.renderConductorSendModal()
	}
	if p.planModal.active {
		body += "\n\n" + m.renderConductorPlanModal()
	}

	return m.renderMCPBox(body)
}

// renderConductorPlanModal draws the "+ new conductor" input box.
func (m *Model) renderConductorPlanModal() string {
	pm := m.conductorPanel.planModal
	title := m.theme.SectionTitle.Render("New conductor — plan path")
	input := "> " + pm.path + "_"
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Accent).
		Padding(0, 1).
		Width(m.width / 2).
		Render(input)
	lines := []string{
		title,
		m.theme.MutedText.Render("Type the path to a plan.toml, Enter to run, Esc to cancel."),
		box,
	}
	if pm.err != "" {
		lines = append(lines, m.theme.AttentionTag.Render("✗ "+pm.err))
	}
	return strings.Join(lines, "\n")
}

// renderConductorToast returns the transient footer line when
// a toast is active, or empty otherwise.
func (m *Model) renderConductorToast() string {
	if m.conductorPanel.toast == "" {
		return ""
	}
	if time.Now().After(m.conductorPanel.toastExpires) {
		return ""
	}
	return m.theme.SectionTitle.Render("• " + m.conductorPanel.toast)
}

// renderConductorSendModal draws the inline message input for the
// worker session of the currently selected task.
func (m *Model) renderConductorSendModal() string {
	sm := m.conductorPanel.sendModal
	title := m.theme.SectionTitle.Render("Send to " + sm.targetName)
	input := "> " + sm.body + "_"
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Accent).
		Padding(0, 1).
		Width(m.width / 2).
		Render(input)

	lines := []string{
		title,
		m.theme.MutedText.Render("Enter to send, Esc to cancel."),
		box,
	}
	if sm.err != "" {
		lines = append(lines, m.theme.AttentionTag.Render("✗ "+sm.err))
	}
	return strings.Join(lines, "\n")
}

// renderConductorTree renders the left pane: one conductor per
// group with its tasks listed underneath with ASCII-tree indents.
func (m *Model) renderConductorTree(p conductorPanelState, width int) string {
	var lines []string
	for i, row := range p.rows {
		var text string
		if row.IsTask {
			text = row.Indent + conductorTaskGlyph(row.Task.Status) + " " + row.Task.Name
		} else {
			text = conductorStatusGlyph(row.Conductor.Status) + " " + row.Conductor.Name
		}
		if visibleLen(text) > width-2 {
			text = truncate(text, width-2)
		}
		if i == p.cursor {
			text = m.theme.AttentionTag.Render("▶ " + text)
		} else {
			text = "  " + text
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

// renderConductorDetail shows the right pane — either the conductor
// header summary or the selected task's expanded view.
func (m *Model) renderConductorDetail(p conductorPanelState) string {
	if p.selection.ConductorID == "" {
		return ""
	}
	var conductor store.Conductor
	for _, c := range p.conductors {
		if c.ID == p.selection.ConductorID {
			conductor = c
			break
		}
	}

	if p.selection.TaskID == "" {
		return m.renderConductorHeader(conductor, p.tasks[conductor.ID])
	}
	for _, t := range p.tasks[conductor.ID] {
		if t.ID == p.selection.TaskID {
			return m.renderTaskDetail(conductor, t)
		}
	}
	return ""
}

func (m *Model) renderConductorHeader(c store.Conductor, tasks []store.ConductorTask) string {
	counts := map[string]int{}
	for _, t := range tasks {
		counts[t.Status]++
	}
	lines := []string{
		m.theme.SectionTitle.Render(c.Name),
		"",
		"status:    " + conductorStatusGlyph(c.Status),
		"project:   " + c.ProjectPath,
		"plan:      " + ifEmpty(c.PlanPath.String, "(ad-hoc)"),
		"created:   " + c.CreatedAt.Format("2006-01-02 15:04:05"),
		"",
		m.theme.SectionTitle.Render("Tasks"),
		fmt.Sprintf("  total:   %d", len(tasks)),
		fmt.Sprintf("  running: %d", counts[string(store.TaskRunning)]),
		fmt.Sprintf("  ready:   %d", counts[string(store.TaskReady)]),
		fmt.Sprintf("  pending: %d", counts[string(store.TaskPending)]),
		fmt.Sprintf("  done:    %d", counts[string(store.TaskDone)]),
		fmt.Sprintf("  failed:  %d", counts[string(store.TaskFailed)]),
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderTaskDetail(c store.Conductor, t store.ConductorTask) string {
	deps := strings.Join(t.Dependencies(), ", ")
	if deps == "" {
		deps = "(none)"
	}
	lines := []string{
		m.theme.SectionTitle.Render(c.Name + " / " + t.Name),
		"",
		"status:    " + conductorTaskGlyph(t.Status) + " " + t.Status,
		"depends:   " + deps,
		"assigned:  " + ifEmpty(t.AssignedSessionID.String, "(not dispatched)"),
	}
	if t.StartedAt.Valid {
		lines = append(lines, "started:   "+t.StartedAt.Time.Format("15:04:05"))
	}
	if t.CompletedAt.Valid {
		lines = append(lines, "completed: "+t.CompletedAt.Time.Format("15:04:05"))
	}
	if t.Prompt != "" {
		lines = append(lines, "", m.theme.SectionTitle.Render("Prompt"), wrapText(t.Prompt, 60))
	}
	if t.Result.Valid && t.Result.String != "" {
		lines = append(lines, "", m.theme.SectionTitle.Render("Result"), wrapText(t.Result.String, 60))
	}
	return strings.Join(lines, "\n")
}

// conductorStatusGlyph maps a conductor lifecycle status to a
// single-character glyph. The colour is picked up by the row-level
// highlight so this function stays theme-agnostic.
func conductorStatusGlyph(status string) string {
	switch status {
	case string(store.ConductorRunning):
		return "⟳"
	case string(store.ConductorDone):
		return "✓"
	case string(store.ConductorFailed):
		return "✗"
	case string(store.ConductorPaused):
		return "⏸"
	default:
		return "·"
	}
}

// conductorTaskGlyph maps a task lifecycle status to a glyph. Kept
// plain text so the colouring can happen at the row level (cursor
// highlight already covers the selection case).
func conductorTaskGlyph(status string) string {
	switch status {
	case string(store.TaskRunning):
		return "⟳"
	case string(store.TaskDone):
		return "✓"
	case string(store.TaskFailed):
		return "✗"
	case string(store.TaskReady):
		return "◎"
	default:
		return "·"
	}
}

// setConductorToast briefly shows a confirmation line in the
// Conductor Panel footer. Used by pause/resume/retry/send to
// acknowledge that the action was accepted.
func (m *Model) setConductorToast(text string) {
	m.conductorPanel.toast = text
	m.conductorPanel.toastExpires = time.Now().Add(3 * time.Second)
}

// jumpToConductorWorker closes the panel and moves the TOP cursor
// to the session a task is currently assigned to. Acts as a no-op
// when the cursor is on a conductor header (not a task) or on a
// task that has not been dispatched yet.
func (m *Model) jumpToConductorWorker() (tea.Model, tea.Cmd) {
	row := m.conductorPanelCurrentRow()
	if row == nil || !row.IsTask {
		return m, nil
	}
	if !row.Task.AssignedSessionID.Valid || row.Task.AssignedSessionID.String == "" {
		m.setConductorToast("task has no worker session yet")
		return m, nil
	}
	targetID := row.Task.AssignedSessionID.String

	// Find the matching row in the TOP list.
	for i, r := range m.rows {
		if !r.IsGroup && !r.IsSubagent && r.Session.ID == targetID {
			m.cursor = i
			m.closeConductorPanel()
			return m, nil
		}
	}
	// The worker is no longer in the TOP list (killed?) — stay
	// on the conductor panel and tell the user.
	m.setConductorToast("worker session not in the session list")
	return m, nil
}

// toggleConductorPause flips the selected conductor's status
// between paused and running. The underlying runner does not
// watch the status column yet (pause is advisory), but the CLI
// `resume` command and the TUI footer both reflect the change.
func (m *Model) toggleConductorPause() tea.Cmd {
	row := m.conductorPanelCurrentRow()
	if row == nil {
		return nil
	}
	c := row.Conductor
	next := store.ConductorPaused
	if c.Status == string(store.ConductorPaused) {
		next = store.ConductorRunning
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.store.Conductors().SetStatus(ctx, c.ID, next); err != nil {
			return conductorPanelToastMsg{text: "pause failed: " + err.Error()}
		}
		return conductorPanelRefreshMsg{toast: "conductor " + c.Name + " → " + string(next)}
	}
}

// retryConductorTask flips a failed task back to pending. The
// next time the runner (or a fresh `mosaic conductor run`) is
// invoked, the scheduler will pick the task up again. Tasks that
// are not in the failed state are ignored.
func (m *Model) retryConductorTask() tea.Cmd {
	row := m.conductorPanelCurrentRow()
	if row == nil || !row.IsTask {
		return nil
	}
	t := row.Task
	if t.Status != string(store.TaskFailed) {
		m.setConductorToast("only failed tasks can be retried")
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.store.Conductors().SetTaskStatus(ctx, t.ID, store.TaskPending); err != nil {
			return conductorPanelToastMsg{text: "retry failed: " + err.Error()}
		}
		return conductorPanelRefreshMsg{toast: "task " + t.Name + " → pending"}
	}
}

// openConductorSendModal pops the inline message input for the
// worker session assigned to the selected task.
func (m *Model) openConductorSendModal() {
	row := m.conductorPanelCurrentRow()
	if row == nil || !row.IsTask {
		return
	}
	if !row.Task.AssignedSessionID.Valid || row.Task.AssignedSessionID.String == "" {
		m.setConductorToast("task has no worker session yet")
		return
	}
	// Resolve the session name for display.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	sess, err := m.store.Sessions().GetByID(ctx, row.Task.AssignedSessionID.String)
	if err != nil {
		m.setConductorToast("worker lookup failed: " + err.Error())
		return
	}
	m.conductorPanel.sendModal = conductorSendModal{
		active:      true,
		targetID:    sess.ID,
		targetName:  sess.Name,
		conductorID: row.ConductorID,
	}
}

// submitConductorSendModal posts the typed message via the
// messaging service. Empty bodies are rejected inline so the user
// can fix them without losing what they wrote.
func (m *Model) submitConductorSendModal() tea.Cmd {
	sm := &m.conductorPanel.sendModal
	if strings.TrimSpace(sm.body) == "" {
		sm.err = "message body is required"
		return nil
	}
	target := sm.targetName
	conductorID := sm.conductorID
	body := sm.body
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		svc := service.NewMessagingService(m.store, nil, m.bus, 0)
		_, err := svc.Post(ctx, service.PostInput{
			ToSession:   target,
			Kind:        "note",
			Body:        body,
			ConductorID: conductorID,
		})
		if err != nil {
			return conductorPanelToastMsg{text: "send failed: " + err.Error()}
		}
		return conductorPanelSendSuccessMsg{target: target}
	}
}

// conductorPanelCurrentRow is a safe accessor that returns the
// row under the cursor, or nil when the panel is empty.
func (m *Model) conductorPanelCurrentRow() *conductorRow {
	p := &m.conductorPanel
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		return nil
	}
	return &p.rows[p.cursor]
}

// conductorPanelRefreshMsg is the Cmd return type for actions
// that mutate store state and need a re-fetch + toast.
type conductorPanelRefreshMsg struct {
	toast string
}

// conductorPanelToastMsg is the lightweight "just show a toast"
// variant for actions that failed or did not touch the store.
type conductorPanelToastMsg struct {
	text string
}

// conductorPanelSendSuccessMsg is the happy-path after submitting
// a send-message modal.
type conductorPanelSendSuccessMsg struct {
	target string
}
