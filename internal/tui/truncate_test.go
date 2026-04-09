package tui

import "testing"

func TestTruncateMiddle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"shorter than budget", "abc", 10, "abc"},
		{"exact fit", "abcdef", 6, "abcdef"},
		{"middle ellipsis preserves both ends", "packages/foo/bar/baz/widget.tsx", 16, "package…dget.tsx"},
		{"single char budget", "abcdef", 1, "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncateMiddle(c.in, c.n)
			if got != c.want {
				t.Errorf("truncateMiddle(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
			}
		})
	}
}
