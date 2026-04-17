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

	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
)

// wizardStep identifies which page of the new-session wizard the
// model is currently rendering. The wizard has four pages and a
// confirmation phase that immediately calls service.Create.
type wizardStep int

const (
	stepName wizardStep = iota
	stepProject
	stepTool
	stepWorktree
	// stepWorktreeSetup is only visited when wantWorktree is
	// true. It lets the user pick an existing recipe from
	// config.Worktree.Setup or open the recipe wizard via the
	// sentinel "+ create new recipe" entry.
	stepWorktreeSetup
	stepDone
)

// wizardState is the per-modal state for the new-session wizard. It
// is owned by the parent Model so the wizard can be entered, exited,
// and re-entered without leaking goroutines.
//
// The Project step is a hybrid: the user can type a path directly
// into `project`, or pick one of the `projectRegistry` entries that
// came from [[project]] in config.toml (or were observed via existing
// sessions). Typing remains available so fresh mosaic installs never
// hit a dead end. Tab triggers shell-style path completion against
// the real filesystem; the matching candidates are cached in
// `projectCandidates` so the render loop can show them to the user.
type wizardState struct {
	step              wizardStep
	name              string
	project           string              // current path value
	projectTyping     bool                // true => keystrokes edit `project`
	projectRegistry   []registeredProject // named projects from config + observed
	projectCandidates []string            // shown when Tab finds >1 match
	tool              string
	wantWorktree      bool
	branch            string
	setup             string
	cursor            int
	toolChoice        []string
	setupChoice       []setupChoice
	confirmation      string

	// Claude-specific launch mode toggles. These override the
	// global [mosaic.tools.claude] config for THIS session only.
	// The wizard shows them inline on the Tool step when
	// "claude" is selected.
	claudeAutoMode          bool
	claudeBypassPermissions bool
	claudeContinue          bool
}

// registeredProject is the wizard-facing view of one project entry.
// We precompute the resolved absolute path so the render and the
// advance handlers never have to re-resolve it.
type registeredProject struct {
	name string
	path string
}

// newWizardState constructs the wizard's initial state seeded from
// the registered project list, the model's current sessions, and
// the current working directory.
func (m *Model) newWizardState() wizardState {
	project := "."
	if cwd, err := os.Getwd(); err == nil {
		project = cwd
	}
	registry := m.buildProjectRegistry()

	// If the registry is non-empty, default the Project field to the
	// first registered path so the user can accept it with one Enter.
	if len(registry) > 0 {
		project = registry[0].path
	}

	defaultTool := m.config.Mosaic.DefaultTool
	if defaultTool == "" {
		defaultTool = "claude"
	}
	defaultSetup := m.config.Worktree.DefaultSetup
	if defaultSetup == "" {
		defaultSetup = "default"
	}

	return wizardState{
		step:            stepName,
		project:         project,
		projectTyping:   len(registry) == 0, // no registry → straight to typing
		projectRegistry: registry,
		tool:            defaultTool,
		wantWorktree:    m.config.Worktree.AutoWorktree,
		setup:           defaultSetup,
		toolChoice:      []string{"claude", "codex", "gemini", "shell"},
		setupChoice:     buildSetupChoices(m.config),
	}
}

// buildProjectRegistry merges [[project]] entries from config.toml
// with any projects observed through existing sessions. Registered
// entries come first; observed entries fill in as a fallback so
// users who haven't edited their config yet still see something
// useful.
func (m *Model) buildProjectRegistry() []registeredProject {
	seen := map[string]struct{}{}
	var out []registeredProject

	for _, p := range m.config.Projects {
		if p.Name == "" || p.Path == "" {
			continue
		}
		path, err := p.ResolvedPath()
		if err != nil || path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, registeredProject{name: p.Name, path: path})
	}

	for _, r := range m.rows {
		if r.IsGroup || r.IsSubagent || r.Session.ProjectPath == "" {
			continue
		}
		if _, ok := seen[r.Session.ProjectPath]; ok {
			continue
		}
		seen[r.Session.ProjectPath] = struct{}{}
		out = append(out, registeredProject{
			name: baseName(r.Session.ProjectPath),
			path: r.Session.ProjectPath,
		})
	}
	return out
}

