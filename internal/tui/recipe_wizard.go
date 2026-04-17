// Package tui — recipe_wizard.go implements the interactive
// wizard that builds a `[worktree.setup.<name>]` TOML block and
// writes it into the project's .mosaic/config.toml.
//
// The wizard is callable from two hosts:
//
//   - the main TUI Model (reachable from the new-session wizard's
//     setup selector via "+ create new recipe")
//   - a standalone `mosaic worktree setup new` CLI (see
//     cmd/mosaic/cmd_worktree.go)
//
// Both hosts embed recipeWizardState and call updateRecipeWizardKey /
// renderRecipeWizard. The save path runs synchronously via
// [configwriter.UpsertWorktreeSetup] — the recipe TOML is small and
// the write is atomic, so there is no reason to ferry the result
// through a tea.Cmd.
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/config/configwriter"
	"github.com/Mizune/mosaic/internal/mosaicpath"
)

// recipeStep enumerates the pages of the recipe wizard.
type recipeStep int

const (
	recipeStepName recipeStep = iota
	recipeStepCopy
	recipeStepToggles
	recipeStepCommands
	recipeStepHook
	recipeStepEnv
	recipeStepPreview
)

// recipeWizardAction is the outcome of a single key press, reported
// back to the host so it can decide whether to dismiss the wizard,
// return to the parent screen, or keep the wizard on screen.
type recipeWizardAction int

const (
	recipeActionNone recipeWizardAction = iota
	recipeActionCancelled
	recipeActionSaved
)

// Named indices for the two-row toggle step. Using named constants
// rather than bare 0/1 keeps the intent obvious at every call site.
const (
	toggleIdxCopyIgnored   = 0
	toggleIdxCopyUntracked = 1
	toggleIdxMax           = toggleIdxCopyUntracked
)

// listEditor groups the mutable state for a single list-editor step
// (Copy / Commands / Env). Bundling lines/cursor/editing/buf into one
// struct keeps helper signatures to a single `*listEditor` instead of
// four separately aliased pointers.
type listEditor struct {
	lines   []string
	cursor  int
	editing bool
	buf     string
}

// recipeSaver persists a completed recipe. Hosts inject one at
// construction so the wizard does not depend on any concrete
// persistence package. The default (wired in [openRecipeWizard]) is
// [configwriter.UpsertWorktreeSetup].
type recipeSaver func(path, name string, recipe config.WorktreeSetup) error

// recipeSaveResultMsg carries the outcome of an async save back into
// the Bubble Tea update loop. Hosts pipe it through
// [applyRecipeSaveResult] to update wizard state.
type recipeSaveResultMsg struct {
	name string
	path string
	err  error
}

// recipeWizardState is the entire state of the recipe-building
// wizard. It is owned by whatever host is driving the UI (either
// Model.recipeWizard or the standalone CLI model).
type recipeWizardState struct {
	// active signals the host is currently rendering the wizard.
	// Hosts flip this in openRecipeWizard / after a save or cancel.
	active bool

	step recipeStep

	// projectPath is the absolute path to the repo the wizard is
	// building a recipe for. Drives save location, existence checks
	// on the Preview step, and lockfile detection on the Commands
	// step.
	projectPath string

	// savePath is the absolute path to the .mosaic/config.toml we
	// will write on Preview-step Enter. Defaults to
	// mosaicpath.ProjectConfigFile(projectPath) but exposed so tests
	// can redirect it.
	savePath string

	// existingNames is the set of recipe names already defined in
	// merged config. Used to warn about overwrites on the Name step.
	existingNames map[string]struct{}

	// --- Recipe data ------------------------------------------
	name          string
	copyIgnored   bool
	copyUntracked bool
	hook          string

	// --- Editor state (per list) ----------------------------------
	// copy / commands / env each bundle lines+cursor+editing+buf so
	// the list editor helpers can take a single *listEditor. env.lines
	// holds "KEY=VAL" rows.
	copy     listEditor
	commands listEditor
	env      listEditor

	// toggleCursor selects the active row on the two-row toggle
	// step. See toggleIdxCopyIgnored / toggleIdxCopyUntracked.
	toggleCursor int

	// --- Computed previews --------------------------------------
	lockHints   []string        // suggestions derived from repo lockfiles
	missingCopy map[string]bool // Copy entries that do not exist in projectPath
	previewTOML string          // pre-rendered TOML block for the Preview step

	// --- Diagnostics --------------------------------------------
	confirmation string // inline error / warning
	saved        bool   // true after a successful save (Preview page flips to "done")
	saving       bool   // true while the async save is in flight

	// saver persists a completed recipe. Defaults to
	// [configwriter.UpsertWorktreeSetup] when [openRecipeWizard] is
	// used; tests can swap in a fake via
	// [openRecipeWizardWithSaver].
	saver recipeSaver
}

