// Package tui — recipe_wizard_model.go hosts the recipe wizard inside
// the main TUI [Model]. It mirrors recipe_wizard_cli.go (the CLI host)
// and keeps recipe_wizard.go focused on pure state/update/render logic.
package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/config"
)

// --- Integration with the main TUI Model ----------------------------------

// openRecipeWizardFromNewSession seeds the recipe wizard for the
// project the new-session wizard is currently pointed at, then
// switches the screen to ScreenRecipeWizard. On completion the
// Model's Update will bring the user back to ScreenNewSession with
// the new recipe pre-selected.
func (m *Model) openRecipeWizardFromNewSession(projectPath string) {
	names := make(map[string]struct{}, len(m.config.Worktree.Setup))
	for k := range m.config.Worktree.Setup {
		names[k] = struct{}{}
	}
	m.recipeWizard = openRecipeWizard(projectPath, names)
	m.screen = ScreenRecipeWizard
}

// updateRecipeWizardScreen handles a key press while the recipe
// wizard is the active screen. It delegates to the pure wizard
// logic and forwards any save [tea.Cmd] the wizard emitted. Screen
// transitions happen here (cancel) or in
// [Model.applyRecipeSaveResult] (save success), depending on which
// action fires.
func (m *Model) updateRecipeWizardScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action, cmd := updateRecipeWizardKey(&m.recipeWizard, msg)
	switch action {
	case recipeActionCancelled:
		// Return to the new-session wizard without altering its
		// state. The user can still switch setup or back out.
		m.recipeWizard = recipeWizardState{}
		m.screen = ScreenNewSession
		return m, nil
	case recipeActionSaved:
		// Post-save dismissal (the Preview screen shows "saved" and
		// any key now advances). The real transition happened when
		// applyRecipeSaveResult fired; here we just close the
		// overlay.
		m.recipeWizard = recipeWizardState{}
		m.screen = ScreenNewSession
		return m, nil
	}
	return m, cmd
}

// applyRecipeSaveResult is called from [Model.Update] when a
// [recipeSaveResultMsg] lands. It folds the save outcome into the
// wizard state and, on success, refreshes the new-session wizard's
// setup choices so the just-created recipe is pre-selected.
func (m *Model) applyRecipeSaveResultMsg(msg recipeSaveResultMsg) (tea.Model, tea.Cmd) {
	action := applyRecipeSaveResult(&m.recipeWizard, msg)
	if action != recipeActionSaved {
		return m, nil
	}
	// Layer the new recipe into the running config so the
	// new-session wizard's setup choices include it immediately,
	// without requiring a TUI restart.
	recipe := m.recipeWizard.toRecipe()
	name := m.recipeWizard.name
	if m.config.Worktree.Setup == nil {
		m.config.Worktree.Setup = map[string]config.WorktreeSetup{}
	}
	m.config.Worktree.Setup[name] = recipe
	// Refresh the wizard's setup choices and pre-select the recipe
	// we just built. Re-syncing cursor here keeps the highlight on
	// the just-created recipe — otherwise it would still point at
	// whatever sits at the old index (possibly the "+ create new
	// recipe" sentinel).
	m.wizard.setupChoice = buildSetupChoices(m.config)
	m.wizard.setup = name
	m.wizard.cursor = setupChoiceIndex(m.wizard.setupChoice, name)
	m.setTopToast("saved worktree recipe \"" + name + "\"")
	return m, nil
}

// choiceKind discriminates the two types of entry the new-session
// wizard's setup picker can show: a concrete existing recipe, or
// the "+ create new recipe" sentinel that launches the recipe
// wizard.
type choiceKind int

const (
	// choiceRecipe is a real recipe name pulled from
	// config.Worktree.Setup. The choice's name field carries the
	// recipe key.
	choiceRecipe choiceKind = iota
	// choiceNewRecipe is the trailing sentinel that dispatches into
	// the recipe wizard. The name field is unused.
	choiceNewRecipe
)

// setupChoice is one row in the new-session wizard's setup picker.
// Using a typed struct (instead of a []string with a magic
// sentinel) makes the producer/consumer contract explicit: the
// wizard's advance handler switches on kind and the label is
// computed, not compared against a literal.
type setupChoice struct {
	kind choiceKind
	name string
}

// label returns the string to render for this choice.
func (c setupChoice) label() string {
	if c.kind == choiceNewRecipe {
		return "+ create new recipe"
	}
	return c.name
}

// buildSetupChoices returns every defined worktree recipe in
// alphabetical order, followed by the "+ create new recipe"
// sentinel that dispatches into the recipe wizard.
func buildSetupChoices(cfg config.Config) []setupChoice {
	names := make([]string, 0, len(cfg.Worktree.Setup))
	for k := range cfg.Worktree.Setup {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]setupChoice, 0, len(names)+1)
	for _, n := range names {
		out = append(out, setupChoice{kind: choiceRecipe, name: n})
	}
	out = append(out, setupChoice{kind: choiceNewRecipe})
	return out
}