// startWizard pops the wizard modal.
func (m *Model) startWizard() {
	m.wizard = m.newWizardState()
	m.screen = ScreenNewSession
}

// updateWizard handles key input while the wizard modal is active.
// The Project step has two sub-modes that change what keys mean:
//
//   - Pick mode: the cursor is on the registered-projects list.
//     j/k (or arrows) highlight entries, Enter accepts the
//     highlighted path and advances. Pressing `i` (vim-style) or
//     any real text key drops into Type mode. Pick mode is the
//     default when the registry is non-empty so muscle-memory
//     vim users can navigate without first typing anything.
//   - Type mode: the path field accepts every keystroke. Tab
//     completes against the filesystem, Esc drops back to Pick
//     mode (or cancels the wizard if the registry is empty and
//     there's nowhere to retreat to).
func (m *Model) updateWizard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := &m.wizard
	key := msg.String()

	// ---- Global keys that always take precedence ---------------
	switch key {
	case "ctrl+[":
		// Same as Esc on most terminals; handled below.
		key = "esc"
	}

	// Esc in Type mode drops back to Pick mode instead of closing
	// the wizard, so the user gets one level of "undo" before
	// losing the whole wizard.
	if key == "esc" && w.step == stepProject && w.projectTyping && len(w.projectRegistry) > 0 {
		w.projectTyping = false
		w.projectCandidates = nil
		if w.cursor < 0 || w.cursor >= len(w.projectRegistry) {
			w.cursor = 0
		}
		// Snap the path field back to the highlighted entry so
		// the user sees a coherent state.
		w.project = w.projectRegistry[w.cursor].path
		return m, nil
	}
	if key == "esc" {
		m.screen = ScreenTop
		return m, nil
	}

	switch key {
	case "tab":
		// In Project Type mode, Tab does filesystem completion;
		// in every other text step it advances the wizard. In
		// Project Pick mode, Tab advances (the user has already
		// highlighted an entry).
		if w.step == stepProject && w.projectTyping {
			completed, candidates := completePath(w.project)
			w.project = completed
			if len(candidates) > 1 {
				w.projectCandidates = candidates
			} else {
				w.projectCandidates = nil
			}
			return m, nil
		}
		return m.advanceWizard()

	case "enter":
		// In Project Pick mode, Enter first copies the highlighted
		// registry entry into w.project so the advance handler
		// validates the right path.
		if w.step == stepProject && !w.projectTyping {
			if w.cursor >= 0 && w.cursor < len(w.projectRegistry) {
				w.project = w.projectRegistry[w.cursor].path
			}
		}
		return m.advanceWizard()

	case "shift+tab":
		if w.step > 0 {
			w.step--
			w.cursor = 0
		}
		return m, nil

	case "up":
		if w.step == stepProject && w.projectTyping {
			// Up in Type mode moves the cursor through the suggested
			// candidate list (or does nothing if empty).
			if w.cursor > 0 {
				w.cursor--
			}
			return m, nil
		}
		if w.cursor > 0 {
			w.cursor--
		}
		return m, nil

	case "down":
		if w.step == stepProject && w.projectTyping {
			w.cursor = clampWizardCursor(w, w.cursor+1)
			return m, nil
		}
		w.cursor = clampWizardCursor(w, w.cursor+1)
		return m, nil

	case "ctrl+u":
		switch w.step {
		case stepName:
			w.name = ""
		case stepProject:
			// Clearing the path also drops the user into Type mode
			// — there's nothing to "pick" from an empty field.
			w.project = ""
			w.projectTyping = true
			w.projectCandidates = nil
		case stepWorktree:
			w.branch = ""
		}
		return m, nil

	case "backspace":
		switch w.step {
		case stepName:
			if len(w.name) > 0 {
				w.name = w.name[:len(w.name)-1]
			}
		case stepProject:
			// Backspace forces Type mode: the user is editing text,
			// not picking from the registry.
			if !w.projectTyping {
				w.projectTyping = true
			}
			if len(w.project) > 0 {
				w.project = w.project[:len(w.project)-1]
			}
			w.projectCandidates = nil
		case stepWorktree:
			if len(w.branch) > 0 {
				w.branch = w.branch[:len(w.branch)-1]
			}
		}
		return m, nil

	case "ctrl+p":
		if w.step == stepProject && w.cursor >= 0 && w.cursor < len(w.projectRegistry) {
			w.project = w.projectRegistry[w.cursor].path
			w.projectTyping = false
		}
		return m, nil

	case " ":
		if w.step == stepWorktree {
			w.wantWorktree = !w.wantWorktree
			return m, nil
		}
		// In other steps, fall through and treat space as input.
	}

	// ---- Character input -----------------------------------
	// From here on we are looking at keystrokes that should either
	// update a text field or navigate a choice list. The rule:
	//
	//   - In stepProject Pick mode, j/k (and arrows) navigate the
	//     registry, `i` enters Type mode pre-filled with the
	//     highlighted path, and any other printable key enters
	//     Type mode with that key already appended.
	//   - In stepProject Type mode, every printable key (including
	//     j/k) appends to the path field.
	//   - In stepTool, j/k/arrows navigate the tool choices and
	//     any letter jumps to the first tool starting with it.
	//   - In stepName / stepWorktree, every printable key appends
	//     to the matching text field.
	if len(key) != 1 && key != " " {
		return m, nil
	}

	switch w.step {
	case stepName:
		w.name += key

	case stepProject:
		if !w.projectTyping {
			switch key {
			case "j":
				w.cursor = clampWizardCursor(w, w.cursor+1)
				return m, nil
			case "k":
				if w.cursor > 0 {
					w.cursor--
				}
				return m, nil
			case "i", "a":
				// vim-style: enter Type mode. "a" appends at the
				// end of the current selection, "i" keeps the
				// cursor where it is (semantically the same for
				// a one-line text field).
				if w.cursor >= 0 && w.cursor < len(w.projectRegistry) {
					w.project = w.projectRegistry[w.cursor].path
				}
				w.projectTyping = true
				return m, nil
			}
			// Any other printable key drops into Type mode with
			// that key already appended. This is the shell-like
			// behaviour: the user started typing, so they mean to
			// edit.
			w.project = key
			w.projectTyping = true
			w.projectCandidates = nil
			return m, nil
		}
		w.project += key
		w.projectCandidates = nil

	case stepWorktreeSetup:
		switch key {
		case "j":
			w.cursor = clampWizardCursor(w, w.cursor+1)
		case "k":
			if w.cursor > 0 {
				w.cursor--
			}
		}
		return m, nil

	case stepTool:
		switch key {
		case "j":
			w.cursor = clampWizardCursor(w, w.cursor+1)
		case "k":
			if w.cursor > 0 {
				w.cursor--
			}
		case " ":
			// Space toggles auto-mode when cursor is on claude.
			if w.cursor >= 0 && w.cursor < len(w.toolChoice) && w.toolChoice[w.cursor] == "claude" {
				w.claudeAutoMode = !w.claudeAutoMode
			}
			return m, nil
		case "b":
			// b toggles bypass permissions when cursor is on claude.
			if w.cursor >= 0 && w.cursor < len(w.toolChoice) && w.toolChoice[w.cursor] == "claude" {
				w.claudeBypassPermissions = !w.claudeBypassPermissions
			}
			return m, nil
		case "r":
			// r toggles --continue (resume) when cursor is on claude.
			if w.cursor >= 0 && w.cursor < len(w.toolChoice) && w.toolChoice[w.cursor] == "claude" {
				w.claudeContinue = !w.claudeContinue
			}
			return m, nil
		default:
			// Type the first letter of a tool to jump to it.
			for i, t := range w.toolChoice {
				if strings.HasPrefix(t, key) {
					w.cursor = i
					break
				}
			}
		}

	case stepWorktree:
		w.branch += key
	}
	return m, nil
}

