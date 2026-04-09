package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// completePath attempts to expand current into a filesystem path,
// returning the completed value plus the full list of candidates
// mosaic should show in the UI.
//
// The rules mirror what shells do:
//
//   - Leading "~" is expanded to $HOME.
//   - Relative paths resolve against the current working directory.
//   - The input is split into (dir, prefix); entries in dir that
//     start with prefix become candidates.
//   - When the input ends with "/", every entry in that directory
//     is a candidate (prefix is empty).
//   - If exactly one candidate matches, the returned path is the
//     full candidate — with a trailing "/" when it is a directory,
//     so the user can keep tabbing deeper without retyping.
//   - When multiple candidates match, the returned path is the
//     longest common prefix so one Tab press still moves the
//     cursor forward.
//
// A non-readable directory or total mismatch yields (current, nil).
func completePath(current string) (string, []string) {
	if current == "" {
		current = "."
	}

	// Expand a leading "~". We keep the operation local so the rest
	// of the function doesn't have to special-case it.
	display := current
	expanded, hadTilde := expandTilde(current)

	dir, prefix := filepath.Split(expanded)
	if dir == "" {
		dir = "."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return display, nil
	}

	var matches []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		full := filepath.Join(dir, name)
		if e.IsDir() {
			full += string(os.PathSeparator)
		}
		matches = append(matches, full)
	}
	sort.Strings(matches)

	if len(matches) == 0 {
		return display, nil
	}

	var completed string
	if len(matches) == 1 {
		completed = matches[0]
	} else {
		completed = longestCommonPrefix(matches)
		// longestCommonPrefix may land short of the user's current
		// input when they've typed something that doesn't appear in
		// every candidate (shouldn't happen, but guard anyway).
		if len(completed) < len(expanded) {
			completed = expanded
		}
	}

	// If the user's original input used a ~, keep the tilde notation
	// in the display value — they'll appreciate the brevity.
	if hadTilde {
		if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(completed, home) {
			completed = "~" + strings.TrimPrefix(completed, home)
		}
	}
	return completed, matches
}

// expandTilde replaces a leading "~" or "~/" with $HOME. It returns
// the expanded path and a flag that tells the caller whether the
// substitution happened (so the display value can be re-abbreviated
// afterwards).
func expandTilde(p string) (string, bool) {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home, true
		}
		return p, false
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:]), true
		}
	}
	return p, false
}

// longestCommonPrefix returns the longest string that is a prefix of
// every element in the argument slice. Empty input → "".
func longestCommonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	prefix := ss[0]
	for _, s := range ss[1:] {
		max := len(prefix)
		if len(s) < max {
			max = len(s)
		}
		i := 0
		for i < max && prefix[i] == s[i] {
			i++
		}
		prefix = prefix[:i]
		if prefix == "" {
			return ""
		}
	}
	return prefix
}
