package configwriter

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// splicedSection replaces (or appends) the block of lines belonging
// to tablePath in content with fragment. tablePath is a dot-separated
// TOML key path like "worktree.setup.alpha".
//
// "Belonging to tablePath" means the line's current-table-path equals
// tablePath OR is one of its sub-tables — so [worktree.setup.alpha.env]
// is considered part of [worktree.setup.alpha]'s block and is replaced
// alongside the parent.
//
// When content does not contain the section, fragment is appended at
// the end with a blank-line separator. Byte-for-byte preservation
// applies to every line OUTSIDE the target block; comments, blank
// lines, and unrelated sections round-trip identically.
func splicedSection(content []byte, tablePath string, fragment []byte) []byte {
	lines := splitKeepTerminator(content)

	// Start-scan: only regular tables can match our target path.
	// Array-of-tables headers ([[...]]) are never a target and are
	// skipped here — they matter only as end boundaries below.
	startIdx := -1
	for i, line := range lines {
		path, isArray, ok := sectionHeader(line)
		if !ok || isArray {
			continue
		}
		if pathMatches(path, tablePath) {
			startIdx = i
			break
		}
	}

	if startIdx == -1 {
		return appendFragment(content, fragment)
	}

	// End-scan: ANY header (regular OR array-of-tables) ends the
	// block, unless it is a matching sub-table of our target. This
	// ensures a [[project]] entry sitting between our section and the
	// next regular header is preserved rather than swallowed into the
	// replace range.
	endIdx := len(lines)
	for i := startIdx + 1; i < len(lines); i++ {
		path, isArray, ok := sectionHeader(lines[i])
		if !ok {
			continue
		}
		if isArray || !pathMatches(path, tablePath) {
			endIdx = i
			break
		}
	}

	// Trailing comments that sit directly above the next section
	// header (separated from our section's body by at least one
	// blank line) are conventionally that next section's header
	// comment. Rewinding past them keeps them attached to the
	// following section instead of silently deleting them with our
	// recipe.
	endIdx = rewindTrailingHeaderComments(lines, startIdx, endIdx)

	var out bytes.Buffer
	for _, l := range lines[:startIdx] {
		out.WriteString(l)
	}
	out.Write(fragment)
	if len(fragment) > 0 && !bytes.HasSuffix(fragment, []byte{'\n'}) {
		out.WriteByte('\n')
	}
	// Keep a blank line between our block and whatever follows, so the
	// output stays readable when the next section has no leading blank.
	if endIdx < len(lines) && !isBlank(lines[endIdx]) {
		out.WriteByte('\n')
	}
	for _, l := range lines[endIdx:] {
		out.WriteString(l)
	}
	return out.Bytes()
}