// advanceWizard moves to the next wizard step, or runs the
// service.Create call when stepWorktree is confirmed.
func (m *Model) advanceWizard() (tea.Model, tea.Cmd) {
	w := &m.wizard
	switch w.step {
	case stepName:
		if strings.TrimSpace(w.name) == "" {
			w.confirmation = "name cannot be empty"
			return m, nil
		}
		w.confirmation = ""
		// Pre-fill the branch name from the session name so the
		// worktree step has a sensible default the user can just
		// Enter through. The name is sanitized to be a valid git
		// branch by replacing spaces/slashes.
		if w.branch == "" {
			w.branch = strings.TrimSpace(w.name)
		}
		w.step = stepProject
		w.cursor = 0

	case stepProject:
		if strings.TrimSpace(w.project) == "" {
			w.confirmation = "project path cannot be empty"
			return m, nil
		}
		// Expand ~ and make the path absolute so the rest of the
		// pipeline (worktree manager, git wrapper) never has to
		// worry about home directories.
		abs, err := resolveProjectPath(w.project)
		if err != nil {
			w.confirmation = "invalid path: " + err.Error()
			return m, nil
		}
		w.project = abs
		// Verify the directory exists so the user gets immediate
		// feedback instead of a cryptic git error two steps later.
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			w.confirmation = "directory does not exist: " + abs
			return m, nil
		}
		w.confirmation = ""
		w.step = stepTool
		w.cursor = 0

	case stepTool:
		if w.cursor >= 0 && w.cursor < len(w.toolChoice) {
			w.tool = w.toolChoice[w.cursor]
		}
		w.confirmation = ""
		w.step = stepWorktree
		w.cursor = 0

	case stepWorktree:
		if w.wantWorktree && strings.TrimSpace(w.branch) == "" {
			w.confirmation = "branch name is required when creating a worktree"
			return m, nil
		}
		if w.wantWorktree {
			// Offer a setup recipe picker — this is where the user
			// can also jump into the recipe wizard to build a new
			// recipe before committing to session creation.
			w.confirmation = ""
			w.setupChoice = buildSetupChoices(m.config)
			w.cursor = setupChoiceIndex(w.setupChoice, w.setup)
			w.step = stepWorktreeSetup
			return m, nil
		}
		return m.finaliseWizard()

	case stepWorktreeSetup:
		if w.cursor < 0 || w.cursor >= len(w.setupChoice) {
			w.confirmation = "pick a setup recipe first"
			return m, nil
		}
		choice := w.setupChoice[w.cursor]
		switch choice.kind {
		case choiceNewRecipe:
			m.openRecipeWizardFromNewSession(w.project)
			return m, nil
		case choiceRecipe:
			w.setup = choice.name
			w.confirmation = ""
			return m.finaliseWizard()
		}
	}
	return m, nil
}

