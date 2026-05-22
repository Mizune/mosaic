package hook_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mizune/mosaic/internal/hook"
)

func TestInstall_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	inst := hook.NewInstaller(path)

	if err := inst.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), hook.MarkerEnvVar) {
		t.Fatalf("expected installed file to contain %s, got:\n%s", hook.MarkerEnvVar, string(body))
	}

	got, err := inst.IsInstalled()
	if err != nil || !got {
		t.Fatalf("IsInstalled = %v, %v; want true", got, err)
	}
}

func TestInstall_PreservesExistingHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "tool == \"Bash\"", "hooks": [{"type": "command", "command": "user-tool"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	inst := hook.NewInstaller(path)
	if err := inst.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "user-tool") {
		t.Fatalf("user PreToolUse command was lost:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, hook.MarkerEnvVar) {
		t.Fatalf("mosaic markers missing:\n%s", bodyStr)
	}
}

func TestInstall_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	inst := hook.NewInstaller(path)

	// First install: capture how many command entries result.
	if err := inst.Install(); err != nil {
		t.Fatalf("install 0: %v", err)
	}
	first := countCommandEntries(t, path)

	// Subsequent installs must not grow the entry count.
	for i := 1; i < 3; i++ {
		if err := inst.Install(); err != nil {
			t.Fatalf("install %d: %v", i, err)
		}
		got := countCommandEntries(t, path)
		if got != first {
			t.Fatalf("install %d: entry count drifted from %d to %d", i, first, got)
		}
	}
	if first != len(hook.MosaicHookSet()) {
		t.Fatalf("expected %d mosaic command entries, got %d", len(hook.MosaicHookSet()), first)
	}
}

func TestInstallSet_CodexHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	inst := hook.NewInstaller(path)

	if err := inst.InstallSetSync(hook.CodexHookSet()); err != nil {
		t.Fatalf("install codex: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	bodyStr := string(body)
	for _, event := range []string{"PermissionRequest", "PreToolUse", "PostToolUse", "Stop", "UserPromptSubmit"} {
		if !strings.Contains(bodyStr, event) {
			t.Fatalf("expected Codex event %q in hooks file:\n%s", event, bodyStr)
		}
	}
	if !strings.Contains(bodyStr, "codex") {
		t.Fatalf("expected codex notification command:\n%s", bodyStr)
	}
	if strings.Contains(bodyStr, `"async"`) {
		t.Fatalf("codex hooks must not include async:\n%s", bodyStr)
	}
}

// countCommandEntries counts how many JSON command entries in the
// settings file are mosaic-owned (i.e. have the marker env var in
// their command string).
func countCommandEntries(t *testing.T, path string) int {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("parse: %v", err)
	}
	hooks, _ := root["hooks"].(map[string]any)
	n := 0
	for _, val := range hooks {
		groups, _ := val.([]any)
		for _, g := range groups {
			obj, _ := g.(map[string]any)
			hs, _ := obj["hooks"].([]any)
			for _, h := range hs {
				hObj, _ := h.(map[string]any)
				if cmd, _ := hObj["command"].(string); strings.Contains(cmd, hook.MarkerEnvVar) {
					n++
				}
			}
		}
	}
	return n
}

func TestUninstall_StripsOnlyMosaicCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	inst := hook.NewInstaller(path)

	original := `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "tool == \"Bash\"", "hooks": [{"type": "command", "command": "user-tool"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := inst.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := inst.Uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	body, _ := os.ReadFile(path)
	bodyStr := string(body)
	if strings.Contains(bodyStr, hook.MarkerEnvVar) {
		t.Fatalf("mosaic command still present after uninstall:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "user-tool") {
		t.Fatalf("user-tool was wrongly removed:\n%s", bodyStr)
	}

	// Make sure the JSON is still valid.
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("settings.json no longer valid JSON: %v", err)
	}
}
