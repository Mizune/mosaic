package tui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/config"
)

// pressRecipe injects a bubbletea key and returns just the action,
// matching the ergonomics of the pre-async API for most test cases.
// The tea.Cmd returned by updateRecipeWizardKey is non-nil only when
// the Preview step kicks off a save — tests that need to observe
// the save use [pressRecipeWithCmd] instead.
func pressRecipe(t *testing.T, state *recipeWizardState, key string) recipeWizardAction {
	t.Helper()
	action, _ := pressRecipeWithCmd(t, state, key)
	return action
}

// pressRecipeWithCmd is the full variant that returns both values so
// a test can inspect and invoke the returned save Cmd.
func pressRecipeWithCmd(t *testing.T, state *recipeWizardState, key string) (recipeWizardAction, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+u":
		msg = tea.KeyMsg{Type: tea.KeyCtrlU}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	return updateRecipeWizardKey(state, msg)
}

func typeRecipe(t *testing.T, state *recipeWizardState, text string) {
	t.Helper()
	for _, r := range text {
		pressRecipe(t, state, string(r))
	}
}

func TestRecipeWizard_NameStep_ValidatesInput(t *testing.T) {
	state := openRecipeWizard(t.TempDir(), nil)

	// Empty name → refuses to advance.
	if a := pressRecipe(t, &state, "enter"); a != recipeActionNone {
		t.Fatalf("expected no action on empty name, got %v", a)
	}
	if state.step != recipeStepName {
		t.Fatalf("expected to stay on Name step, got %v", state.step)
	}
	if state.confirmation == "" {
		t.Fatalf("expected an inline error explaining why")
	}

	// Invalid characters → refuses.
	state.name = "bad name!"
	if a := pressRecipe(t, &state, "enter"); a != recipeActionNone {
		t.Fatalf("expected no action on bad name, got %v", a)
	}
	if state.step != recipeStepName {
		t.Fatalf("expected to stay on Name step for invalid chars")
	}

	// Valid name → advances.
	state.name = ""
	typeRecipe(t, &state, "my-recipe")
	if state.name != "my-recipe" {
		t.Fatalf("expected name=my-recipe, got %q", state.name)
	}
	pressRecipe(t, &state, "enter")
	if state.step != recipeStepCopy {
		t.Fatalf("expected to advance to Copy step, got %v", state.step)
	}
}

func TestRecipeWizard_CopyStep_AddsEditsAndDeletes(t *testing.T) {
	state := openRecipeWizard(t.TempDir(), nil)
	state.step = recipeStepCopy
	// The default recipe pre-seeds four Copy entries.
	originalLen := len(state.copy.lines)

	// Delete the first entry.
	state.copy.cursor = 0
	pressRecipe(t, &state, "d")
	if len(state.copy.lines) != originalLen-1 {
		t.Fatalf("expected delete to shrink list: want %d, got %d", originalLen-1, len(state.copy.lines))
	}

	// Add a new empty row and type into it.
	state.copy.cursor = len(state.copy.lines) // end of list
	pressRecipe(t, &state, "a")
	if !state.copy.editing {
		t.Fatalf("expected 'a' to drop into edit mode")
	}
	typeRecipe(t, &state, ".custom")
	pressRecipe(t, &state, "enter")
	if state.copy.editing {
		t.Fatalf("expected Enter to commit the edit")
	}
	if state.copy.lines[state.copy.cursor] != ".custom" {
		t.Fatalf("expected .custom at cursor, got %q", state.copy.lines[state.copy.cursor])
	}

	// Edit an existing row with 'i'.
	state.copy.cursor = 0
	before := state.copy.lines[0]
	pressRecipe(t, &state, "i")
	if !state.copy.editing {
		t.Fatalf("expected 'i' to enter edit mode")
	}
	if state.copy.buf != before {
		t.Fatalf("expected edit buf to be pre-filled with %q, got %q", before, state.copy.buf)
	}
}

