package artifact

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/Mizune/mosaic/pkg/transcript"
)

// maxTextBlockBytes caps the text scanned per block. Larger blocks are
// pathological (dumped files, binary noise) and are skipped so the bus
// goroutine never spends long inside the fence scanner.
const maxTextBlockBytes = 256 << 10

// ExtractMermaid returns the body of every fenced code block in text
// whose info string names mermaid (```mermaid or ~~~mermaid, any case).
// It follows CommonMark fence rules closely enough for agent output: a
// fence is three or more identical backticks or tildes, optionally
// indented, and closes on a run at least as long of the same character.
// An unterminated fence yields nothing.
func ExtractMermaid(text string) []string {
	if len(text) > maxTextBlockBytes {
		return nil
	}
	var (
		out       []string
		fence     string // opening fence run, e.g. "```" or "~~~~"
		capturing bool
		body      []string
	)
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if fence == "" {
			run, info := splitFence(trimmed)
			if run == "" {
				continue
			}
			fence = run
			capturing = strings.EqualFold(firstWord(info), "mermaid")
			body = nil
			continue
		}
		if isClosingFence(trimmed, fence) {
			if capturing {
				out = append(out, strings.TrimRight(strings.Join(body, "\n"), "\n"))
			}
			fence, capturing, body = "", false, nil
			continue
		}
		if capturing {
			body = append(body, line)
		}
	}
	return out
}

// splitFence recognises an opening fence at the start of line and
// returns the fence run plus the trimmed info string. A backtick fence
// whose info string contains a backtick is inline code, not a fence.
func splitFence(line string) (run, info string) {
	if line == "" {
		return "", ""
	}
	ch := line[0]
	if ch != '`' && ch != '~' {
		return "", ""
	}
	n := leadingRun(line, ch)
	if n < 3 {
		return "", ""
	}
	info = strings.TrimSpace(line[n:])
	if ch == '`' && strings.Contains(info, "`") {
		return "", ""
	}
	return line[:n], info
}

// isClosingFence reports whether line closes the fence opened by open:
// the same character, at least as many of them, and nothing else.
func isClosingFence(line, open string) bool {
	if line == "" || line[0] != open[0] {
		return false
	}
	n := leadingRun(line, open[0])
	return n >= len(open) && strings.TrimSpace(line[n:]) == ""
}

func leadingRun(s string, ch byte) int {
	n := 0
	for n < len(s) && s[n] == ch {
		n++
	}
	return n
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

// writeTools are the Claude Code tools whose input names a file the
// agent created or changed.
var writeTools = map[string]bool{
	"Write":     true,
	"Edit":      true,
	"MultiEdit": true,
}

// filePathInput is the slice of a Write/Edit tool input mosaic needs.
type filePathInput struct {
	FilePath string `json:"file_path"`
}

// DetectWrittenFile reports the file artifact named by a Write/Edit
// tool_use block, if its extension is one the viewer can show. Relative
// paths resolve against the entry's cwd, then the session worktree.
func DetectWrittenFile(block transcript.ContentBlock, cwd, worktree string) (Detected, bool) {
	if !block.IsToolUse() || !writeTools[block.Name] {
		return Detected{}, false
	}
	var in filePathInput
	if err := json.Unmarshal(block.Input, &in); err != nil || in.FilePath == "" {
		return Detected{}, false
	}
	kind, ok := KindForPath(in.FilePath)
	if !ok {
		return Detected{}, false
	}
	abs := resolvePath(in.FilePath, cwd, worktree)
	return Detected{
		Kind:  kind,
		Title: filepath.Base(abs),
		Path:  abs,
		Hash:  ContentHash(abs),
	}, true
}

func resolvePath(p, cwd, worktree string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	base := cwd
	if base == "" {
		base = worktree
	}
	if base == "" {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

// DetectFromEntry walks one transcript entry and returns every artifact
// it contains, deduplicated by hash within the entry. Only assistant
// entries are considered: user entries carry tool results and prompts.
func DetectFromEntry(e transcript.Entry, worktree string) []Detected {
	if e.Type != transcript.EntryAssistant || e.Message == nil {
		return nil
	}
	at := e.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	seen := map[string]bool{}
	var out []Detected
	add := func(d Detected) {
		if seen[d.Hash] {
			return
		}
		seen[d.Hash] = true
		d.At = at
		out = append(out, d)
	}
	for _, block := range e.Message.Content {
		switch {
		case block.IsText():
			for _, src := range ExtractMermaid(block.Text) {
				if strings.TrimSpace(src) == "" {
					continue
				}
				add(Detected{
					Kind:    KindMermaid,
					Title:   TitleFor(src),
					Content: src,
					Hash:    ContentHash(src),
				})
			}
		case block.IsToolUse():
			if d, ok := DetectWrittenFile(block, e.CWD, worktree); ok {
				add(d)
			}
		}
	}
	return out
}