// finaliseWizard assembles the CreateInput and kicks off session
// creation. Callers are responsible for validating wizard-step
// specifics (branch name, etc.) before calling this.
func (m *Model) finaliseWizard() (tea.Model, tea.Cmd) {
	w := &m.wizard
	input := service.CreateInput{
		Name:          w.name,
		ProjectPath:   w.project,
		WorktreePath:  w.project,
		Tool:          store.SessionTool(w.tool),
		Branch:        w.branch,
		ForceContinue: w.claudeContinue,
	}
	if w.wantWorktree {
		input.CreateWorktree = true
		input.WorktreeRepo = w.project
		input.WorktreePath = filepath.Join(w.project, ".worktrees", w.branch)
		input.WorktreeSetup = m.config.Worktree.Setup[w.setup]
	}
	if w.tool == "claude" && (w.claudeAutoMode || w.claudeBypassPermissions) {
		parts := []string{"claude"}
		if w.claudeBypassPermissions {
			parts = append(parts, "--dangerously-skip-permissions")
		}
		if w.claudeAutoMode {
			parts = append(parts, "--auto")
		}
		input.Command = strings.Join(parts, " ")
	}
	m.screen = ScreenTop
	return m, m.runCreate(input)
}

// setupChoiceIndex returns the index of the recipe named name in
// choices, or 0 if it is not present (a safe default that lands the
// cursor on the first entry).
func setupChoiceIndex(choices []setupChoice, name string) int {
	for i, c := range choices {
		if c.kind == choiceRecipe && c.name == name {
			return i
		}
	}
	return 0
}

