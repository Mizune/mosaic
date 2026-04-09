// Package mcp discovers Claude Code's globally configured MCP
// servers and exposes them as typed records mosaic can present in
// the TUI.
//
// The source of truth is ~/.claude.json (Claude Code's top-level
// config file), which has a "mcpServers" map: server name → command
// object. Some installations additionally put MCP entries into
// ~/.claude/settings.json; when both files are present mosaic takes
// the union, with settings.json winning on name conflicts.
//
// The discover step is intentionally read-only and security-aware:
// env values (often personal access tokens) are NEVER returned to
// the caller. Only the env var *keys* travel back so the UI can
// show "this server depends on $GITHUB_PAT" without leaking the
// secret.
package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
)

// Server is a discovered MCP server. Fields mirror Claude Code's
// schema but omit secret values.
type Server struct {
	// Name is the map key in mcpServers.
	Name string

	// Source identifies which file the server came from, used by
	// the UI to explain precedence ("declared in ~/.claude.json").
	Source string

	// Command is the executable Claude Code would launch. Empty
	// when the config object was malformed, which the UI can
	// surface as a warning rather than a hard error.
	Command string

	// Args is the argv list passed to Command.
	Args []string

	// Type is Claude Code's transport hint ("stdio" by default).
	Type string

	// EnvKeys lists the environment variable *names* the server
	// declares. Values are stripped so secrets never leave this
	// package.
	EnvKeys []string
}

// Discover loads and merges MCP servers from every known source.
// Paths may be absolute or relative; a missing file is treated as
// "no servers from this source" rather than an error so mosaic
// still runs on machines without a global Claude config.
//
// The returned slice is sorted by Name for deterministic rendering.
func Discover(homeClaudeJSON, settingsJSON string) ([]Server, error) {
	all := map[string]Server{}

	if homeClaudeJSON != "" {
		servers, err := loadFromFile(homeClaudeJSON, "~/.claude.json")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		for _, s := range servers {
			all[s.Name] = s
		}
	}
	if settingsJSON != "" {
		servers, err := loadFromFile(settingsJSON, "~/.claude/settings.json")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		// settings.json overrides ~/.claude.json on name conflict.
		for _, s := range servers {
			all[s.Name] = s
		}
	}

	out := make([]Server, 0, len(all))
	for _, s := range all {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// loadFromFile reads path, extracts "mcpServers", and converts each
// entry to a [Server]. Unknown or malformed entries are skipped so a
// single bad server never blocks discovery of the healthy ones.
func loadFromFile(path, source string) ([]Server, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	out := make([]Server, 0, len(envelope.McpServers))
	for name, raw := range envelope.McpServers {
		s, ok := parseServer(name, raw, source)
		if !ok {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// parseServer decodes one MCP server config object, stripping every
// secret (env values) before returning it.
func parseServer(name string, raw json.RawMessage, source string) (Server, bool) {
	var cfg struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		Type    string            `json:"type"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Server{}, false
	}
	envKeys := make([]string, 0, len(cfg.Env))
	for k := range cfg.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	return Server{
		Name:    name,
		Source:  source,
		Command: cfg.Command,
		Args:    cfg.Args,
		Type:    cfg.Type,
		EnvKeys: envKeys,
	}, true
}