func TestRecipeWizard_Toggles(t *testing.T) {
	state := openRecipeWizard(t.TempDir(), nil)
	state.step = recipeStepToggles

	// Space on cursor 0 → CopyIgnored on.
	pressRecipe(t, &state, " ")
	if !state.copyIgnored {
		t.Fatalf("expected CopyIgnored=true after Space on cursor 0")
	}

	// j moves to cursor 1, Space toggles CopyUntracked.
	pressRecipe(t, &state, "j")
	pressRecipe(t, &state, " ")
	if !state.copyUntracked {
		t.Fatalf("expected CopyUntracked=true after Space on cursor 1")
	}

	// Tab advances to Commands.
	pressRecipe(t, &state, "tab")
	if state.step != recipeStepCommands {
		t.Fatalf("expected to advance to Commands, got %v", state.step)
	}
}

func TestRecipeWizard_FullFlowSavesToDisk(t *testing.T) {
	projectDir := t.TempDir()
	// Seed a file that lockfile detection should spot.
	must(t, os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module x\n"), 0o644))

	state := openRecipeWizard(projectDir, map[string]struct{}{})
	if !containsString(state.lockHints, "go mod download") {
		t.Fatalf("expected go.mod to surface a suggestion, got %v", state.lockHints)
	}

	// Name step.
	typeRecipe(t, &state, "alpha")
	pressRecipe(t, &state, "enter")
	if state.step != recipeStepCopy {
		t.Fatalf("stuck on step %v at Name", state.step)
	}

	// Copy step: accept defaults, Tab to advance.
	state.copy.cursor = len(state.copy.lines)
	pressRecipe(t, &state, "tab")
	if state.step != recipeStepToggles {
		t.Fatalf("stuck on step %v at Copy", state.step)
	}

	// Toggles: leave both off, Tab to Commands.
	pressRecipe(t, &state, "tab")
	if state.step != recipeStepCommands {
		t.Fatalf("stuck on step %v at Toggles", state.step)
	}

	// Commands: add "echo hi".
	pressRecipe(t, &state, "a")
	typeRecipe(t, &state, "echo hi")
	pressRecipe(t, &state, "enter")
	state.commands.cursor = len(state.commands.lines) // end sentinel
	pressRecipe(t, &state, "tab")
	if state.step != recipeStepHook {
		t.Fatalf("stuck on step %v at Commands", state.step)
	}

	// Hook: skip.
	pressRecipe(t, &state, "tab")
	if state.step != recipeStepEnv {
		t.Fatalf("stuck on step %v at Hook", state.step)
	}

	// Env: add a KEY=VAL line.
	pressRecipe(t, &state, "a")
	typeRecipe(t, &state, "FOO=bar")
	pressRecipe(t, &state, "enter")
	state.env.cursor = len(state.env.lines)
	pressRecipe(t, &state, "tab")
	if state.step != recipeStepPreview {
		t.Fatalf("stuck on step %v at Env", state.step)
	}
	if state.previewTOML == "" {
		t.Fatalf("expected preview TOML to be populated")
	}
	if !strings.Contains(state.previewTOML, "[worktree.setup.alpha]") {
		t.Fatalf("expected rendered TOML to address [worktree.setup.alpha], got:\n%s", state.previewTOML)
	}

	// Preview + Enter → kicks off async save (cmd non-nil).
	action, cmd := pressRecipeWithCmd(t, &state, "enter")
	if action != recipeActionNone {
		t.Fatalf("expected no action while save is pending, got %v", action)
	}
	if !state.saving {
		t.Fatalf("expected saving=true after Enter on Preview")
	}
	if cmd == nil {
		t.Fatalf("expected a save tea.Cmd from Preview step")
	}
	// Invoke the Cmd (this is what Bubble Tea's runtime would do) and
	// feed the resulting msg through applyRecipeSaveResult.
	rawMsg := cmd()
	saveMsg, ok := rawMsg.(recipeSaveResultMsg)
	if !ok {
		t.Fatalf("expected recipeSaveResultMsg, got %T", rawMsg)
	}
	if saveMsg.err != nil {
		t.Fatalf("save Cmd reported error: %v", saveMsg.err)
	}
	if action := applyRecipeSaveResult(&state, saveMsg); action != recipeActionSaved {
		t.Fatalf("expected saved action after applying save result, got %v (confirmation=%q)", action, state.confirmation)
	}
	if state.saving {
		t.Fatalf("expected saving=false after save result")
	}
	if !state.saved {
		t.Fatalf("expected saved=true after save result")
	}

	// Verify the file on disk matches.
	path := filepath.Join(projectDir, ".mosaic", "config.toml")
	var onDisk config.Config
	if _, err := toml.DecodeFile(path, &onDisk); err != nil {
		t.Fatalf("decode written file: %v", err)
	}
	r, ok := onDisk.Worktree.Setup["alpha"]
	if !ok {
		t.Fatalf("expected recipe 'alpha' in written config, got %v", onDisk.Worktree.Setup)
	}
	if !containsString(r.Commands, "echo hi") {
		t.Fatalf("expected Commands to include 'echo hi', got %v", r.Commands)
	}
	if r.Env["FOO"] != "bar" {
		t.Fatalf("expected FOO=bar in Env, got %v", r.Env)
	}
}

func TestRecipeWizard_PreviewMarksMissingCopyEntries(t *testing.T) {
	projectDir := t.TempDir()
	// .env exists; .cursor/ does not.
	must(t, os.WriteFile(filepath.Join(projectDir, ".env"), []byte(""), 0o644))

	state := openRecipeWizard(projectDir, nil)
	state.name = "alpha"
	state.copy.lines = []string{".env", ".cursor/"}
	state.step = recipeStepPreview
	state.missingCopy = computeMissingCopy(projectDir, state.copy.lines)

	if state.missingCopy[".env"] {
		t.Fatalf("expected .env to be marked present, got missing")
	}
	if !state.missingCopy[".cursor/"] {
		t.Fatalf("expected .cursor/ to be marked missing")
	}
}

func TestRecipeWizard_DetectLockfileCommands(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte(""), 0o644))

	got := detectLockfileCommands(dir)
	sort.Strings(got)
	want := []string{"go mod download", "pnpm install"}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lockfile detection: want %v, got %v", want, got)
	}

	if len(detectLockfileCommands("")) != 0 {
		t.Fatalf("empty path should yield no hints")
	}
}

