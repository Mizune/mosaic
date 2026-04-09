package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/conductor"
)

// conductorWizardStep identifies which page of the conductor wizard is active.
type conductorWizardStep int

const (
	cwStepSource  conductorWizardStep = iota // template / scratch / file
	cwStepMeta                               // name, project, description
	cwStepTasks                              // task list editor
	cwStepReview                             // DAG preview + confirm
)

type conductorWizardTask struct {
	Name, Prompt, Tool, Branch string
	DependsOn                  []string
}

type conductorSourceChoice struct {
	Label, Description string
	IsFile             bool
	Template           *conductorTemplate
}

type conductorTemplate struct {
	Name, Description string
	Tasks             []conductorWizardTask
}

type conductorWizardState struct {
	step          conductorWizardStep
	cursor        int
	sourceChoices []conductorSourceChoice
	// Meta
	name, project, description string
	metaField                  int // 0=name, 1=project, 2=description
	projectTyping              bool
	projectRegistry            []registeredProject
	projectCandidates          []string
	// Tasks
	tasks           []conductorWizardTask
	taskCursor      int
	taskFieldCursor int  // 0=name, 1=tool, 2=prompt, 3=branch, 4=depends_on
	editingField    bool
	editingDeps     bool
	// Review
	dagPreview, validationErr string
	// Shared
	confirmation, filePath string
	fileMode               bool
}

type conductorWizardCompleteMsg struct {
	name string
	err  error
}

var cwToolChoices = []string{"claude", "codex", "gemini", "shell"}

// --- Templates ---

func defaultSourceChoices() []conductorSourceChoice {
	choices := []conductorSourceChoice{
		{Label: "New from scratch", Description: "blank plan with one placeholder task"},
	}
	for _, t := range builtinConductorTemplates() {
		tpl := t
		choices = append(choices, conductorSourceChoice{
			Label: tpl.Name, Description: tpl.Description, Template: &tpl,
		})
	}
	choices = append(choices, conductorSourceChoice{
		Label: "Load from file", Description: "import an existing plan.toml", IsFile: true,
	})
	return choices
}

// --- Open ---

func (m *Model) openConductorWizard() {
	project := "."
	if cwd, err := os.Getwd(); err == nil {
		project = cwd
	}
	reg := m.buildProjectRegistry()
	if len(reg) > 0 {
		project = reg[0].path
	}
	m.conductorWizard = conductorWizardState{
		step: cwStepSource, sourceChoices: defaultSourceChoices(),
		project: project, projectTyping: len(reg) == 0, projectRegistry: reg,
		tasks: []conductorWizardTask{{Name: "task-1", Prompt: "Describe what this task should do.", Tool: "claude"}},
	}
	m.screen = ScreenConductorWizard
}

// --- Update ---

func (m *Model) updateConductorWizard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := &m.conductorWizard
	key := msg.String()
	if key == "ctrl+[" {
		key = "esc"
	}
	if w.fileMode {
		return m.updateCWFileInput(key)
	}
	if key == "esc" {
		m.screen = ScreenConductor
		return m, nil
	}
	if key == "shift+tab" && w.step > cwStepSource {
		w.editingField, w.editingDeps = false, false
		w.step--
		w.confirmation, w.validationErr = "", ""
		return m, nil
	}
	switch w.step {
	case cwStepSource:
		return m.updateCWSource(key)
	case cwStepMeta:
		return m.updateCWMeta(key)
	case cwStepTasks:
		return m.updateCWTasks(key)
	case cwStepReview:
		if key == "enter" {
			return m.advanceConductorWizard()
		}
	}
	return m, nil
}

func (m *Model) updateCWFileInput(key string) (tea.Model, tea.Cmd) {
	w := &m.conductorWizard
	switch key {
	case "esc":
		w.fileMode, w.filePath = false, ""
	case "enter":
		return m.loadConductorPlanFile()
	case "backspace":
		if n := len(w.filePath); n > 0 {
			w.filePath = w.filePath[:n-1]
		}
	case "tab":
		w.filePath, _ = completePath(w.filePath)
	default:
		if len(key) == 1 {
			w.filePath += key
		}
	}
	return m, nil
}