// splitKeepTerminator splits content on '\n' while keeping the newline
// attached to each line. This lets callers reassemble the file
// byte-for-byte by concatenating the returned slice.
func splitKeepTerminator(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	s := string(content)
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// sectionHeader extracts the dotted path from a TOML section header
// line. A regular header `[x.y]` returns (path, isArray=false, ok=true).
// An array-of-tables header `[[x.y]]` returns (path, isArray=true,
// ok=true). Any other line (key=value, comment, blank, multi-line
// string continuation) returns ("", false, false).
//
// The distinction matters in splicedSection: regular headers can be
// the target section, while array-of-tables headers can only be
// section boundaries — never a splice target. Treating [[...]] as
// "not a header" (the previous behaviour) silently swallowed any
// [[project]] block that sat between the target section and the next
// regular header, losing user data. See regression test in
// configwriter_test.go (b-006).
func sectionHeader(line string) (path string, isArray bool, ok bool) {
	// Strip the newline terminator and surrounding whitespace.
	trimmed := strings.TrimRight(line, "\r\n")
	trimmed = strings.TrimSpace(trimmed)
	if !strings.HasPrefix(trimmed, "[") {
		return "", false, false
	}
	// Handle trailing comments: [x.y]  # note
	if i := strings.IndexByte(trimmed, '#'); i >= 0 {
		trimmed = strings.TrimSpace(trimmed[:i])
	}
	if strings.HasPrefix(trimmed, "[[") {
		if !strings.HasSuffix(trimmed, "]]") {
			return "", false, false
		}
		inner := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
		if inner == "" {
			return "", false, false
		}
		return inner, true, true
	}
	if !strings.HasSuffix(trimmed, "]") {
		return "", false, false
	}
	inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	if inner == "" {
		return "", false, false
	}
	return inner, false, true
}

// pathMatches reports whether current is the same table as prefix or
// one of its sub-tables.
func pathMatches(current, prefix string) bool {
	return current == prefix || strings.HasPrefix(current, prefix+".")
}

// isBlank reports whether line contains only whitespace.
func isBlank(line string) bool {
	return strings.TrimSpace(line) == ""
}

// isComment reports whether line is a TOML comment line — one whose
// first non-whitespace character is '#'.
func isComment(line string) bool {
	t := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(t, "#")
}

// rewindTrailingHeaderComments narrows the deletion range to keep a
// "header comment block" attached to the section that follows. The
// heuristic: if the tail of lines[startIdx+1:endIdx] is a run of
// comment lines preceded by at least one blank line, those comments
// are the next section's heading documentation — preserve them by
// shrinking endIdx to the first comment's index.
func rewindTrailingHeaderComments(lines []string, startIdx, endIdx int) int {
	walk := endIdx
	for walk > startIdx+1 && isComment(lines[walk-1]) {
		walk--
	}
	if walk == endIdx {
		return endIdx // no trailing comments to preserve
	}
	if walk <= startIdx+1 {
		return endIdx // comments run all the way back into the section
	}
	if !isBlank(lines[walk-1]) {
		return endIdx // comments hug the previous content — they belong to us
	}
	return walk
}

// appendFragment concatenates fragment onto content, ensuring a blank
// line separates them when content was not already blank-terminated.
func appendFragment(content, fragment []byte) []byte {
	var out bytes.Buffer
	out.Write(content)
	if len(content) > 0 {
		if !bytes.HasSuffix(content, []byte{'\n'}) {
			out.WriteByte('\n')
		}
		// Ensure a blank line between the existing content and our
		// new section so headers read cleanly.
		if !bytes.HasSuffix(out.Bytes(), []byte{'\n', '\n'}) {
			out.WriteByte('\n')
		}
	}
	out.Write(fragment)
	if !bytes.HasSuffix(fragment, []byte{'\n'}) {
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// RenderFragment marshals value as a TOML fragment targeted at
// tablePath. The output starts with `[<tablePath>]` and any nested
// sub-tables receive their full dotted headers (e.g.
// `[<tablePath>.env]`) so the fragment can be dropped into a file
// without further editing.
//
// Exported so callers that want to PREVIEW what UpsertSections would
// write (e.g. the recipe wizard) can render the exact same bytes —
// avoiding silent drift between preview and on-disk output.
//
// Implementation: we marshal a `map[string]any{leaf: value}` wrapper
// and then rewrite the wrapper-injected header prefix back to the
// fully-qualified tablePath. When tablePath is a single path segment
// (no dots), the wrapper header already matches and no rewrite is
// needed.
func RenderFragment(tablePath string, value any) ([]byte, error) {
	if tablePath == "" {
		return nil, fmt.Errorf("configwriter: tablePath must not be empty")
	}
	leaf := tablePath
	if i := strings.LastIndexByte(tablePath, '.'); i >= 0 {
		leaf = tablePath[i+1:]
	}
	wrapper := map[string]any{leaf: value}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(wrapper); err != nil {
		return nil, err
	}

	if leaf == tablePath {
		return buf.Bytes(), nil
	}
	// Rewrite the wrapper's leaf-only headers to the full dotted path.
	// The rewrite is anchored to lines whose first non-whitespace
	// character is `[` so it only touches real table headers. An
	// unanchored bytes.ReplaceAll would also rewrite the literal
	// substring `[<leaf>.` when it appears inside a string VALUE
	// (e.g. `MY_VAR = "prefix[alpha.nested]suffix"`), silently
	// corrupting user data.
	return rewriteLeafHeaders(buf.Bytes(), leaf, tablePath), nil
}

// rewriteLeafHeaders replaces `[leaf]` / `[leaf.` at the start of each
// TOML table header line with `[tablePath]` / `[tablePath.`. Lines
// whose first non-whitespace character is not `[` are copied through
// unchanged, so string values containing the literal `[leaf.` are
// preserved verbatim.
func rewriteLeafHeaders(content []byte, leaf, tablePath string) []byte {
	openLeafClose := "[" + leaf + "]"
	openLeafDot := "[" + leaf + "."
	openTablePath := "[" + tablePath

	var out bytes.Buffer
	out.Grow(len(content))
	for _, line := range splitKeepTerminator(content) {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, openLeafClose) || strings.HasPrefix(trimmed, openLeafDot) {
			// Rewrite only the first occurrence — the header prefix —
			// leaving any later `[...]` on the same line untouched
			// (e.g. an inline comment the encoder did not emit but a
			// future encoder might).
			line = strings.Replace(line, "["+leaf, openTablePath, 1)
		}
		out.WriteString(line)
	}
	return out.Bytes()
}