func TestRecipeWizard_ToRecipe_OmitsEmpty(t *testing.T) {
	state := openRecipeWizard(t.TempDir(), nil)
	state.name = "x"
	state.copy.lines = []string{" ", ""} // all whitespace — should drop out entirely
	state.commands.lines = nil
	state.env.lines = []string{"INVALID-LINE", "=val", "A=1"}

	r := state.toRecipe()
	if r.Copy != nil {
		t.Fatalf("expected Copy nil after cleaning whitespace-only entries, got %v", r.Copy)
	}
	if r.Commands != nil {
		t.Fatalf("expected Commands nil, got %v", r.Commands)
	}
	if r.Env["A"] != "1" {
		t.Fatalf("expected A=1 parsed, got %v", r.Env)
	}
	if _, bad := r.Env[""]; bad {
		t.Fatalf("expected malformed env lines to be dropped, got %v", r.Env)
	}
}

func TestRecipeWizard_BuildSetupChoicesAppendsSentinel(t *testing.T) {
	cfg := config.Config{
		Worktree: config.WorktreeSection{
			Setup: map[string]config.WorktreeSetup{
				"default": {},
				"beta":    {},
				"alpha":   {},
			},
		},
	}
	got := buildSetupChoices(cfg)
	if len(got) != 4 {
		t.Fatalf("expected 3 recipes + sentinel, got %d choices: %v", len(got), got)
	}
	if got[3].kind != choiceNewRecipe {
		t.Fatalf("expected sentinel last, got kind=%v name=%q", got[3].kind, got[3].name)
	}
	// Recipes are sorted alphabetically before the sentinel.
	wantNames := []string{"alpha", "beta", "default"}
	for i, w := range wantNames {
		if got[i].kind != choiceRecipe || got[i].name != w {
			t.Fatalf("expected choices[%d] = recipe %q, got kind=%v name=%q", i, w, got[i].kind, got[i].name)
		}
	}
}