func (m *Model) loadConductorPlanFile() (tea.Model, tea.Cmd) {
	w := &m.conductorWizard
	path := strings.TrimSpace(w.filePath)
	if path == "" {
		w.confirmation = "file path is required"
		return m, nil
	}
	if !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	plan, err := conductor.LoadPlan(path)
	if err != nil {
		w.confirmation = "load failed: " + err.Error()
		return m, nil
	}
	w.name, w.description = plan.Conductor.Name, plan.Conductor.Description
	if plan.Conductor.Project != "" {
		w.project = plan.Conductor.Project
	}
	w.tasks = planTasksToWizard(plan.Tasks)
	w.fileMode, w.filePath = false, path
	w.step, w.confirmation = cwStepMeta, ""
	return m, nil
}

func planTasksToWizard(specs []conductor.TaskSpec) []conductorWizardTask {
	out := make([]conductorWizardTask, 0, len(specs))
	for _, s := range specs {
		tool := s.Tool
		if tool == "" {
			tool = "claude"
		}
		deps := make([]string, len(s.DependsOn))
		copy(deps, s.DependsOn)
		out = append(out, conductorWizardTask{Name: s.Name, Prompt: s.Prompt, Tool: tool, Branch: s.Branch, DependsOn: deps})
	}
	return out
}

func (m *Model) updateCWSource(key string) (tea.Model, tea.Cmd) {
	w := &m.conductorWizard
	switch key {
	case "j", "down":
		if w.cursor < len(w.sourceChoices)-1 {
			w.cursor++
		}
	case "k", "up":
		if w.cursor > 0 {
			w.cursor--
		}
	case "enter":
		if w.sourceChoices[w.cursor].IsFile {
			w.fileMode, w.filePath = true, ""
			return m, nil
		}
		return m.advanceConductorWizard()
	}
	return m, nil
}

func (m *Model) updateCWMeta(key string) (tea.Model, tea.Cmd) {
	w := &m.conductorWizard
	if w.metaField == 1 && w.projectTyping && key == "tab" {
		completed, cands := completePath(w.project)
		w.project = completed
		if len(cands) > 1 {
			w.projectCandidates = cands
		} else {
			w.projectCandidates = nil
		}
		return m, nil
	}
	switch key {
	case "tab", "enter":
		if w.metaField < 2 {
			w.metaField++
			if w.metaField == 1 {
				w.projectTyping = len(w.projectRegistry) == 0
				w.cursor = 0
			}
			return m, nil
		}
		return m.advanceConductorWizard()
	case "ctrl+u":
		switch w.metaField {
		case 0:
			w.name = ""
		case 1:
			w.project, w.projectTyping, w.projectCandidates = "", true, nil
		case 2:
			w.description = ""
		}
		return m, nil
	case "backspace":
		switch w.metaField {
		case 0:
			w.name = trimLast(w.name)
		case 1:
			w.projectTyping = true
			w.project = trimLast(w.project)
			w.projectCandidates = nil
		case 2:
			w.description = trimLast(w.description)
		}
		return m, nil
	}
	// Project pick-mode navigation.
	if w.metaField == 1 && !w.projectTyping {
		switch key {
		case "j", "down":
			if w.cursor < len(w.projectRegistry)-1 {
				w.cursor++
			}
			w.project = w.projectRegistry[w.cursor].path
		case "k", "up":
			if w.cursor > 0 {
				w.cursor--
			}
			w.project = w.projectRegistry[w.cursor].path
		case "i":
			if w.cursor >= 0 && w.cursor < len(w.projectRegistry) {
				w.project = w.projectRegistry[w.cursor].path
			}
			w.projectTyping = true
		default:
			if len(key) == 1 {
				w.project, w.projectTyping, w.projectCandidates = key, true, nil
			}
		}
		return m, nil
	}
	if len(key) == 1 {
		switch w.metaField {
		case 0:
			w.name += key
		case 1:
			w.project += key
			w.projectCandidates = nil
		case 2:
			w.description += key
		}
	}
	return m, nil
}

