package tui

import (
	"os"
	"path/filepath"
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
	// Hooks: select "No, install later" (cursor = 1).
	m.firstRun.hookCursor = 1
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