func TestRecipeWizard_SaverInjectionAndErrorHandling(t *testing.T) {
	// Drive the Preview step with a mock saver so we can assert
	// (a) it is invoked with the recipe values the wizard collected,
	// and (b) a saver error leaves the wizard open with an inline
	// error instead of advancing.
	var calls []struct {
		path, name string
		recipe     config.WorktreeSetup
	}
	wantErr := errors.New("disk full")
	mockSaver := func(path, name string, r config.WorktreeSetup) error {
		calls = append(calls, struct {
			path, name string
			recipe     config.WorktreeSetup
		}{path, name, r})
		return wantErr
	}

	state := openRecipeWizardWithSaver(t.TempDir(), nil, mockSaver)
	state.name = "alpha"
	state.step = recipeStepPreview

	// Press Enter → saver runs, returns wantErr → apply → inline error banner.
	_, cmd := pressRecipeWithCmd(t, &state, "enter")
	if cmd == nil {
		t.Fatalf("expected a save tea.Cmd")
	}
	saveMsg := cmd().(recipeSaveResultMsg)
	if action := applyRecipeSaveResult(&state, saveMsg); action != recipeActionNone {
		t.Fatalf("expected no-action on saver error, got %v", action)
	}
	if state.saved {
		t.Fatalf("expected saved=false after saver error")
	}
	if state.confirmation == "" || !strings.Contains(state.confirmation, "disk full") {
		t.Fatalf("expected inline error including 'disk full', got %q", state.confirmation)
	}
	if len(calls) != 1 || calls[0].name != "alpha" {
		t.Fatalf("expected saver called once with name=alpha, got %v", calls)
	}
}

func TestRecipeWizard_EscCancels(t *testing.T) {
	state := openRecipeWizard(t.TempDir(), nil)
	if a := pressRecipe(t, &state, "esc"); a != recipeActionCancelled {
		t.Fatalf("expected Esc to cancel, got %v", a)
	}
}

func TestRecipeWizard_ShiftTabGoesBack(t *testing.T) {
	state := openRecipeWizard(t.TempDir(), nil)
	state.step = recipeStepCommands
	pressRecipe(t, &state, "shift+tab")
	if state.step != recipeStepToggles {
		t.Fatalf("expected shift+tab to back up one step, landed on %v", state.step)
	}
}

// TestRecipeWizard_IsPrintableRejectsNamedKeys guards against a
// regression where multi-character bubbletea key names like "up",
// "down", "home", "pgup", "shift+tab" leaked into text buffers
// because isPrintable accepted any string of printable runes. The
// correct behavior is to accept only single-rune keys so navigation
// keys never get appended to recipe names or hook paths.
func TestRecipeWizard_IsPrintableRejectsNamedKeys(t *testing.T) {
	namedKeys := []string{"up", "down", "left", "right", "home", "end", "pgup", "pgdown", "shift+tab", "ctrl+a"}
	for _, k := range namedKeys {
		if isPrintable(k) {
			t.Errorf("isPrintable(%q) = true; want false (multi-char named key)", k)
		}
	}

	singleRuneKeys := []string{"a", "Z", "0", " ", "-", "日", "é"}
	for _, k := range singleRuneKeys {
		if !isPrintable(k) {
			t.Errorf("isPrintable(%q) = false; want true (single printable rune)", k)
		}
	}

	// Integration check: pressing "up" on the Name step must not
	// append "up" to the recipe name.
	state := openRecipeWizard(t.TempDir(), nil)
	typeRecipe(t, &state, "my")
	pressRecipe(t, &state, "up")
	if state.name != "my" {
		t.Fatalf("expected 'up' arrow to be ignored on Name step, got name=%q", state.name)
	}
}

// TestRecipeWizardCLI_CtrlCIgnoredDuringSave is a regression for
// b-008: pressing Ctrl+C after Enter on the Preview step (but before
// the save Cmd completes) used to quit the program with done=false,
// which made the CLI report "cancelled" even though the atomic write
// had already committed to disk. Ctrl+C must be swallowed while
// state.saving is true so the Cmd's recipeSaveResultMsg drives the
// outcome instead.
func TestRecipeWizardCLI_CtrlCIgnoredDuringSave(t *testing.T) {
	p := NewRecipeWizardProgram(t.TempDir(), "alpha", nil, nil)
	p.state.saving = true

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Fatalf("expected Ctrl+C during save to return nil cmd (no quit), got non-nil")
	}
	if p.done {
		t.Fatalf("done must stay false until recipeSaveResultMsg decides the outcome")
	}

	// Sanity: once saving clears, Ctrl+C regains its normal cancel
	// semantics and quits the program.
	p.state.saving = false
	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatalf("expected Ctrl+C to return a tea.Quit cmd after save completes")
	}
	if p.done {
		t.Fatalf("Ctrl+C should mark the run as cancelled, got done=true")
	}
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