func (m *Model) updateCWTasks(key string) (tea.Model, tea.Cmd) {
	w := &m.conductorWizard
	if w.editingDeps {
		return m.updateCWTasksDeps(key)
	}
	if w.editingField {
		return m.updateCWTasksFieldEdit(key)
	}
	switch key {
	case "j", "down":
		if w.taskCursor < len(w.tasks)-1 {
			w.taskCursor++
		}
	case "k", "up":
		if w.taskCursor > 0 {
			w.taskCursor--
		}
	case "a":
		w.tasks = append(w.tasks, conductorWizardTask{Name: fmt.Sprintf("task-%d", len(w.tasks)+1), Tool: "claude"})
		w.taskCursor = len(w.tasks) - 1
	case "d":
		if len(w.tasks) > 1 {
			t := make([]conductorWizardTask, 0, len(w.tasks)-1)
			t = append(t, w.tasks[:w.taskCursor]...)
			t = append(t, w.tasks[w.taskCursor+1:]...)
			w.tasks = t
			if w.taskCursor >= len(w.tasks) {
				w.taskCursor = len(w.tasks) - 1
			}
		}
	case "J":
		if w.taskCursor < len(w.tasks)-1 {
			t := make([]conductorWizardTask, len(w.tasks))
			copy(t, w.tasks)
			t[w.taskCursor], t[w.taskCursor+1] = t[w.taskCursor+1], t[w.taskCursor]
			w.tasks = t
			w.taskCursor++
		}
	case "K":
		if w.taskCursor > 0 {
			t := make([]conductorWizardTask, len(w.tasks))
			copy(t, w.tasks)
			t[w.taskCursor], t[w.taskCursor-1] = t[w.taskCursor-1], t[w.taskCursor]
			w.tasks = t
			w.taskCursor--
		}
	case "enter":
		w.editingField, w.taskFieldCursor = true, 0
	case "tab":
		return m.advanceConductorWizard()
	}
	return m, nil
}

func (m *Model) updateCWTasksFieldEdit(key string) (tea.Model, tea.Cmd) {
	w := &m.conductorWizard
	task := &w.tasks[w.taskCursor]
	switch key {
	case "esc":
		w.editingField = false
		return m, nil
	case "tab":
		w.taskFieldCursor = (w.taskFieldCursor + 1) % 5
		if w.taskFieldCursor == 4 {
			w.editingField, w.editingDeps, w.cursor = false, true, 0
		}
		return m, nil
	}
	switch w.taskFieldCursor {
	case 0: // name
		task.Name = cwEditText(task.Name, key)
	case 1: // tool (j/k pick)
		idx := cwToolIndex(task.Tool)
		if (key == "j" || key == "down") && idx < len(cwToolChoices)-1 {
			task.Tool = cwToolChoices[idx+1]
		} else if (key == "k" || key == "up") && idx > 0 {
			task.Tool = cwToolChoices[idx-1]
		}
	case 2: // prompt
		task.Prompt = cwEditText(task.Prompt, key)
	case 3: // branch
		task.Branch = cwEditText(task.Branch, key)
	}
	return m, nil
}

func (m *Model) updateCWTasksDeps(key string) (tea.Model, tea.Cmd) {
	w := &m.conductorWizard
	task := &w.tasks[w.taskCursor]
	others := cwOtherNames(w.tasks, w.taskCursor)
	switch key {
	case "esc":
		w.editingDeps = false
	case "j", "down":
		if w.cursor < len(others)-1 {
			w.cursor++
		}
	case "k", "up":
		if w.cursor > 0 {
			w.cursor--
		}
	case " ":
		if w.cursor >= 0 && w.cursor < len(others) {
			dep := others[w.cursor]
			if cwContains(task.DependsOn, dep) {
				task.DependsOn = cwRemove(task.DependsOn, dep)
			} else {
				task.DependsOn = append(append([]string{}, task.DependsOn...), dep)
			}
		}
	case "tab":
		w.editingDeps, w.editingField, w.taskFieldCursor = false, true, 0
	}
	return m, nil
}

// --- Advance ---

