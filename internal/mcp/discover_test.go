package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Mizune/mosaic/internal/mcp"
)

func TestDiscover_ParsesSingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(path, []byte(`{
		"numStartups": 1,
		"mcpServers": {
			"github": {
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-github"],
				"env": {"GITHUB_PERSONAL_ACCESS_TOKEN": "super-secret-value"}
			},
			"codex": {
				"type": "stdio",
				"command": "/opt/homebrew/bin/codex",
				"args": ["mcp-server"],
				"env": {}
			}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := mcp.Discover(path, "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("want 2 servers, got %d", len(servers))
	}

	// Sorted by name: codex, github
	if servers[0].Name != "codex" || servers[1].Name != "github" {
		t.Errorf("unexpected order: %+v", servers)
	}

	gh := servers[1]
	if gh.Command != "npx" || len(gh.Args) != 2 {
		t.Errorf("github command/args mismatch: %+v", gh)
	}
	if len(gh.EnvKeys) != 1 || gh.EnvKeys[0] != "GITHUB_PERSONAL_ACCESS_TOKEN" {
		t.Errorf("env keys mismatch: %+v", gh.EnvKeys)
	}
}

func TestDiscover_NeverLeaksEnvValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(path, []byte(`{
		"mcpServers": {
			"gh": {
				"command": "npx",
				"env": {"TOKEN": "VALUE_THAT_MUST_NEVER_APPEAR"}
			}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := mcp.Discover(path, "")
	if err != nil {
		t.Fatal(err)
	}
	// Walk everything string-like in the returned struct and make
	// sure the secret value never shows up.
	for _, s := range servers {
		if s.Name == "VALUE_THAT_MUST_NEVER_APPEAR" ||
			s.Command == "VALUE_THAT_MUST_NEVER_APPEAR" ||
			s.Type == "VALUE_THAT_MUST_NEVER_APPEAR" {
			t.Fatalf("secret leaked via non-env field: %+v", s)
		}
		for _, a := range s.Args {
			if a == "VALUE_THAT_MUST_NEVER_APPEAR" {
				t.Fatalf("secret leaked via args: %+v", s)
			}
		}
		for _, k := range s.EnvKeys {
			if k == "VALUE_THAT_MUST_NEVER_APPEAR" {
				t.Fatalf("EnvKeys should carry names only, not values")
			}
		}
	}
}

func TestDiscover_MissingFileIsNotAnError(t *testing.T) {
	servers, err := mcp.Discover(filepath.Join(t.TempDir(), "nope.json"), "")
	if err != nil {
		t.Fatalf("missing file should be silent: %v", err)
	}
	if len(servers) != 0 {
		t.Fatalf("want empty, got %+v", servers)
	}
}

func TestDiscover_SettingsOverridesClaudeJSON(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, "claude.json")
	settingsJSON := filepath.Join(dir, "settings.json")

	// claude.json declares "shared" with command=A.
	os.WriteFile(claudeJSON, []byte(`{
		"mcpServers": { "shared": { "command": "A" } }
	}`), 0o644)
	// settings.json declares the same name with command=B — the
	// override behaviour.
	os.WriteFile(settingsJSON, []byte(`{
		"mcpServers": { "shared": { "command": "B" } }
	}`), 0o644)

	servers, err := mcp.Discover(claudeJSON, settingsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Command != "B" {
		t.Fatalf("settings.json should win: %+v", servers)
	}
}

func TestDiscover_MalformedEntrySkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")
	os.WriteFile(path, []byte(`{
		"mcpServers": {
			"broken": 42,
			"ok":     { "command": "foo" }
		}
	}`), 0o644)

	servers, err := mcp.Discover(path, "")
	if err != nil {
		t.Fatalf("a single bad entry should not fail discovery: %v", err)
	}
	if len(servers) != 1 || servers[0].Name != "ok" {
		t.Fatalf("want only the ok entry, got %+v", servers)
	}
}
