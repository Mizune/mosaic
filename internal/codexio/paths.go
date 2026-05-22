// Package codexio knows how to discover Codex CLI rollout JSONL files
// under ~/.codex/sessions and extract session metadata from them.
package codexio

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SessionMeta is the stable metadata Codex writes near the beginning
// of each rollout JSONL file.
type SessionMeta struct {
	ID         string
	CWD        string
	CLIVersion string
	Path       string
	Timestamp  time.Time
}

// SessionsRoot returns ~/.codex/sessions for the supplied home
// override. Passing "" uses the current user's home directory.
func SessionsRoot(home string) string {
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	return filepath.Join(home, ".codex", "sessions")
}

// FindLatestSession returns the newest Codex rollout whose recorded cwd
// belongs to worktreePath and whose timestamp is not before earliest.
func FindLatestSession(home, worktreePath string, earliest time.Time) (SessionMeta, bool) {
	root := SessionsRoot(home)
	var best SessionMeta
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		meta, ok := ReadSessionMeta(path)
		if !ok || meta.ID == "" {
			return nil
		}
		if !earliest.IsZero() && meta.Timestamp.Before(earliest) {
			return nil
		}
		if meta.CWD != "" && !sameTree(meta.CWD, worktreePath) {
			return nil
		}
		if best.ID == "" || meta.Timestamp.After(best.Timestamp) {
			best = meta
		}
		return nil
	})
	if err != nil || best.ID == "" {
		return SessionMeta{}, false
	}
	return best, true
}

// ReadSessionMeta scans the start of a Codex rollout JSONL file for the
// session_meta row. It tolerates malformed leading lines.
func ReadSessionMeta(path string) (SessionMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return SessionMeta{}, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		var row struct {
			Timestamp string `json:"timestamp"`
			Type      string `json:"type"`
			Payload   struct {
				ID         string `json:"id"`
				CWD        string `json:"cwd"`
				CLIVersion string `json:"cli_version"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			continue
		}
		if row.Type != "session_meta" {
			continue
		}
		ts, _ := time.Parse(time.RFC3339Nano, row.Timestamp)
		return SessionMeta{
			ID:         row.Payload.ID,
			CWD:        row.Payload.CWD,
			CLIVersion: row.Payload.CLIVersion,
			Path:       path,
			Timestamp:  ts,
		}, true
	}
	return SessionMeta{}, false
}

func sameTree(a, b string) bool {
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)
	return strings.HasPrefix(ca, cb) || strings.HasPrefix(cb, ca)
}