func (m *Model) advanceConductorWizard() (tea.Model, tea.Cmd) {
	w := &m.conductorWizard
	switch w.step {
	case cwStepSource:
		if tpl := w.sourceChoices[w.cursor].Template; tpl != nil {
			w.name, w.description = tpl.Name, tpl.Description
			w.tasks = make([]conductorWizardTask, len(tpl.Tasks))
			copy(w.tasks, tpl.Tasks)
		}
		w.step, w.cursor, w.metaField, w.confirmation = cwStepMeta, 0, 0, ""
	case cwStepMeta:
		if strings.TrimSpace(w.name) == "" {
			w.confirmation = "name cannot be empty"
			return m, nil
		}
		abs, err := resolveProjectPath(w.project)
		if err != nil {
			w.confirmation = "invalid path: " + err.Error()
			return m, nil
		}
		w.project = abs
		if info, e := os.Stat(abs); e != nil || !info.IsDir() {
			w.confirmation = "directory does not exist: " + abs
			return m, nil
		}
		w.confirmation = ""
		w.step, w.taskCursor, w.editingField = cwStepTasks, 0, false
	case cwStepTasks:
		if len(w.tasks) == 0 {
			w.confirmation = "at least one task is required"
			return m, nil
		}
		for _, t := range w.tasks {
			if strings.TrimSpace(t.Name) == "" {
				w.confirmation = "every task needs a name"
				return m, nil
			}
		}
		plan := m.assemblePlan()
		if err := plan.Validate(); err != nil {
			w.validationErr = err.Error()
			w.dagPreview = ""
		} else {
			w.dagPreview = cwRenderDAG(plan)
			w.validationErr = ""
		}
		w.step, w.confirmation = cwStepReview, ""
	case cwStepReview:
		return m, m.runConductorWizardPlan()
	}
	return m, nil
}

// --- Assemble & Run ---

func (m *Model) assemblePlan() conductor.Plan {
	w := m.conductorWizard
	tasks := make([]conductor.TaskSpec, 0, len(w.tasks))
	for _, t := range w.tasks {
		tool := t.Tool
		if tool == "" {
			tool = "claude"
		}
		deps := make([]string, len(t.DependsOn))
		copy(deps, t.DependsOn)
		tasks = append(tasks, conductor.TaskSpec{
			Name: t.Name, DependsOn: deps, Prompt: t.Prompt, Tool: tool, Branch: t.Branch,
		})
	}
	return conductor.Plan{
		Conductor: conductor.ConductorSection{Name: w.name, Project: w.project, Description: w.description},
		Tasks:     tasks,
	}
}

func (m *Model) runConductorWizardPlan() tea.Cmd {
	plan := m.assemblePlan()
	if err := plan.Validate(); err != nil {
		return func() tea.Msg { return conductorWizardCompleteMsg{name: plan.Conductor.Name, err: err} }
	}
	project := plan.Conductor.Project
	if project == "" {
		if cwd, err := os.Getwd(); err == nil {
			project = cwd
		}
	}
	if abs, err := filepath.Abs(project); err == nil {
		project = abs
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		runner := conductor.NewRunner(m.store, m.sessions)
		if err := runner.Prepare(ctx, plan, project, ""); err != nil {
			return conductorWizardCompleteMsg{name: plan.Conductor.Name, err: err}
		}
		go func() {
			bg, bgCancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer bgCancel()
			_ = runner.Run(bg, conductor.SessionDispatch(m.sessions))
		}()
		return conductorWizardCompleteMsg{name: plan.Conductor.Name}
	}
}

