package service

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/Mizune/mosaic/internal/mcp"
	"github.com/Mizune/mosaic/internal/store"
)

// prepareLaunch applies per-session attachment intent to the agent
// launch argv/env. It is intentionally additive: user-owned agent
// config files stay untouched, and regular launch flags such as
// Codex model/sandbox/approval mode remain owned by tool_launchers.go.
func (s *SessionService) prepareLaunch(ctx context.Context, row store.Session, argv []string) ([]string, map[string]string, error) {
	if len(argv) == 0 {
		return argv, nil, nil
	}
	if store.SessionTool(row.Tool) != store.ToolCodex {
		return argv, nil, nil
	}
	args, err := s.codexAttachmentArgs(ctx, row)
	if err != nil {
		return nil, nil, err
	}
	if len(args) == 0 {
		return argv, nil, nil
	}
	return insertCodexConfigArgs(argv, args), nil, nil
}

func (s *SessionService) codexAttachmentArgs(ctx context.Context, row store.Session) ([]string, error) {
	var out []string

	mcpRows, err := s.store.SessionMCPs().ForSession(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	if len(mcpRows) > 0 {
		servers, err := mcp.DiscoverCodex(codexConfigPaths(row.WorktreePath)...)
		if err != nil {
			return nil, err
		}
		attachedNames := make(map[string]bool, len(mcpRows))
		for _, attached := range mcpRows {
			attachedNames[attached.MCPName] = true
		}
		for _, server := range servers {
			if !attachedNames[server.Name] {
				continue
			}
			out = append(out, codexMCPConfigArgs(server)...)
		}
	}

	skillRows, err := s.store.SessionSkills().ForSession(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	skillNames := make([]string, 0, len(skillRows))
	for _, attached := range skillRows {
		skillNames = append(skillNames, attached.SkillName)
	}
	sort.Strings(skillNames)
	for _, name := range skillNames {
		out = append(out, "-c", codexConfigKey("skills", name, "enabled")+"=true")
	}
	return out, nil
}

func codexConfigPaths(worktreePath string) []string {
	var out []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = append(out, filepath.Join(home, ".codex", "config.toml"))
	}
	if worktreePath != "" {
		out = append(out, filepath.Join(worktreePath, ".codex", "config.toml"))
	}
	return out
}

func codexMCPConfigArgs(server mcp.Server) []string {
	key := func(field string) string {
		return codexConfigKey("mcp_servers", server.Name, field)
	}
	var out []string
	if server.Command != "" {
		out = append(out, "-c", key("command")+"="+tomlString(server.Command))
	}
	if len(server.Args) > 0 {
		out = append(out, "-c", key("args")+"="+tomlStringArray(server.Args))
	}
	if server.Type != "" {
		out = append(out, "-c", key("type")+"="+tomlString(server.Type))
	}
	return out
}

func codexConfigKey(parts ...string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += "."
		}
		if i == 1 {
			out += strconv.Quote(part)
		} else {
			out += part
		}
	}
	return out
}

func tomlString(s string) string {
	return strconv.Quote(s)
}

func tomlStringArray(values []string) string {
	out := "["
	for i, value := range values {
		if i > 0 {
			out += ", "
		}
		out += tomlString(value)
	}
	out += "]"
	return out
}

func insertCodexConfigArgs(argv, configArgs []string) []string {
	if len(configArgs) == 0 || len(argv) == 0 || argv[0] != "codex" {
		return argv
	}
	insertAt := 1
	if len(argv) > 1 && argv[1] == "resume" {
		insertAt = 2
	}
	out := make([]string, 0, len(argv)+len(configArgs))
	out = append(out, argv[:insertAt]...)
	out = append(out, configArgs...)
	out = append(out, argv[insertAt:]...)
	return out
}

func mergeEnv(dst, src map[string]string) map[string]string {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
