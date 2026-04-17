// Package tui — recipe_wizard_render.go owns every rendering helper
// for the recipe wizard. Keeping the render surface in its own file
// leaves recipe_wizard.go focused on pure state + update logic, which
// matches the cli/model host split (recipe_wizard_cli.go,
// recipe_wizard_model.go).
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/tui/theme"
)

const (
	wizardBoxWidthRatio = 3 // numerator; denominator is 4 (i.e. 75% of screen)
	wizardMinBoxWidth   = 60
)

// renderRecipeWizard draws the wizard and returns its full-screen
// box. Callers are responsible for any surrounding chrome.
func renderRecipeWizard(s *recipeWizardState, t *theme.Theme, width, height int) string {
	var lines []string
	lines = append(lines, t.Header.Render("New Worktree Recipe — "+recipeStepTitle(s.step)), "")

	if s.saved {
		lines = append(lines,
			t.SectionTitle.Render("✓ saved"),
			"  "+t.MutedText.Render(s.savePath),
			"",
			t.MutedText.Render("press any key to close"),
		)
		return frameRecipeWizard(t, lines, width, height)
	}

	switch s.step {
	case recipeStepName:
		lines = append(lines, renderRecipeName(s, t)...)
	case recipeStepCopy:
		lines = append(lines, renderRecipeList(t, "files to copy into the worktree", &s.copy, nil)...)
	case recipeStepToggles:
		lines = append(lines, renderRecipeToggles(s, t)...)
	case recipeStepCommands:
		lines = append(lines, renderRecipeList(t, "shell commands to run after copying", &s.commands, s.lockHints)...)
	case recipeStepHook:
		lines = append(lines, renderRecipeHook(s, t)...)
	case recipeStepEnv:
		lines = append(lines, renderRecipeList(t, "environment variables (KEY=value)", &s.env, nil)...)
	case recipeStepPreview:
		lines = append(lines, renderRecipePreview(s, t)...)
	}

	if s.confirmation != "" {
		lines = append(lines, "", t.AttentionTag.Render("⚠ "+s.confirmation))
	}
	return frameRecipeWizard(t, lines, width, height)
}