func cwRenderDAG(plan conductor.Plan) string {
	var b strings.Builder
	for _, t := range plan.Tasks {
		deps := "(root)"
		if len(t.DependsOn) > 0 {
			deps = "depends on: " + strings.Join(t.DependsOn, ", ")
		}
		tool := t.Tool
		if tool == "" {
			tool = "claude"
		}
		fmt.Fprintf(&b, "  [%s] %s  (%s)\n", tool, t.Name, deps)
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- Render ---

func (m *Model) renderConductorWizard() string {
	w := m.conductorWizard
	var lines []string
	lines = append(lines, m.theme.Header.Render("New Conductor \u2014 "+cwStepTitle(w.step)), "")

	switch w.step {
	case cwStepSource:
		lines = append(lines, m.renderCWSource(w)...)
	case cwStepMeta:
		lines = append(lines, m.renderCWMeta(w)...)
	case cwStepTasks:
		lines = append(lines, m.renderCWTasks(w)...)
	case cwStepReview:
		lines = append(lines, m.renderCWReview(w)...)
	}
	if w.confirmation != "" {
		lines = append(lines, "", m.theme.AttentionTag.Render("\u26a0 "+w.confirmation))
	}
	body := strings.Join(lines, "\n")
	width := m.width * 3 / 4
	if width < 60 {
		width = 60
	}
	box := m.theme.BorderBox.Width(width).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) renderCWSource(w conductorWizardState) []string {
	lines := []string{m.theme.MutedText.Render("choose a starting point for the conductor plan"), ""}
	if w.fileMode {
		return append(lines,
			"Path: "+m.theme.AttentionTag.Render("> ")+w.filePath+"_", "",
			m.theme.MutedText.Render("[Enter] load  [Tab] complete  [Esc] cancel"))
	}
	for i, c := range w.sourceChoices {
		mk := "  "
		if i == w.cursor {
			mk = m.theme.AttentionTag.Render("\u25b6 ")
		}
		line := mk + c.Label
		if c.Description != "" {
			line += "  " + m.theme.MutedText.Render(c.Description)
		}
		lines = append(lines, line)
	}
	return append(lines, "", m.theme.MutedText.Render("[j/k] move  [Enter] select  [Esc] cancel"))
}

func (m *Model) renderCWMeta(w conductorWizardState) []string {
	lines := []string{m.theme.MutedText.Render("conductor metadata"), ""}
	// Name
	lines = append(lines, cwFieldLine(m, w.metaField == 0, "Name:        ", w.name, w.metaField == 0))
	// Project
	projSuffix := ""
	if w.metaField == 1 {
		if w.projectTyping {
			projSuffix = " " + m.theme.AttentionTag.Render("[type]")
		} else {
			projSuffix = " " + m.theme.SectionTitle.Render("[pick]")
		}
	}
	lines = append(lines, cwFieldLine(m, w.metaField == 1, "Project:     ", w.project, w.metaField == 1 && w.projectTyping)+projSuffix)
	if w.metaField == 1 && !w.projectTyping && len(w.projectRegistry) > 0 {
		lines = append(lines, "", m.theme.SectionTitle.Render("registered projects"),
			m.renderRegistryChoices(w.projectRegistry, w.cursor, true))
	}
	if w.metaField == 1 && w.projectTyping && len(w.projectCandidates) > 1 {
		lines = append(lines, "", m.theme.SectionTitle.Render("tab candidates"),
			m.renderPathCandidates(w.projectCandidates, 8))
	}
	// Description
	lines = append(lines, cwFieldLine(m, w.metaField == 2, "Description: ", w.description, w.metaField == 2))
	return append(lines, "", m.theme.MutedText.Render("[Tab/Enter] next field  [Shift+Tab] back  [Ctrl+U] clear  [Esc] cancel"))
}

func (m *Model) renderCWTasks(w conductorWizardState) []string {
	lines := []string{m.theme.MutedText.Render("define the tasks that make up this conductor plan"), ""}
	for i, t := range w.tasks {
		mk := "  "
		if i == w.taskCursor {
			mk = m.theme.AttentionTag.Render("\u25b6 ")
		}
		tool := t.Tool
		if tool == "" {
			tool = "claude"
		}
		deps := ""
		if len(t.DependsOn) > 0 {
			deps = " \u2192 " + strings.Join(t.DependsOn, ", ")
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s%s", mk, tool, t.Name, deps))
	}
	if w.taskCursor >= 0 && w.taskCursor < len(w.tasks) {
		lines = append(lines, "", m.theme.SectionTitle.Render("Task detail"))
		lines = append(lines, m.renderCWTaskFields(w)...)
	}
	if w.editingDeps {
		others := cwOtherNames(w.tasks, w.taskCursor)
		task := w.tasks[w.taskCursor]
		lines = append(lines, "", m.theme.SectionTitle.Render("Dependencies (Space to toggle)"))
		for i, name := range others {
			ck := "[ ]"
			if cwContains(task.DependsOn, name) {
				ck = "[x]"
			}
			cur := "  "
			if i == w.cursor {
				cur = m.theme.AttentionTag.Render("\u25b6 ")
			}
			lines = append(lines, cur+ck+" "+name)
		}
		return append(lines, "", m.theme.MutedText.Render("[j/k] move  [Space] toggle  [Tab] next field  [Esc] done"))
	}
	if w.editingField {
		return append(lines, "", m.theme.MutedText.Render("[Tab] next field  [Esc] done editing"))
	}
	return append(lines, "", m.theme.MutedText.Render("[j/k] select  [Enter] edit  [a] add  [d] delete  [J/K] reorder  [Tab] next step  [Shift+Tab] back"))
}

func (m *Model) renderCWTaskFields(w conductorWizardState) []string {
	t := w.tasks[w.taskCursor]
	fields := []struct{ label, value string }{
		{"name", t.Name}, {"tool", t.Tool}, {"prompt", t.Prompt},
		{"branch", t.Branch}, {"depends_on", strings.Join(t.DependsOn, ", ")},
	}
	var lines []string
	for i, f := range fields {
		pfx, sfx := "    ", ""
		if w.editingField && w.taskFieldCursor == i {
			pfx, sfx = "  "+m.theme.AttentionTag.Render("> "), "_"
		} else if w.editingDeps && i == 4 {
			pfx = "  " + m.theme.AttentionTag.Render("> ")
		}
		val := f.value
		if val == "" {
			val = m.theme.MutedText.Render("(empty)")
		}
		lines = append(lines, pfx+f.label+": "+val+sfx)
	}
	return lines
}

func (m *Model) renderCWReview(w conductorWizardState) []string {
	lines := []string{
		m.theme.MutedText.Render("review your conductor plan before running"), "",
		m.theme.SectionTitle.Render("Plan: " + w.name), "  project: " + w.project,
	}
	if w.description != "" {
		lines = append(lines, "  description: "+w.description)
	}
	lines = append(lines, "", m.theme.SectionTitle.Render("DAG Preview"))
	if w.dagPreview != "" {
		lines = append(lines, w.dagPreview)
	}
	if w.validationErr != "" {
		lines = append(lines, "", m.theme.AttentionTag.Render("\u26a0 "+w.validationErr))
	}
	return append(lines, "", m.theme.MutedText.Render("[Enter] run plan  [Shift+Tab] back  [Esc] cancel"))
}

func cwStepTitle(s conductorWizardStep) string {
	switch s {
	case cwStepSource:
		return "Source (1/4)"
	case cwStepMeta:
		return "Metadata (2/4)"
	case cwStepTasks:
		return "Tasks (3/4)"
	case cwStepReview:
		return "Review (4/4)"
	}
	return ""
}

// --- Helpers ---

func cwFieldLine(m *Model, active bool, label, value string, showCursor bool) string {
	pfx := "  "
	if active {
		pfx = m.theme.AttentionTag.Render("> ")
	}
	cur := ""
	if showCursor {
		cur = "_"
	}
	return pfx + label + value + cur
}

func cwEditText(s, key string) string {
	switch key {
	case "backspace":
		return trimLast(s)
	case "ctrl+u":
		return ""
	default:
		if len(key) == 1 {
			return s + key
		}
	}
	return s
}

func cwToolIndex(tool string) int {
	for i, t := range cwToolChoices {
		if t == tool {
			return i
		}
	}
	return 0
}

func cwOtherNames(tasks []conductorWizardTask, exclude int) []string {
	out := make([]string, 0, len(tasks)-1)
	for i, t := range tasks {
		if i != exclude {
			out = append(out, t.Name)
		}
	}
	return out
}

func cwContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func cwRemove(ss []string, s string) []string {
	out := make([]string, 0, len(ss))
	for _, v := range ss {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

func trimLast(s string) string {
	if len(s) > 0 {
		return s[:len(s)-1]
	}
	return s
}
