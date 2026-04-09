package tui

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// highlightDiffContent returns s with chroma-rendered ANSI colour
// codes applied for the language implied by path, using a chroma
// style chosen from themeName. The function is deliberately
// defensive — any lexer/formatter/style failure causes it to
// return the input unchanged so the Diff View always has
// something to show.
func highlightDiffContent(s, path, themeName string) string {
	if s == "" {
		return s
	}
	lexerAlias := lexerName(path)
	if lexerAlias == "" {
		return s
	}
	lexer := lexers.Get(lexerAlias)
	if lexer == nil {
		return s
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, s)
	if err != nil {
		return s
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		return s
	}
	style := chromaStyleForTheme(themeName)

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return s
	}
	out := buf.String()
	// chroma sometimes appends a trailing newline we do not want
	// inside a single-line render.
	return strings.TrimRight(out, "\n")
}

// chromaStyleForTheme picks the chroma style that visually
// matches the mosaic theme the Model is currently using. When a
// direct match does not exist in chroma's registry we fall back
// to a close cousin, and finally to chroma's built-in Fallback
// style so rendering never breaks.
func chromaStyleForTheme(themeName string) *chroma.Style {
	candidates := map[string][]string{
		"catppuccin-mocha":  {"catppuccin-mocha", "dracula"},
		"catppuccin-latte":  {"catppuccin-latte", "github"},
		"tokyo-night":       {"tokyonight-night", "dracula"},
		"tokyo-night-storm": {"tokyonight-storm", "tokyonight-night", "dracula"},
		"system":            {"bw"},
	}
	names := candidates[themeName]
	if len(names) == 0 {
		names = candidates["catppuccin-mocha"]
	}
	for _, n := range names {
		if st := styles.Get(n); st != nil && st != styles.Fallback {
			return st
		}
	}
	return styles.Fallback
}