// resolveProjectPath expands a leading "~" and makes the path
// absolute. It is deliberately lenient: callers always stat() the
// result before using it, so e.g. relative paths resolve against the
// process's working directory and unknown paths fall through.
func resolveProjectPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if p == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[2:])
	}
	return filepath.Abs(p)
}

// runCreate is the tea.Cmd that performs service.Create off the main
// goroutine and refreshes the model when done. It funnels through
// [Model.fetchRefreshed] so the returned [refreshedMsg] carries the
// complete list+subagents snapshot — returning a partial msg flashes
// the list empty and was the source of a frustrating UX bug.
func (m *Model) runCreate(in service.CreateInput) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := m.sessions.Create(ctx, in); err != nil {
			msg := m.fetchRefreshed(ctx)
			msg.err = err
			return msg
		}
		return m.fetchRefreshed(ctx)
	}
}

// clampWizardCursor keeps the cursor inside the choice list of the
// current step.
func clampWizardCursor(w *wizardState, n int) int {
	switch w.step {
	case stepProject:
		if len(w.projectRegistry) == 0 {
			return 0
		}
		if n >= len(w.projectRegistry) {
			return len(w.projectRegistry) - 1
		}
	case stepTool:
		if n >= len(w.toolChoice) {
			return len(w.toolChoice) - 1
		}
	case stepWorktree:
		return 0
	case stepWorktreeSetup:
		if len(w.setupChoice) == 0 {
			return 0
		}
		if n >= len(w.setupChoice) {
			return len(w.setupChoice) - 1
		}
	}
	if n < 0 {
		return 0
	}
	return n
}

