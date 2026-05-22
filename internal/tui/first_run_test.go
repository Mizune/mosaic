package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestFirstRunWizard_WritesMarkerOnFinish drives the wizard
// through every step and asserts that the sentinel marker file
// is written at the end. This is the happy path — it also
// verifies the theme selection actually lands on the Model.
func TestFirstRunWizard_WritesMarkerOnFinish(t *testing.T) {
	m := setupModel(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, ".first-run-done")
	settings := filepath.Join(dir, "settings.json") // non-existent → skipped

	m.ShowFirstRunWizard(marker, settings)

	if m.screen != ScreenFirstRun {
		t.Fatalf("expected ScreenFirstRun, got %v", m.screen)
	}
	if !m.firstRun.active {
		t.Fatal("wizard should be active")
	}

	// Step through: welcome → theme → hooks → done → finish.
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	// Welcome
	m.updateFirstRunWizard(enter)
	if m.firstRun.step != firstRunStepTheme {
		t.Fatalf("step = %v, want theme", m.firstRun.step)
	}
	// Theme: pick the first entry with Enter.
	m.updateFirstRunWizard(enter)
	if m.firstRun.step != firstRunStepHooks {
		t.Fatalf("step = %v, want hooks", m.firstRun.step)
	}
	if m.firstRun.themeApplied == "" {
		t.Error("theme should have been applied")
	}
	// Hooks: install nothing, then continue.
	m.firstRun.installClaudeHook = false
	m.firstRun.installCodexHook = false
	m.firstRun.hookCursor = 2
	m.updateFirstRunWizard(enter)
	if m.firstRun.step != firstRunStepDone {
		t.Fatalf("step = %v, want done", m.firstRun.step)
	}
	// Done: Enter finishes.
	m.updateFirstRunWizard(enter)
	if m.screen == ScreenFirstRun {
		t.Fatal("wizard should have closed after Done Enter")
	}

	// Marker file must exist now.
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker file not created: %v", err)
	}
}

func userHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestFirstRunWizard_InstallsCodexHooksWhenDefaultToolIsCodex(t *testing.T) {
	m := setupModel(t)
	m.config.Mosaic.DefaultTool = "codex"
	dir := t.TempDir()
	marker := filepath.Join(dir, ".first-run-done")
	settings := filepath.Join(dir, "hooks.json")

	m.ShowFirstRunWizard(marker, settings)
	m.firstRun.installClaudeHook = false
	m.firstRun.installCodexHook = true

	enter := tea.KeyMsg{Type: tea.KeyEnter}
	m.updateFirstRunWizard(enter) // welcome
	m.updateFirstRunWizard(enter) // theme
	m.updateFirstRunWizard(enter) // hooks: default yes

	body, err := os.ReadFile(settings)
	if err != nil {
		t.Fatalf("codex hooks file not written: %v", err)
	}
	if !strings.Contains(string(body), "PermissionRequest") {
		t.Fatalf("expected Codex hook set, got:\n%s", string(body))
	}
}

func TestFirstRunWizard_CanInstallBothHookSets(t *testing.T) {
	m := setupModel(t)
	m.config.Mosaic.DefaultTool = "claude"
	dir := t.TempDir()
	home := userHome(t)
	marker := filepath.Join(dir, ".first-run-done")
	claudeSettings := filepath.Join(dir, "settings.json")

	m.ShowFirstRunWizard(marker, claudeSettings)
	m.firstRun.installClaudeHook = true

	enter := tea.KeyMsg{Type: tea.KeyEnter}
	m.updateFirstRunWizard(enter) // welcome
	m.updateFirstRunWizard(enter) // theme
	m.updateFirstRunWizard(tea.KeyMsg{Type: tea.KeyDown})
	m.firstRun.installCodexHook = true
	m.updateFirstRunWizard(enter) // hooks

	claudeBody, err := os.ReadFile(claudeSettings)
	if err != nil {
		t.Fatalf("claude hooks file not written: %v", err)
	}
	if !strings.Contains(string(claudeBody), "Notification") {
		t.Fatalf("expected Claude hook set, got:\n%s", string(claudeBody))
	}

	codexPath := firstRunHookPath(home, "codex")
	codexBody, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatalf("codex hooks file not written: %v", err)
	}
	if !strings.Contains(string(codexBody), "PermissionRequest") {
		t.Fatalf("expected Codex hook set, got:\n%s", string(codexBody))
	}
}

// TestFirstRunWizard_EscPersistsMarker reproduces the "user
// pressed Esc" path: the wizard still writes the marker so the
// user does not get prompted again on the next launch.
func TestFirstRunWizard_EscPersistsMarker(t *testing.T) {
	m := setupModel(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, ".first-run-done")
	m.ShowFirstRunWizard(marker, "")

	m.updateFirstRunWizard(tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen == ScreenFirstRun {
		t.Fatal("Esc should close the wizard")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker should exist after Esc: %v", err)
	}
}