// openRecipeWizard initialises a recipeWizardState with the default
// saver ([configwriter.UpsertWorktreeSetup]). projectPath must be an
// absolute directory. The initial Copy list seeds from the default
// recipe so users accepting every default get a useful starting
// point.
func openRecipeWizard(projectPath string, existingNames map[string]struct{}) recipeWizardState {
	return openRecipeWizardWithSaver(projectPath, existingNames, configwriter.UpsertWorktreeSetup)
}

// openRecipeWizardWithSaver is the testing-friendly variant that
// lets the caller supply a saver. Passing nil falls back to the
// default (real configwriter) saver.
func openRecipeWizardWithSaver(projectPath string, existingNames map[string]struct{}, saver recipeSaver) recipeWizardState {
	if saver == nil {
		saver = configwriter.UpsertWorktreeSetup
	}
	// Seed the Copy list with paths that most projects want copied
	// into a worktree: environment files (.env / .env.local) and
	// editor settings (.vscode/ / .cursor/). The user can add or
	// remove entries on the Copy step.
	seed := []string{".env", ".env.local", ".vscode/", ".cursor/"}
	return recipeWizardState{
		active:        true,
		step:          recipeStepName,
		projectPath:   projectPath,
		savePath:      mosaicpath.ProjectConfigFile(projectPath),
		existingNames: existingNames,
		copy:          listEditor{lines: append([]string(nil), seed...)},
		commands:      listEditor{lines: []string{}},
		env:           listEditor{lines: []string{}},
		lockHints:     detectLockfileCommands(projectPath),
		saver:         saver,
	}
}

// toRecipe folds wizard state into a [config.WorktreeSetup] value
// ready to hand to configwriter.
func (s *recipeWizardState) toRecipe() config.WorktreeSetup {
	recipe := config.WorktreeSetup{
		CopyIgnored:    s.copyIgnored,
		CopyUntracked:  s.copyUntracked,
		PostCreateHook: strings.TrimSpace(s.hook),
	}
	recipe.Copy = cleanStringList(s.copy.lines)
	recipe.Commands = cleanStringList(s.commands.lines)
	if env := parseEnvLines(s.env.lines); len(env) > 0 {
		recipe.Env = env
	}
	return recipe
}

// renderRecipeTOML serialises a single recipe as the TOML block the
// writer would emit, so the Preview step can show the user exactly
// what is about to land in config.toml. Delegating to
// configwriter.RenderFragment guarantees the preview matches the
// on-disk output byte-for-byte — any future encoder tweaks (escaping,
// multi-line strings, etc.) land in both places simultaneously.
func renderRecipeTOML(name string, recipe config.WorktreeSetup) string {
	b, err := configwriter.RenderFragment("worktree.setup."+name, recipe)
	if err != nil {
		return "(failed to render preview: " + err.Error() + ")"
	}
	return string(b)
}