// renderWizard draws the modal panel.
func (m *Model) renderWizard() string {
	w := m.wizard
	var lines []string
	title := wizardStepName(w.step, w.wantWorktree)
	lines = append(lines, m.theme.Header.Render("New Session — "+title))
	lines = append(lines, "")

	switch w.step {
	case stepName:
		lines = append(lines,
			m.theme.MutedText.Render("a short, unique label for this session"),
			"",
			"Name:  "+m.theme.AttentionTag.Render(">")+" "+w.name+"_",
			"",
			m.theme.MutedText.Render("[Enter] next  [Esc] cancel  [Ctrl+U] clear"),
		)

	case stepProject:
		modeTag := m.theme.SectionTitle.Render("[pick mode]")
		if w.projectTyping {
			modeTag = m.theme.AttentionTag.Render("[type mode]")
		}
		lines = append(lines,
			m.theme.MutedText.Render("absolute path to the project directory Claude should run in"),
			m.theme.MutedText.Render("(this is also the git repo root when you create a worktree below)"),
			"",
			"Path:  "+modeTag+" "+w.project+typingCursor(w.projectTyping),
		)
		if w.projectTyping && len(w.projectCandidates) > 1 {
			lines = append(lines,
				"",
				m.theme.SectionTitle.Render("tab candidates (keep typing to narrow)"),
				m.renderPathCandidates(w.projectCandidates, 8),
			)
		}
		if len(w.projectRegistry) > 0 {
			lines = append(lines,
				"",
				m.theme.SectionTitle.Render("registered projects"),
				m.renderRegistryChoices(w.projectRegistry, w.cursor, !w.projectTyping),
			)
		}

		// Footer: keys depend on the sub-mode so users see the
		// bindings that apply *right now*.
		if w.projectTyping {
			lines = append(lines,
				"",
				m.theme.MutedText.Render("[Tab] complete  [Enter] next  [Esc] back to pick mode  [Ctrl+U] clear"),
			)
		} else {
			lines = append(lines,
				"",
				m.theme.MutedText.Render("[j/k ↑/↓] highlight  [Enter] use selection  [i] type custom path  [Shift+Tab] back"),
			)
			if len(w.projectRegistry) == 0 {
				// Shouldn't happen (we auto-type mode with empty
				// registry) but guard anyway.
				lines = append(lines,
					"",
					m.theme.MutedText.Render("tip: register projects in ~/.config/mosaic/config.toml:"),
					m.theme.MutedText.Render("  [[project]]"),
					m.theme.MutedText.Render("  name = \"orchard\""),
					m.theme.MutedText.Render("  path = \"~/Projects/orchard\""),
				)
			}
		}

	case stepTool:
		lines = append(lines,
			m.theme.MutedText.Render("which agent runtime should run in this session"),
			"",
			m.renderChoices(w.toolChoice, w.cursor),
		)
		// When the cursor is on "claude", show inline toggles for
		// the auto-mode / bypass-permissions launch flags. These
		// are per-session overrides — the global config defaults
		// are in [mosaic.tools.claude].
		if w.cursor >= 0 && w.cursor < len(w.toolChoice) && w.toolChoice[w.cursor] == "claude" {
			autoBox := "[ ]"
			if w.claudeAutoMode {
				autoBox = "[x]"
			}
			bypassBox := "[ ]"
			if w.claudeBypassPermissions {
				bypassBox = "[x]"
			}
			continueBox := "[ ]"
			if w.claudeContinue {
				continueBox = "[x]"
			}
			lines = append(lines, "",
				m.theme.SectionTitle.Render("Claude options"),
				"  "+continueBox+" resume previous session "+
					m.theme.MutedText.Render("(r to toggle)"),
				"  "+autoBox+" auto mode "+
					m.theme.MutedText.Render("(Space to toggle)"),
				"  "+bypassBox+" bypass permissions "+
					m.theme.MutedText.Render("(b to toggle)"),
			)
		}
		lines = append(lines, "",
			m.theme.MutedText.Render("[Enter] next  [j/k] move  [Space] auto  [b] bypass  [Shift+Tab] back"),
		)

	case stepWorktree:
		toggle := "[ ]"
		if w.wantWorktree {
			toggle = "[x]"
		}
		lines = append(lines,
			m.theme.MutedText.Render("optionally isolate this session in its own git worktree"),
			"",
			toggle+" Create a git worktree for this session  "+m.theme.MutedText.Render("(Space to toggle)"),
		)
		if w.wantWorktree {
			worktreeDest := filepath.Join(w.project, ".worktrees", w.branch)
			lines = append(lines,
				"",
				"Branch: "+m.theme.AttentionTag.Render(">")+" "+w.branch+"_",
				"",
				m.theme.MutedText.Render("  will create: "+worktreeDest),
			)
		}
		if w.wantWorktree {
			lines = append(lines, "",
				m.theme.MutedText.Render("[Enter] next → pick a setup recipe  [Shift+Tab] back  [Esc] cancel"),
			)
		} else {
			lines = append(lines,
				"",
				m.theme.MutedText.Render("[Enter] create  [Shift+Tab] back  [Esc] cancel"),
			)
		}

	case stepWorktreeSetup:
		lines = append(lines,
			m.theme.MutedText.Render("pick the setup recipe to apply to the new worktree"),
			m.theme.MutedText.Render("(recipes declare which files to copy and which commands to run after checkout)"),
			"",
			m.renderSetupChoices(w.setupChoice, w.cursor),
			"",
			m.theme.MutedText.Render("[j/k] move  [Enter] select  [Shift+Tab] back  [Esc] cancel"),
		)
	}

	if w.confirmation != "" {
		lines = append(lines, "", m.theme.AttentionTag.Render("⚠ "+w.confirmation))
	}

	body := strings.Join(lines, "\n")
	width := m.width * 3 / 4
	if width < 60 {
		width = 60
	}
	box := m.theme.BorderBox.Width(width).Render(body)
	// Centre the wizard panel the same way the confirmation modal
	// does, so modal overlays visually sit in one consistent
	// location regardless of terminal size.
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// renderSetupChoices renders the setup picker, styling the
// "+ create new recipe" sentinel in muted text so it reads as an
// action rather than a recipe entry.
func (m *Model) renderSetupChoices(choices []setupChoice, cursor int) string {
	var lines []string
	for i, c := range choices {
		marker := "  "
		if i == cursor {
			marker = m.theme.AttentionTag.Render("▶ ")
		}
		label := c.label()
		if c.kind == choiceNewRecipe {
			label = m.theme.MutedText.Render(label)
		}
		lines = append(lines, marker+label)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderChoices(choices []string, cursor int) string {
	var lines []string
	for i, c := range choices {
		marker := "  "
		if i == cursor {
			marker = m.theme.AttentionTag.Render("▶ ")
		}
		lines = append(lines, marker+c)
	}
	return strings.Join(lines, "\n")
}

// renderRegistryChoices draws a name + path two-column view. When
// highlightCursor is true (pick mode), the current row gets an arrow
// marker; in type mode the list is dimmed so the user does not
// confuse it for an active selection.
func (m *Model) renderRegistryChoices(choices []registeredProject, cursor int, highlightCursor bool) string {
	var lines []string
	for i, c := range choices {
		marker := "  "
		nameStyle := m.theme.NormalRow
		if highlightCursor && i == cursor {
			marker = m.theme.AttentionTag.Render("▶ ")
			nameStyle = m.theme.AttentionTag
		} else if !highlightCursor {
			nameStyle = m.theme.MutedText
		}
		lines = append(lines, marker+nameStyle.Render(c.name)+"  "+m.theme.MutedText.Render(c.path))
	}
	return strings.Join(lines, "\n")
}

// typingCursor returns the blinking-ish cursor indicator shown at
// the end of the path input in Type mode.
func typingCursor(typing bool) string {
	if typing {
		return "_"
	}
	return ""
}

// renderPathCandidates draws up to limit entries from a Tab-completion
// candidate list. When the list is longer than limit, a "… +N more"
// footer lets the user know they should keep typing to narrow it.
func (m *Model) renderPathCandidates(cands []string, limit int) string {
	if limit <= 0 || limit > len(cands) {
		limit = len(cands)
	}
	var lines []string
	for i := 0; i < limit; i++ {
		lines = append(lines, "  "+m.theme.MutedText.Render(cands[i]))
	}
	if len(cands) > limit {
		lines = append(lines, m.theme.MutedText.Render(fmt.Sprintf("  … +%d more", len(cands)-limit)))
	}
	return strings.Join(lines, "\n")
}

// wizardStepName turns the enum into the modal title. The total step
// count depends on whether the user has toggled worktree creation on —
// flipping the denominator keeps the displayed "(n/total)" consistent
// across pages instead of mixing "(1/4)" with "(5/5)".
func wizardStepName(s wizardStep, wantWorktree bool) string {
	total := 4
	if wantWorktree {
		total = 5
	}
	switch s {
	case stepName:
		return fmt.Sprintf("Name (1/%d)", total)
	case stepProject:
		return fmt.Sprintf("Project (2/%d)", total)
	case stepTool:
		return fmt.Sprintf("Tool (3/%d)", total)
	case stepWorktree:
		return fmt.Sprintf("Worktree (4/%d)", total)
	case stepWorktreeSetup:
		return "Setup recipe (5/5)"
	}
	return ""
}
