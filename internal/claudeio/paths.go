// Package claudeio knows how to find, parse and watch the JSONL
// transcripts that Claude Code writes under ~/.claude/projects/. It is
// the package that turns "the Claude session for this worktree" into a
// stream of typed events the rest of mosaic can consume.
//
// The mapping between an absolute project path and the directory
// Claude Code uses is:
//
//	/home/you/Projects/mosaic   →   -home-you-Projects-mosaic
//
// i.e. forward slashes are replaced with dashes and a leading dash is
// preserved. The directory contains one <sessionId>.jsonl per Claude
// session and one <sessionId>/subagents/agent-<id>.jsonl per
// background sub-agent the parent session spawned.
package claudeio

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Sanitize converts an absolute project path into the directory name
// Claude Code uses inside ~/.claude/projects.
func Sanitize(absPath string) string {
	clean := filepath.Clean(absPath)
	return strings.ReplaceAll(clean, string(filepath.Separator), "-")
}

// ProjectsRoot returns ~/.claude/projects (or the override
// CLAUDE_HOME / $HOME/.claude/projects equivalent).
func ProjectsRoot(home string) string {
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	return filepath.Join(home, ".claude", "projects")
}

// ProjectDir returns the on-disk directory Claude Code uses for the
// given project path.
func ProjectDir(home, projectPath string) string {
	return filepath.Join(ProjectsRoot(home), Sanitize(projectPath))
}

// SessionTranscriptPath returns the absolute path to the parent JSONL
// for one Claude session under the project.
func SessionTranscriptPath(home, projectPath, sessionID string) string {
	return filepath.Join(ProjectDir(home, projectPath), sessionID+".jsonl")
}

// SubagentDir returns the directory holding sub-agent transcripts for
// a given Claude session.
func SubagentDir(home, projectPath, sessionID string) string {
	return filepath.Join(ProjectDir(home, projectPath), sessionID, "subagents")
}

// SubagentTranscriptPath returns the JSONL path for a specific
// sub-agent.
func SubagentTranscriptPath(home, projectPath, sessionID, agentID string) string {
	return filepath.Join(SubagentDir(home, projectPath, sessionID), "agent-"+agentID+".jsonl")
}

// FindLatestSession scans the project's directory for *.jsonl files
// and returns the one whose mtime is the most recent. Empty results
// return ("", os.ErrNotExist).
func FindLatestSession(home, projectPath string) (string, error) {
	dir := ProjectDir(home, projectPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	type entry struct {
		name string
		mod  int64
	}
	var jsonlEntries []entry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		jsonlEntries = append(jsonlEntries, entry{e.Name(), info.ModTime().UnixNano()})
	}
	if len(jsonlEntries) == 0 {
		return "", os.ErrNotExist
	}
	sort.Slice(jsonlEntries, func(i, j int) bool {
		return jsonlEntries[i].mod > jsonlEntries[j].mod
	})
	name := strings.TrimSuffix(jsonlEntries[0].name, ".jsonl")
	return name, nil
}

// ListSubagents returns the agent IDs found under the sub-agent dir
// for one Claude session.
func ListSubagents(home, projectPath, sessionID string) ([]string, error) {
	dir := SubagentDir(home, projectPath, sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(strings.TrimPrefix(name, "agent-"), ".jsonl"))
	}
	sort.Strings(ids)
	return ids, nil
}