// cleanStringList drops empty / whitespace-only entries so the user
// is not punished for leaving a blank row in the editor.
func cleanStringList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseEnvLines turns "KEY=VALUE" editor rows into a map. Rows that
// do not contain a "=" are skipped so a half-typed line does not
// crash the encoder.
func parseEnvLines(in []string) map[string]string {
	out := make(map[string]string)
	for _, line := range in {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		out[key] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// --- Filesystem probes (pragmatically placed here) ----------------
//
// detectLockfileCommands, computeMissingCopy and fileExists are pure
// I/O probes that could live in a domain package (e.g.
// internal/worktree/recipehints) if they grow or gain callers
// outside the wizard. For now they stay co-located with the wizard
// that consumes them.

// detectLockfileCommands scans projectPath for common package
// manager lockfiles and returns a list of suggested install
// commands. It is purely informational — the Commands step never
// auto-inserts these; the user has to type them.
func detectLockfileCommands(projectPath string) []string {
	if projectPath == "" {
		return nil
	}
	type probe struct {
		file string
		cmd  string
	}
	probes := []probe{
		{"pnpm-lock.yaml", "pnpm install"},
		{"yarn.lock", "yarn install --frozen-lockfile"},
		{"package-lock.json", "npm ci"},
		{"bun.lockb", "bun install"},
		{"go.mod", "go mod download"},
		{"Cargo.toml", "cargo fetch"},
		{"requirements.txt", "pip install -r requirements.txt"},
		{"Pipfile", "pipenv install --deploy"},
		{"pyproject.toml", "uv sync"},
		{"Gemfile", "bundle install"},
		{"composer.json", "composer install"},
	}
	var out []string
	for _, p := range probes {
		if fileExists(filepath.Join(projectPath, p.file)) {
			out = append(out, p.cmd)
		}
	}
	return out
}

// fileExists reports whether a regular file or directory is present
// at path. Errors other than "does not exist" are treated as "not
// present" so a permission hiccup does not crash the wizard.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// --- Key handling ---------------------------------------------------------

// updateRecipeWizardKey dispatches one key press to the wizard and
// returns (action, cmd). `action` tells the host what transition to
// perform (cancel / saved); `cmd` is a tea.Cmd the host should run
// (only non-nil when the Preview step kicks off an async save).
//
// The function never calls tea.Quit or reassigns screens itself;
// hosts translate actions and the resulting
// [recipeSaveResultMsg] into their own screen state so the wizard
// remains reusable between the big Model and the standalone CLI
// model.
func updateRecipeWizardKey(s *recipeWizardState, msg tea.KeyMsg) (recipeWizardAction, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+[" {
		key = "esc"
	}

	// While a save is in flight, swallow every key except ctrl+c
	// (handled at the host level) so the user cannot double-submit
	// or navigate away mid-write.
	if s.saving {
		return recipeActionNone, nil
	}

	// Any key press clears a stale confirmation banner so the user
	// isn't left wondering why it's still there after they've moved
	// on.
	if key != "" && s.confirmation != "" && key != "enter" {
		s.confirmation = ""
	}

	// The saved=true screen has one keybinding: anything dismisses.
	if s.saved {
		return recipeActionSaved, nil
	}

	// Editing modes own every key until Enter commits or Esc cancels.
	switch {
	case s.step == recipeStepCopy && s.copy.editing:
		return updateRecipeListEdit(&s.copy, key), nil
	case s.step == recipeStepCommands && s.commands.editing:
		return updateRecipeListEdit(&s.commands, key), nil
	case s.step == recipeStepEnv && s.env.editing:
		return updateRecipeListEdit(&s.env, key), nil
	}

	if key == "esc" {
		return recipeActionCancelled, nil
	}

	if key == "shift+tab" && s.step > recipeStepName {
		s.step--
		return recipeActionNone, nil
	}

	switch s.step {
	case recipeStepName:
		return updateRecipeName(s, key), nil
	case recipeStepCopy:
		return updateRecipeCopy(s, key), nil
	case recipeStepToggles:
		return updateRecipeToggles(s, key), nil
	case recipeStepCommands:
		return updateRecipeCommands(s, key), nil
	case recipeStepHook:
		return updateRecipeHook(s, key), nil
	case recipeStepEnv:
		return updateRecipeEnv(s, key), nil
	case recipeStepPreview:
		return updateRecipePreview(s, key)
	}
	return recipeActionNone, nil
}

// applyRecipeSaveResult folds the async save outcome into the
// wizard state. Hosts call this when they receive a
// [recipeSaveResultMsg] on their Update loop. The returned action
// tells the host what to do next (transition or remain).
func applyRecipeSaveResult(s *recipeWizardState, msg recipeSaveResultMsg) recipeWizardAction {
	s.saving = false
	if msg.err != nil {
		s.confirmation = "save failed: " + msg.err.Error()
		return recipeActionNone
	}
	s.saved = true
	s.confirmation = ""
	return recipeActionSaved
}

func updateRecipeName(s *recipeWizardState, key string) recipeWizardAction {
	switch key {
	case "enter", "tab":
		name := strings.TrimSpace(s.name)
		if name == "" {
			s.confirmation = "recipe name cannot be empty"
			return recipeActionNone
		}
		if !isValidRecipeName(name) {
			s.confirmation = "recipe name may only contain letters, digits, '-' and '_'"
			return recipeActionNone
		}
		s.name = name
		s.step = recipeStepCopy
		s.copy.cursor = 0
		return recipeActionNone
	case "backspace":
		s.name = dropLastRune(s.name)
		return recipeActionNone
	case "ctrl+u":
		s.name = ""
		return recipeActionNone
	}
	if isPrintable(key) {
		s.name += key
	}
	return recipeActionNone
}

func updateRecipeCopy(s *recipeWizardState, key string) recipeWizardAction {
	return updateRecipeList(&s.copy, key, func() { s.step = recipeStepToggles })
}

func updateRecipeToggles(s *recipeWizardState, key string) recipeWizardAction {
	switch key {
	case "j", "down":
		if s.toggleCursor < toggleIdxMax {
			s.toggleCursor++
		}
	case "k", "up":
		if s.toggleCursor > 0 {
			s.toggleCursor--
		}
	case " ":
		switch s.toggleCursor {
		case toggleIdxCopyIgnored:
			s.copyIgnored = !s.copyIgnored
		case toggleIdxCopyUntracked:
			s.copyUntracked = !s.copyUntracked
		}
	case "enter", "tab":
		s.step = recipeStepCommands
		s.commands.cursor = 0
	}
	return recipeActionNone
}

func updateRecipeCommands(s *recipeWizardState, key string) recipeWizardAction {
	return updateRecipeList(&s.commands, key, func() { s.step = recipeStepHook })
}

func updateRecipeHook(s *recipeWizardState, key string) recipeWizardAction {
	switch key {
	case "enter", "tab":
		s.hook = strings.TrimSpace(s.hook)
		s.step = recipeStepEnv
		s.env.cursor = 0
		return recipeActionNone
	case "backspace":
		s.hook = dropLastRune(s.hook)
		return recipeActionNone
	case "ctrl+u":
		s.hook = ""
		return recipeActionNone
	}
	if isPrintable(key) {
		s.hook += key
	}
	return recipeActionNone
}

func updateRecipeEnv(s *recipeWizardState, key string) recipeWizardAction {
	return updateRecipeList(&s.env, key, func() {
		s.step = recipeStepPreview
		s.previewTOML = renderRecipeTOML(s.name, s.toRecipe())
		s.missingCopy = computeMissingCopy(s.projectPath, s.copy.lines)
	})
}

func updateRecipePreview(s *recipeWizardState, key string) (recipeWizardAction, tea.Cmd) {
	if key != "enter" {
		return recipeActionNone, nil
	}
	s.saving = true
	s.confirmation = "saving…"
	return recipeActionNone, recipeSaveCmd(s.saver, s.savePath, s.name, s.toRecipe())
}

// recipeSaveCmd runs the injected saver off the Bubble Tea goroutine
// and wraps the outcome in a [recipeSaveResultMsg] so hosts can feed
// it back into [applyRecipeSaveResult] on their Update loop.
func recipeSaveCmd(saver recipeSaver, path, name string, recipe config.WorktreeSetup) tea.Cmd {
	return func() tea.Msg {
		err := saver(path, name, recipe)
		return recipeSaveResultMsg{name: name, path: path, err: err}
	}
}

// --- List editing helpers --------------------------------------------------

// updateRecipeList handles key input on a list-editor step (Copy /
// Commands / Env). When the user presses Enter on the last row it
// advances to the next step via advance().
func updateRecipeList(ed *listEditor, key string, advance func()) recipeWizardAction {
	switch key {
	case "j", "down":
		if ed.cursor < len(ed.lines) {
			ed.cursor++
		}
	case "k", "up":
		if ed.cursor > 0 {
			ed.cursor--
		}
	case "a":
		// Add a blank row below the cursor and drop into edit mode.
		insertAt := ed.cursor
		if insertAt > len(ed.lines) {
			insertAt = len(ed.lines)
		}
		// Splice in a blank row at insertAt, shifting the tail right by one.
		tail := append([]string{""}, ed.lines[insertAt:]...)
		ed.lines = append(ed.lines[:insertAt], tail...)
		ed.cursor = insertAt
		ed.editing = true
		ed.buf = ""
	case "d":
		if ed.cursor >= 0 && ed.cursor < len(ed.lines) {
			ed.lines = append(ed.lines[:ed.cursor], ed.lines[ed.cursor+1:]...)
			if ed.cursor >= len(ed.lines) && ed.cursor > 0 {
				ed.cursor--
			}
		}
	case "i":
		if ed.cursor >= 0 && ed.cursor < len(ed.lines) {
			ed.editing = true
			ed.buf = ed.lines[ed.cursor]
		}
	case "enter":
		if ed.cursor >= 0 && ed.cursor < len(ed.lines) {
			ed.editing = true
			ed.buf = ed.lines[ed.cursor]
			return recipeActionNone
		}
		advance()
	case "tab":
		advance()
	}
	return recipeActionNone
}

// updateRecipeListEdit handles key input while a list entry is
// being edited in place.
func updateRecipeListEdit(ed *listEditor, key string) recipeWizardAction {
	switch key {
	case "enter":
		if ed.cursor < 0 || ed.cursor >= len(ed.lines) {
			ed.editing = false
			return recipeActionNone
		}
		ed.lines[ed.cursor] = ed.buf
		ed.editing = false
	case "esc":
		ed.editing = false
	case "backspace":
		ed.buf = dropLastRune(ed.buf)
	case "ctrl+u":
		ed.buf = ""
	default:
		if isPrintable(key) {
			ed.buf += key
		}
	}
	return recipeActionNone
}

// isPrintable reports whether a Bubble Tea key string represents a
// single printable character we should append to a text buffer. It
// accepts multi-byte UTF-8 so users can type non-ASCII (e.g. CJK,
// accented Latin) into any wizard text field — those all decode to
// exactly one rune.
//
// Multi-character key names like "up", "down", "home", "pgup", or
// "shift+tab" are rejected here so navigation keys never leak into
// text buffers as literal text. Control keys like "esc", "enter",
// "tab" are also rejected (handled by explicit case arms above).
func isPrintable(key string) bool {
	if utf8.RuneCountInString(key) != 1 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(key)
	// Space is printable by unicode.IsPrint; tab/newline are not,
	// which matches the previous ASCII-only behavior.
	return unicode.IsPrint(r)
}

// dropLastRune returns s with its final rune removed, or "" when s
// is empty. Used by text-input backspace handlers so multi-byte
// characters are deleted whole rather than byte-by-byte.
func dropLastRune(s string) string {
	if s == "" {
		return s
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

// isValidRecipeName enforces a conservative name charset so the
// generated TOML key does not need quoting.
func isValidRecipeName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// computeMissingCopy returns the set of Copy entries that are not
// present in projectPath. Empty strings are ignored.
func computeMissingCopy(projectPath string, copyLines []string) map[string]bool {
	out := map[string]bool{}
	for _, rel := range copyLines {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		if !fileExists(filepath.Join(projectPath, rel)) {
			out[rel] = true
		}
	}
	return out
}

