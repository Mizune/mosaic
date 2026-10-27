package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/artifact"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

const emptyArtifactsMessage = "No artifacts yet.\n" +
	"Mermaid blocks and Write/Edit of .png/.jpg/.svg/.html in Claude Code\n" +
	"transcripts are picked up automatically; press + to open a path or URL by hand."

// view renders the picker body without any frame: title, the list and
// preview columns, the optional input field and the key footer.
func (p *artifactPicker) view() string {
	t := p.deps.theme
	title := t.SectionTitle.Render(fmt.Sprintf("Artifacts: %s (%d)", p.sessionName, len(p.items)))

	var body string
	switch {
	case p.loadErr != "":
		body = t.AttentionTag.Render("✗ load failed: " + p.loadErr)
	case len(p.items) == 0:
		body = t.MutedText.Render(emptyArtifactsMessage)
	default:
		_, leftWidth, rightWidth := p.columns()
		left := lipgloss.NewStyle().Width(leftWidth).Render(p.renderRows(leftWidth))
		right := p.renderInlinePane(rightWidth)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	}

	sections := []string{title, "", body}
	if p.input.active {
		sections = append(sections, "", p.renderInput())
	}
	footer := t.MutedText.Render(
		"j/k: move   Enter: open   +: path/URL   R: refresh   q/Esc: close")
	sections = append(sections, "", footer)
	return strings.Join(sections, "\n")
}

// renderRows lists the items with a scroll window around the cursor:
// glyph + title on the left, source / age on the right, fitted into
// inner cells.
func (p *artifactPicker) renderRows(inner int) string {
	t := p.deps.theme
	if inner < 30 {
		inner = 30
	}
	_, maxRows := p.previewBox()
	maxRows++
	start, end := scrollWindow(p.cursor, len(p.items), maxRows)
	lines := make([]string, 0, end-start+2)
	if start > 0 {
		lines = append(lines, t.MutedText.Render(fmt.Sprintf("  … %d above", start)))
	}
	for i := start; i < end; i++ {
		a := p.items[i]
		indicator := "  "
		if i == p.cursor {
			indicator = "▶ "
		}
		meta := artifactMeta(a)
		title := a.Title
		if title == "" {
			title = a.Path
		}
		avail := inner - 4 - visibleLen(meta) - 2
		if avail < 8 {
			avail = 8
		}
		title = clipRunes(title, avail)
		pad := inner - 4 - visibleLen(title) - visibleLen(meta)
		if pad < 1 {
			pad = 1
		}
		plain := indicator + artifactGlyph(a.Kind) + " " + title + strings.Repeat(" ", pad)
		if i == p.cursor {
			lines = append(lines, t.AttentionTag.Render(plain+meta))
			continue
		}
		lines = append(lines, plain+t.MutedText.Render(meta))
	}
	if end < len(p.items) {
		lines = append(lines, t.MutedText.Render(fmt.Sprintf("  … %d more", len(p.items)-end)))
	}
	return strings.Join(lines, "\n")
}

// renderInput draws the `+` field under the list, mirroring the
// Conductor panel's plan-path modal.
func (p *artifactPicker) renderInput() string {
	t := p.deps.theme
	in := p.input
	title := t.SectionTitle.Render("Open path or URL")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Accent).
		Padding(0, 1).
		Width(p.width / 2).
		Render("> " + in.value + "_")
	lines := []string{
		title,
		t.MutedText.Render("Enter: open · Tab: complete path · Ctrl+U: clear · Esc: cancel"),
		box,
	}
	if len(in.candidates) > 0 {
		shown := in.candidates
		if len(shown) > 8 {
			shown = append(shown[:8:8], fmt.Sprintf("… %d more", len(in.candidates)-8))
		}
		lines = append(lines, t.MutedText.Render(strings.Join(shown, "  ")))
	}
	if in.err != "" {
		lines = append(lines, t.AttentionTag.Render("✗ "+in.err))
	}
	return strings.Join(lines, "\n")
}

// renderModalFrame is the canonical full-screen modal frame used by
// every TUI panel: same border, padding, accent colour and centred
// placement, so new panels look like the existing ones.
func renderModalFrame(t *theme.Theme, width, height int, body string) string {
	boxWidth := width - 8
	if boxWidth < 60 {
		boxWidth = 60
	}
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Accent).
		Padding(1, 2).
		Width(boxWidth).
		Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// artifactMeta is the right-hand column of a picker row.
func artifactMeta(a store.Artifact) string {
	age := humanAge(a.CreatedAt)
	switch {
	case a.Source == string(artifact.SourceManual):
		return "manual · " + age
	case a.AgentID != "":
		return "sub-agent · " + age
	default:
		return age
	}
}

// artifactGlyph is the one-cell kind marker used in lists.
func artifactGlyph(kind string) string {
	switch artifact.Kind(kind) {
	case artifact.KindMermaid:
		return "◇"
	case artifact.KindImage:
		return "▣"
	case artifact.KindHTML:
		return "▤"
	case artifact.KindURL:
		return "↗"
	default:
		return "•"
	}
}

// scrollWindow returns the [start, end) slice of total rows that keeps
// cursor visible within max rows.
func scrollWindow(cursor, total, max int) (int, int) {
	if total <= max {
		return 0, total
	}
	start := cursor - max/2
	if start < 0 {
		start = 0
	}
	if start+max > total {
		start = total - max
	}
	return start, start + max
}

// clipRunes shortens s to at most n display runes, ending in an
// ellipsis when something was cut. Unlike truncate it never splits a
// multi-byte character.
func clipRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}
