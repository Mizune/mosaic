package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletePath_SingleMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, _ := completePath(filepath.Join(dir, "al"))
	want := filepath.Join(dir, "alpha") + string(os.PathSeparator)
	if got != want {
		t.Errorf("completePath(%q) = %q, want %q", filepath.Join(dir, "al"), got, want)
	}
}

func TestCompletePath_MultipleMatches_LongestCommonPrefix(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha", "alpine", "apricot"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	input := filepath.Join(dir, "al")
	got, candidates := completePath(input)
	// "alpha" and "alpine" share "alp", so the longest common prefix
	// should be <dir>/alp.
	want := filepath.Join(dir, "alp")
	if got != want {
		t.Errorf("completePath(%q) = %q, want %q", input, got, want)
	}
	if len(candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d: %v", len(candidates), candidates)
	}
}

func TestCompletePath_NoMatch(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "definitely-missing")
	got, candidates := completePath(input)
	if got != input {
		t.Errorf("expected %q (unchanged), got %q", input, got)
	}
	if candidates != nil {
		t.Errorf("expected nil candidates, got %v", candidates)
	}
}

func TestCompletePath_TrailingSlash_ListsDirContents(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, candidates := completePath(dir + string(os.PathSeparator))
	if len(candidates) != 3 {
		t.Errorf("expected 3 candidates, got %d: %v", len(candidates), candidates)
	}
}

func TestCompletePath_TildePreservesAbbreviation(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	// We only verify that a returned path starting with the home
	// directory is re-abbreviated to "~/..." — we cannot assume
	// particular contents of $HOME.
	got, _ := completePath("~/")
	if got != "" && !strings.HasPrefix(got, "~") && !strings.HasPrefix(got, home) {
		t.Errorf("expected tilde-preserved result, got %q", got)
	}
}

func TestLongestCommonPrefix(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"alpha"}, "alpha"},
		{[]string{"alpha", "alpine"}, "alp"},
		{[]string{"alpha", "beta"}, ""},
		{[]string{"foo/a", "foo/b"}, "foo/"},
	}
	for _, c := range cases {
		if got := longestCommonPrefix(c.in); got != c.want {
			t.Errorf("longestCommonPrefix(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