func frameRecipeWizard(t *theme.Theme, lines []string, width, height int) string {
	body := strings.Join(lines, "\n")
	boxWidth := width * wizardBoxWidthRatio / 4
	if boxWidth < wizardMinBoxWidth {
		boxWidth = wizardMinBoxWidth
	}
	box := t.BorderBox.Width(boxWidth).Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderRecipeName(s *recipeWizardState, t *theme.Theme) []string {
	lines := []string{
		t.MutedText.Render("name this recipe — it becomes the TOML key under [worktree.setup.<name>]"),
		"",
		"  " + t.AttentionTag.Render("> ") + s.name + "_",
	}
	if _, clash := s.existingNames[s.name]; clash && s.name != "" {
		lines = append(lines, "", t.AttentionTag.Render("⚠ recipe \""+s.name+"\" already exists — saving will overwrite it"))
	}
	lines = append(lines, "",
		t.MutedText.Render("save path: "+s.savePath),
		"",
		t.MutedText.Render("[Tab/Enter] next  [Esc] cancel"))
	return lines
}

func renderRecipeToggles(s *recipeWizardState, t *theme.Theme) []string {
	row := func(label string, on bool, selected bool) string {
		check := "[ ]"
		if on {
			check = "[x]"
		}
		prefix := "  "
		if selected {
			prefix = t.AttentionTag.Render("▶ ")
		}
		return prefix + check + " " + label
	}
	return []string{
		t.MutedText.Render("optional bulk-copy toggles"), "",
		row("CopyIgnored    (copy .gitignore'd files such as .env, build caches)", s.copyIgnored, s.toggleCursor == toggleIdxCopyIgnored),
		row("CopyUntracked  (copy untracked working-tree files)", s.copyUntracked, s.toggleCursor == toggleIdxCopyUntracked),
		"",
		t.MutedText.Render("[j/k] move  [Space] toggle  [Tab/Enter] next  [Shift+Tab] back  [Esc] cancel"),
	}
}

func renderRecipeHook(s *recipeWizardState, t *theme.Theme) []string {
	hint := "(leave blank for no hook)"
	val := s.hook
	if val == "" {
		val = t.MutedText.Render(hint)
	}
	return []string{
		t.MutedText.Render("optional post-create hook script (path relative to project root)"), "",
		"  " + t.AttentionTag.Render("> ") + val + "_",
		"",
		t.MutedText.Render("[Tab/Enter] next  [Shift+Tab] back  [Ctrl+U] clear  [Esc] cancel"),
	}
}

// renderRecipeList renders a list-editor step (Copy / Commands /
// Env). It composes three sub-renderers: the rows themselves, an
// optional suggestions block, and the context-appropriate key help
// footer. Keeping them separate makes each section easy to scan.
func renderRecipeList(t *theme.Theme, headline string, ed *listEditor, hints []string) []string {
	out := []string{t.MutedText.Render(headline), ""}
	out = append(out, renderListRows(t, ed)...)
	out = append(out, renderListHints(t, hints)...)
	out = append(out, renderListKeyHelp(t, ed.editing)...)
	return out
}

// renderListRows renders the editable rows plus the synthetic
// "advance" row below the last entry. The advance row lets the user
// press j past the bottom without deselecting, then Enter / Tab to
// move to the next step.
func renderListRows(t *theme.Theme, ed *listEditor) []string {
	if len(ed.lines) == 0 {
		return []string{
			t.MutedText.Render("  (no entries — press 'a' to add)"),
			renderAdvanceRow(t, ed),
		}
	}
	out := make([]string, 0, len(ed.lines)+1)
	for i, v := range ed.lines {
		out = append(out, renderListRow(t, ed, i, v))
	}
	out = append(out, renderAdvanceRow(t, ed))
	return out
}

func renderListRow(t *theme.Theme, ed *listEditor, index int, value string) string {
	prefix := "  "
	if index == ed.cursor {
		prefix = t.AttentionTag.Render("▶ ")
	}
	row := value
	switch {
	case ed.editing && index == ed.cursor:
		row = ed.buf + "_"
	case row == "":
		row = t.MutedText.Render("(empty — press 'i' to edit)")
	}
	return prefix + row
}

func renderAdvanceRow(t *theme.Theme, ed *listEditor) string {
	prefix := "  "
	if ed.cursor == len(ed.lines) && !ed.editing {
		prefix = t.AttentionTag.Render("▶ ")
	}
	return prefix + t.MutedText.Render("<done — [Enter] or [Tab] to advance>")
}

func renderListHints(t *theme.Theme, hints []string) []string {
	if len(hints) == 0 {
		return nil
	}
	out := []string{"", t.SectionTitle.Render("suggestions (detected)")}
	for _, h := range hints {
		out = append(out, "  "+t.MutedText.Render("• "+h))
	}
	return out
}

func renderListKeyHelp(t *theme.Theme, editing bool) []string {
	if editing {
		return []string{"", t.MutedText.Render("[Enter] commit  [Esc] cancel edit  [Ctrl+U] clear")}
	}
	return []string{"",
		t.MutedText.Render("[j/k] move  [a] add  [i/Enter] edit  [d] delete  [Tab] next step  [Shift+Tab] back  [Esc] cancel"),
	}
}

func renderRecipePreview(s *recipeWizardState, t *theme.Theme) []string {
	lines := []string{
		t.MutedText.Render("this TOML block will be written to"),
		"  " + s.savePath,
		"",
		t.SectionTitle.Render("recipe"),
	}
	for _, ln := range strings.Split(strings.TrimRight(s.previewTOML, "\n"), "\n") {
		lines = append(lines, "  "+ln)
	}
	if len(s.missingCopy) > 0 {
		lines = append(lines, "", t.AttentionTag.Render("Copy entries not present in the repo (will be skipped at worktree-create time):"))
		missing := sortedKeys(s.missingCopy)
		for _, p := range missing {
			lines = append(lines, "  "+t.AttentionTag.Render("※ ")+p)
		}
	}
	if _, clash := s.existingNames[s.name]; clash {
		lines = append(lines, "", t.AttentionTag.Render("⚠ overwriting existing recipe \""+s.name+"\""))
	}
	lines = append(lines, "",
		t.MutedText.Render("[Enter] save  [Shift+Tab] back  [Esc] cancel"))
	return lines
}

func recipeStepTitle(step recipeStep) string {
	switch step {
	case recipeStepName:
		return "name"
	case recipeStepCopy:
		return "copy files"
	case recipeStepToggles:
		return "copy toggles"
	case recipeStepCommands:
		return "commands"
	case recipeStepHook:
		return "post-create hook"
	case recipeStepEnv:
		return "env vars"
	case recipeStepPreview:
		return "preview & save"
	}
	return fmt.Sprintf("step %d", int(step))
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
