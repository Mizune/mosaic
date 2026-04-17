package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/tui/theme"
)

// RecipeWizardProgram is a standalone bubbletea model that runs
// only the recipe wizard. It is the backend for `mosaic worktree
// setup new`: the CLI builds one of these, runs it to completion,
// and reads [RecipeWizardProgram.Saved] / [RecipeWizardProgram.Name]
// after the program exits.
//
// Unlike the full TUI [Model], this program does not touch the
// session store, bus, or event loop — it just drives the recipe
// wizard state machine defined in recipe_wizard.go.
type RecipeWizardProgram struct {
	state  recipeWizardState
	theme  *theme.Theme
	width  int
	height int
	done   bool
}

// NewRecipeWizardProgram constructs a RecipeWizardProgram seeded
// with the given project path and any pre-seeded recipe name. The
// existingNames set is used to warn the user before overwriting a
// recipe they already have.
func NewRecipeWizardProgram(projectPath, initialName string, existingNames map[string]struct{}, t *theme.Theme) *RecipeWizardProgram {
	state := openRecipeWizard(projectPath, existingNames)
	state.name = initialName
	return &RecipeWizardProgram{state: state, theme: t}
}

// Init satisfies tea.Model. The wizard has no initial command.
func (p *RecipeWizardProgram) Init() tea.Cmd { return nil }

// Update satisfies tea.Model. Key presses are routed through the
// shared wizard handler; completion (save or cancel) quits the
// program so the CLI caller can inspect the result.
//
// The save step runs asynchronously — the wizard returns a
// [tea.Cmd] that emits [recipeSaveResultMsg] on completion, which
// we then fold into state via [applyRecipeSaveResult] before
// quitting.
func (p *RecipeWizardProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			// Do not allow Ctrl+C to race with an in-flight save —
			// the atomic write may already have committed to disk.
			// Swallow the key and wait for recipeSaveResultMsg to
			// decide the outcome. Once the save returns, Ctrl+C
			// regains its normal cancel semantics.
			if p.state.saving {
				return p, nil
			}
			p.done = false
			return p, tea.Quit
		}
		action, cmd := updateRecipeWizardKey(&p.state, msg)
		switch action {
		case recipeActionSaved:
			p.done = true
			return p, tea.Quit
		case recipeActionCancelled:
			p.done = false
			return p, tea.Quit
		}
		return p, cmd
	case recipeSaveResultMsg:
		action := applyRecipeSaveResult(&p.state, msg)
		if action == recipeActionSaved {
			p.done = true
			return p, tea.Quit
		}
		// Save failed — keep the wizard open so the user can retry
		// or cancel. applyRecipeSaveResult has already populated
		// p.state.confirmation with the error message.
		return p, nil
	}
	return p, nil
}

// View satisfies tea.Model.
func (p *RecipeWizardProgram) View() string {
	if p.width == 0 {
		return "loading..."
	}
	return renderRecipeWizard(&p.state, p.theme, p.width, p.height)
}

// Saved reports whether the wizard finished with a successful save.
// CLI callers check this after tea.Program.Run returns so they can
// report the new recipe's location or a cancellation.
func (p *RecipeWizardProgram) Saved() bool { return p.done && p.state.saved }

// Name returns the recipe name the wizard produced.
func (p *RecipeWizardProgram) Name() string { return p.state.name }

// SavePath returns the file the wizard wrote to (or would have
// written to, for cancelled runs).
func (p *RecipeWizardProgram) SavePath() string { return p.state.savePath }
