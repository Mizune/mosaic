package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

// renderTop is the lazygit-style two-pane TOP screen.
func (m *Model) renderTop() string {
	listWidth := m.width / 3
	if listWidth < 30 {
		listWidth = 30
	}
	previewWidth := m.width - listWidth - 4
	if previewWidth < 30 {
		previewWidth = 30
	}
	// Chrome budget:
	//   row 0: header row 1
	//   row 1: header row 2
	//   row 2: horizontal rule
	//   rows 3..N-3: body
	//   row N-2: footer row 1
	//   row N-1: footer row 2
	// → body gets m.height - 5 rows.
	//
	// The BorderBox in renderLeftList / renderPreview sets
	// Height(bodyHeight) as a *minimum*, so content that is
	// longer than expected expands the box and pushes
	// everything else (including the header) off the top of
	// the alt-screen. To stay safe we clip the rendered body
	// to exactly bodyHeight lines before joining.
	bodyHeight := m.height - 5
	if bodyHeight < 4 {
		bodyHeight = 4
	}

	header := m.renderHeader()
	headerSep := m.theme.MutedText.Render(strings.Repeat("─", m.width))
	left := m.renderLeftList(listWidth, bodyHeight)
	right := m.renderPreview(previewWidth, bodyHeight)
	body := clipLines(lipgloss.JoinHorizontal(lipgloss.Top, left, right), bodyHeight)
	footer := m.renderStatusBar()

	// Build the final frame with plain string concat and clip
	// defensively to m.height so runaway content from any
	// component cannot push the header off the top.
	frame := header + "\n" + headerSep + "\n" + body + "\n" + footer
	return clipLines(frame, m.height)
}

// clipLines truncates s to at most n lines so a runaway render
// inside one of the sub-components cannot knock the header off
// the top of the alt-screen. Whitespace-only trailing lines are
// kept because lipgloss BorderBox pads with them intentionally.
func clipLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}

func (m *Model) renderHeader() string {
	// Count over the authoritative session list so the totals
	// stay honest under any active filter tab.
	attCount := 0
	running := 0
	idle := 0
	stopped := 0
	subagentCount := 0
	for _, s := range m.lastSessions {
		switch {
		case s.AttentionState == string(store.AttentionWaitingInput):
			attCount++
		case s.Status == string(store.StatusRunning):
			running++
		case s.Status == string(store.StatusIdle):
			idle++
		case s.Status == string(store.StatusStopped):
			stopped++
		}
		subagentCount += len(m.subagents[s.ID])
	}
	sessionCount := len(m.lastSessions)
	groupCount := 0
	seen := map[string]bool{}
	for _, s := range m.lastSessions {
		g := m.groupNameFor(s)
		if !seen[g] {
			seen[g] = true
			groupCount++
		}
	}

	// --- Row 1: brand + meta ---------------------------------------
	//
	// Every styled segment below uses Foreground-only styling.
	// Earlier revisions tried lipgloss.Padding + Background on
	// the brand tile and the meta pills, which broke lipgloss's
	// width computation and caused the whole row to silently
	// disappear. Rich colour without backgrounds gets us the
	// visual punch without the fragility.
	profile := m.config.Mosaic.Profile
	if profile == "" {
		profile = "default"
	}
	bold := func(c lipgloss.Color, s string) string {
		return lipgloss.NewStyle().Bold(true).Foreground(c).Render(s)
	}
	sep := m.theme.MutedText.Render("  ·  ")

	row1Parts := []string{
		bold(m.theme.Accent, "◆ mosaic"),
		lipgloss.NewStyle().Foreground(m.theme.Highlight).Render(version),
	}
	if m.availableUpdate != "" {
		row1Parts = append(row1Parts,
			lipgloss.NewStyle().
				Foreground(m.theme.Running).
				Bold(true).
				Render("↑ v"+m.availableUpdate+" available"))
	}
	row1Parts = append(row1Parts,
		sep,
		m.theme.MutedText.Render("profile"),
		bold(m.theme.Text, profile),
		sep,
		m.theme.MutedText.Render("groups"),
		bold(m.theme.Accent, fmt.Sprintf("%d", groupCount)),
		sep,
		m.theme.MutedText.Render("sessions"),
		bold(m.theme.Accent, fmt.Sprintf("%d", sessionCount)),
	)
	if subagentCount > 0 {
		row1Parts = append(row1Parts, sep,
			m.theme.MutedText.Render("sub-agents"),
			bold(m.theme.Highlight, fmt.Sprintf("%d", subagentCount)))
	}
	row1Left := " " + strings.Join(row1Parts, " ")

	// Right-side context chips (filter + visual mode). These
	// are simple bracketed strings with strong foreground
	// colour so they read as alert indicators.
	var ctxParts []string
	if m.statusFilter != statusFilterNone {
		ctxParts = append(ctxParts,
			lipgloss.NewStyle().
				Foreground(m.theme.Highlight).
				Bold(true).
				Render("⚑ filter:"+m.statusFilter.label()))
	}
	if m.visualAnchor >= 0 {
		ctxParts = append(ctxParts,
			lipgloss.NewStyle().
				Foreground(m.theme.Attention).
				Bold(true).
				Render("◉ VISUAL"))
	}
	row1Right := strings.Join(ctxParts, "  ")
	row1 := padRow(row1Left, row1Right, m.width)

	// --- Row 2: status badges + clock -----------------------------
	badge := func(icon, count, label string, iconStyle lipgloss.Style) string {
		return iconStyle.Render(icon) + " " +
			bold(m.theme.Text, count) + " " +
			m.theme.MutedText.Render(label)
	}
	row2Parts := []string{
		badge("●", fmt.Sprintf("%d", running), "running", m.theme.RunningTag),
		badge("○", fmt.Sprintf("%d", idle), "idle", m.theme.IdleTag),
	}
	if attCount > 0 {
		row2Parts = append(row2Parts,
			badge("⚠", fmt.Sprintf("%d", attCount), "waiting", m.theme.AttentionTag))
	} else {
		row2Parts = append(row2Parts,
			badge("⚠", "0", "waiting", m.theme.MutedText))
	}
	if stopped > 0 {
		row2Parts = append(row2Parts,
			badge("■", fmt.Sprintf("%d", stopped), "stopped", m.theme.StoppedTag))
	}
	row2Left := " " + strings.Join(row2Parts, "   ")
	clock := m.theme.MutedText.Render(time.Now().Format("15:04"))
	row2 := padRow(row2Left, clock, m.width)

	return row1 + "\n" + row2
}

// padRow joins left and right into a single row of exactly
// `total` visible columns, filling the gap with spaces and
// clamping when the content overflows. Returning exactly the
// requested width keeps vertical joins stable — a row that is
// narrower than the container was the culprit behind the header
// "row 1 disappearing" bug, because lipgloss's JoinVertical
// right-pads shorter rows with backgrounds from neighbouring
// siblings in ways that sometimes swallowed the row entirely.
func padRow(left, right string, total int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	if lw+rw+1 >= total {
		// Overflow: drop the right block so the left still
		// gets painted. Better to lose a chip than the brand.
		if lw >= total {
			return left
		}
		return left + strings.Repeat(" ", total-lw)
	}
	gap := total - lw - rw
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) renderLeftList(width, height int) string {
	style := m.theme.BorderBox.Width(width).Height(height)

	// Before the first refresh completes, show an animated
	// spinner so the user knows mosaic is talking to disk
	// rather than wondering if it crashed.
	if !m.firstRefreshDone {
		return style.Render(
			m.theme.SectionTitle.Render(m.spinnerGlyph()) + "  " +
				m.theme.MutedText.Render("loading..."))
	}
	if len(m.rows) == 0 {
		return style.Render(m.renderEmptyState())
	}

	// Inner width = outer width minus 2 border chars + 2 horizontal
	// padding chars that lipgloss's BorderBox adds.
	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	var lines []string
	for i, row := range m.rows {
		text := m.renderRowLine(row, innerWidth)
		// Session rows in waiting_input state get a red-tinted
		// background so the user's eye is drawn there first.
		// The pulse cycles the background between two intensities
		// every ~1 second so the row visibly throbs without
		// being painful to look at. The highlight is applied
		// BEFORE the selection style so that a selected-and-
		// waiting row gets both (the selection still reads as
		// the cursor).
		if !row.IsGroup && !row.IsSubagent &&
			row.Session.AttentionState == string(store.AttentionWaitingInput) {
			pulseStyle := m.theme.AttentionRow
			if m.pulsing() {
				pulseStyle = pulseStyle.Background(lipgloss.Color("#5a2638"))
			}
			text = pulseStyle.Width(innerWidth).Render(text)
		}
		if i == m.cursor {
			text = m.theme.SelectedRow.Width(innerWidth).Render(text)
		}
		lines = append(lines, text)
		if len(lines) >= height-2 {
			break
		}
	}
	return style.Render(strings.Join(lines, "\n"))
}

// renderRowLine draws one line in the left tree. The format is
// deliberately narrow so the string always fits inside the border
// box — lipgloss wraps on overflow and the resulting visual corruption
// was the source of earlier "- 14m on its own line" bug reports.
//
// Layout (inner width ≥ 24 assumed):
//
//	▼ <group> (N)                  — group header
//	  ● <name padded>      <age>   — session row
//	    ⟳ <type> · <tool> ⏱<age>  — sub-agent row (indented)
//
// Only a single-character colored icon is used instead of
// "● running" so we get the status at a glance without burning
// half the row on a word.
func (m *Model) renderRowLine(row displayRow, innerWidth int) string {
	if row.IsGroup {
		caret := "▼"
		if !row.GroupExpanded {
			caret = "▶"
		}
		// Group headers get the section-title treatment:
		// accent-coloured caret + bold accent name + a muted
		// `(N)` count. This is one of the few places the
		// theme's brand colour lands in the left pane, so it
		// anchors the eye when scanning a long list.
		caretStyled := lipgloss.NewStyle().
			Foreground(m.theme.Accent).
			Bold(true).
			Render(caret)
		name := lipgloss.NewStyle().
			Foreground(m.theme.Accent).
			Bold(true).
			Render(row.GroupName)
		count := m.theme.MutedText.Render(fmt.Sprintf(" (%d)", row.GroupCount))
		return caretStyled + " " + name + count
	}
	if row.IsSubagent {
		return m.renderSubagentRowLine(row, innerWidth)
	}

	s := row.Session
	icon := statusIcon(m.theme, displayedStatus(s))
	age := m.theme.MutedText.Render(humanAge(s.LastActiveAt))
	subCount := len(m.subagents[s.ID])

	// Budget: 2 (indent) + icon (1 visible) + 1 space + name + tail (age + optional "sN")
	// We reserve a right-aligned block for the tail.
	tail := age
	if subCount > 0 {
		tail = lipgloss.NewStyle().
			Foreground(m.theme.Highlight).
			Render(fmt.Sprintf("s%d", subCount)) + " " + age
	}

	nameBudget := innerWidth - 2 /*indent*/ - 1 /*icon*/ - 1 /*space*/ - 1 /*gap*/ - visibleLen(tail)
	if nameBudget < 4 {
		nameBudget = 4
	}
	name := truncate(s.Name, nameBudget)

	// Session names get themed text colour (not plain default
	// terminal text) so the left list actually reflects the
	// selected palette. Bold on running sessions nudges the
	// eye toward active work.
	nameStyle := lipgloss.NewStyle().Foreground(m.theme.Text)
	if s.Status == string(store.StatusRunning) {
		nameStyle = nameStyle.Bold(true)
	}
	if s.Status == string(store.StatusStopped) {
		nameStyle = lipgloss.NewStyle().Foreground(m.theme.Stopped)
	}
	styledName := nameStyle.Render(name) +
		strings.Repeat(" ", max(0, nameBudget-visibleLen(name)))

	// "×" marker when the row has been Space-marked for batch
	// operations. It replaces the leading indent column so the
	// rest of the layout stays aligned.
	indent := "  "
	if m.selected[s.ID] {
		indent = m.theme.AttentionTag.Render("×") + " "
	}
	return indent + icon + " " + styledName + " " + tail
}

// renderSubagentRowLine draws one sub-agent line beneath its parent
// session. It deliberately uses a narrower format than the session
// row so it reads as a child of the session.
func (m *Model) renderSubagentRowLine(row displayRow, innerWidth int) string {
	s := row.Subagent
	icon := m.theme.MutedText.Render("·")
	switch store.SubagentStatus(s.Status) {
	case store.SubagentRunning:
		icon = m.theme.RunningTag.Render("⟳")
	case store.SubagentCompleted:
		icon = m.theme.IdleTag.Render("✓")
	case store.SubagentErrored:
		icon = m.theme.ErrorTag.Render("✗")
	}

	// current_tool persists across tool gaps (not cleared between
	// rapid calls), so the elapsed timer may reflect a prior tool's
	// start time during inter-tool pauses. This is intentional:
	// showing the last tool is more informative than "(idle)".
	tool := "(idle)"
	elapsed := ""
	if s.CurrentTool.Valid && s.CurrentTool.String != "" {
		tool = s.CurrentTool.String
		if s.CurrentToolStartedAt.Valid {
			elapsed = " ⏱" + humanAge(s.CurrentToolStartedAt.Time)
		}
	}
	label := subagentLabel(s)

	// Tree connector — └─ for the last sibling, ├─ for every
	// other child. The leading two spaces give us the same
	// hanging indent as the parent's "  ●" prefix so the
	// vertical bar lines up under the parent's status icon.
	connector := "  ├─ "
	if row.IsLastSibling {
		connector = "  └─ "
	}
	prefix := m.theme.MutedText.Render(connector) + icon + " "

	connectorWidth := visibleLen(connector) + 2 // icon + space
	budget := innerWidth - connectorWidth - visibleLen(" · "+tool+elapsed)
	if budget < 6 {
		budget = 6
	}
	return prefix + truncate(label, budget) + " · " +
		m.theme.MutedText.Render(tool+elapsed)
}

// spinnerGlyph returns the next frame of the loading spinner.
// The frames are the standard "Braille dots" cycle that most
// modern monospace terminals render correctly.
func (m *Model) spinnerGlyph() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[m.spinnerFrame%len(frames)]
}

// pulsing returns true on roughly half the spinner frames so a
// caller can blink an attention indicator without flickering
// painfully fast.
func (m *Model) pulsing() bool {
	return (m.spinnerFrame/4)%2 == 0
}

// renderEmptyState draws a friendly welcome card when the
// session list is empty. It is more inviting than a single
// "no sessions yet" line and points the user at the actions
// they need next.
func (m *Model) renderEmptyState() string {
	logo := lipgloss.NewStyle().
		Foreground(m.theme.Accent).
		Bold(true).
		Render("◆◆◆   M O S A I C   ◆◆◆")
	msg := lipgloss.NewStyle().
		Foreground(m.theme.Text).
		Bold(true).
		Render("No sessions yet")
	keyStyle := lipgloss.NewStyle().
		Foreground(m.theme.Highlight).
		Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(m.theme.Text)
	hint := []string{
		"",
		logo,
		"",
		msg,
		"",
		"  " + keyStyle.Render("n") + " " + labelStyle.Render("create a new session"),
		"  " + keyStyle.Render("?") + " " + labelStyle.Render("show every keybinding"),
		"  " + keyStyle.Render(",") + " " + labelStyle.Render("open settings"),
		"  " + keyStyle.Render("c") + " " + labelStyle.Render("conductor panel"),
	}
	return strings.Join(hint, "\n")
}

// statusIcon returns the single-character colored status icon used
// in the list view. It deliberately does not append the status name
// (the preview pane carries that) so the list stays narrow.
func statusIcon(t *theme.Theme, status string) string {
	switch status {
	case "running":
		return t.RunningTag.Render("●")
	case "waiting", "waiting_input":
		return t.AttentionTag.Render("⚠")
	case "error":
		return t.ErrorTag.Render("✗")
	case "stopped":
		return t.StoppedTag.Render("■")
	case "spawning":
		return t.MutedText.Render("…")
	default:
		return t.IdleTag.Render("○")
	}
}

// visibleLen is a crude lipgloss-aware width measurement: it strips
// ANSI SGR sequences and counts the remaining runes.
func visibleLen(s string) int {
	return len([]rune(stripEscapes(s)))
}

// stripEscapes removes ANSI SGR sequences (`ESC [ ... m`) and any
// OSC sequences (`ESC ] ... BEL` or `ESC ] ... ESC \`) from s. It is
// intentionally permissive: fragments that look like escape starts
// but never terminate are dropped silently so a truncated capture
// never produces garbled output.
func stripEscapes(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] != 0x1b || i+1 >= len(s) {
			out.WriteByte(s[i])
			i++
			continue
		}
		switch s[i+1] {
		case '[':
			// CSI: skip until a final byte in 0x40-0x7e
			// (typically 'm' for SGR).
			j := i + 2
			for j < len(s) {
				c := s[j]
				if c >= 0x40 && c <= 0x7e {
					j++
					break
				}
				j++
			}
			i = j
		case ']':
			// OSC: skip until BEL or ESC \.
			j := i + 2
			for j < len(s) {
				if s[j] == 0x07 {
					j++
					break
				}
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
		default:
			// Unknown escape: drop the ESC and the following byte.
			i += 2
		}
	}
	return out.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// displayedStatus chooses the status string used for the row glyph,
// preferring AttentionState over Status when waiting_input is set.
func displayedStatus(s store.Session) string {
	if s.AttentionState == string(store.AttentionWaitingInput) {
		return "waiting"
	}
	return s.Status
}

func (m *Model) renderPreview(width, height int) string {
	style := m.theme.BorderBox.Width(width).Height(height)

	// Budget: 2 border + 2 padding rows vertically, 4 columns of
	// horizontal border/padding.
	innerWidth := width - 4
	innerHeight := height - 2
	if innerWidth < 20 {
		innerWidth = 20
	}
	if innerHeight < 4 {
		innerHeight = 4
	}

	// If the cursor is on a group header, show a group-level
	// summary instead of the useless "select a session"
	// placeholder. Users navigating with j/k over many groups
	// see something useful on every row without having to
	// drill in first.
	if len(m.rows) > 0 && m.cursor >= 0 && m.cursor < len(m.rows) {
		if row := m.rows[m.cursor]; row.IsGroup {
			return style.Render(m.renderGroupSummary(row, innerHeight, innerWidth))
		}
	}

	row, ok := m.selectedSession()
	if !ok {
		return style.Render(m.renderEmptyState())
	}

	// Mode tab strip: shows the four preview modes with the
	// active one highlighted, plus a "Tab to cycle" hint. The
	// strip is rendered above the body for every mode so the
	// keybinding stays discoverable.
	tabs := m.renderPreviewTabs(innerWidth)
	body := ""
	switch m.previewMode {
	case previewLiveCapture:
		body = m.renderPreviewLive(row, innerHeight-2, innerWidth)
	case previewSubAgentsOnly:
		body = m.renderPreviewSubAgents(row, innerHeight-2, innerWidth)
	case previewDiffSummary:
		body = m.renderPreviewDiff(row, innerHeight-2, innerWidth)
	default:
		body = m.renderPreviewStructured(row, innerHeight-2, innerWidth)
	}
	return style.Render(tabs + "\n" + body)
}

// renderGroupSummary draws the right-pane content when the
// cursor is on a group header. It is meant to be informative
// at-a-glance without requiring the user to drill into the
// group first — showing the project name, a status breakdown,
// and a compact list of every session in the group with its
// current tool and age.
func (m *Model) renderGroupSummary(row displayRow, innerHeight, innerWidth int) string {
	// Gather every session that belongs to this group. We walk
	// lastSessions so the numbers stay honest under any filter.
	var members []store.Session
	for _, s := range m.lastSessions {
		if m.groupNameFor(s) == row.GroupName {
			members = append(members, s)
		}
	}
	sort.SliceStable(members, func(i, j int) bool {
		return statusRank(members[i]) < statusRank(members[j])
	})

	attn, running, idle, stopped := 0, 0, 0, 0
	for _, s := range members {
		switch {
		case s.AttentionState == string(store.AttentionWaitingInput):
			attn++
		case s.Status == string(store.StatusRunning):
			running++
		case s.Status == string(store.StatusStopped):
			stopped++
		default:
			idle++
		}
	}

	header := m.theme.Header.Render(row.GroupName) + "  " +
		m.theme.MutedText.Render(fmt.Sprintf("(%d sessions)", len(members)))

	badge := func(icon, count, label string, st lipgloss.Style) string {
		if count == "0" {
			return ""
		}
		return st.Render(icon) + " " +
			lipgloss.NewStyle().Bold(true).Render(count) + " " +
			m.theme.MutedText.Render(label)
	}
	badges := []string{
		badge("●", fmt.Sprintf("%d", running), "running", m.theme.RunningTag),
		badge("○", fmt.Sprintf("%d", idle), "idle", m.theme.IdleTag),
		badge("⚠", fmt.Sprintf("%d", attn), "waiting", m.theme.AttentionTag),
		badge("■", fmt.Sprintf("%d", stopped), "stopped", m.theme.StoppedTag),
	}
	filtered := make([]string, 0, len(badges))
	for _, b := range badges {
		if b != "" {
			filtered = append(filtered, b)
		}
	}
	breakdown := strings.Join(filtered, "   ")

	// Compact member list — one row per session, status icon
	// plus name plus last-active age. Long lists get truncated
	// with a "+N more" tail so the summary always fits.
	listBudget := innerHeight - 6
	if listBudget < 3 {
		listBudget = 3
	}
	var lines []string
	for i, s := range members {
		if i >= listBudget {
			lines = append(lines, "",
				m.theme.MutedText.Render(
					fmt.Sprintf("… +%d more (Enter / j to drill in)", len(members)-i)))
			break
		}
		icon := statusIcon(m.theme, displayedStatus(s))
		tool := m.theme.MutedText.Render(s.Tool)
		age := m.theme.MutedText.Render(humanAge(s.LastActiveAt))
		nameBudget := innerWidth - 12
		if nameBudget < 10 {
			nameBudget = 10
		}
		name := truncate(s.Name, nameBudget)
		lines = append(lines, fmt.Sprintf("  %s  %-*s  %s  %s",
			icon, nameBudget, name, tool, age))
	}

	hint := m.theme.MutedText.Render(
		"Enter to open · h/l to collapse · j/k to move")

	body := header + "\n" +
		breakdown + "\n\n" +
		strings.Join(lines, "\n") + "\n\n" +
		hint
	return body
}

// renderPreviewTabs draws the mode toggle row at the top of
// the right pane. Active tab gets the theme accent colour +
// bold + underline; inactive tabs are muted. Foreground-only
// styling sidesteps the background-padding fragility we hit
// in the header.
func (m *Model) renderPreviewTabs(innerWidth int) string {
	mkTab := func(label string, mode previewMode) string {
		if mode == m.previewMode {
			return lipgloss.NewStyle().
				Foreground(m.theme.Accent).
				Bold(true).
				Underline(true).
				Render(" " + label + " ")
		}
		return lipgloss.NewStyle().
			Foreground(m.theme.Muted).
			Render(" " + label + " ")
	}
	sep := m.theme.MutedText.Render("│")
	tabs := mkTab("structured", previewStructured) + sep +
		mkTab("live", previewLiveCapture) + sep +
		mkTab("sub-agents", previewSubAgentsOnly) + sep +
		mkTab("diff", previewDiffSummary)
	hint := m.theme.MutedText.Render("Tab ↹")
	gap := innerWidth - lipgloss.Width(tabs) - lipgloss.Width(hint)
	if gap < 1 {
		gap = 1
	}
	return tabs + strings.Repeat(" ", gap) + hint
}

// renderPreviewStructured is the original metadata-rich layout.
func (m *Model) renderPreviewStructured(row store.Session, innerHeight, innerWidth int) string {
	var top []string
	top = append(top, m.theme.Header.Render(row.Name)+
		m.theme.MutedText.Render("  "+row.WorktreePath))

	if row.AttentionState == string(store.AttentionWaitingInput) {
		title := row.AttentionTitle.String
		body := row.AttentionBody.String
		top = append(top,
			m.theme.AttentionTag.Render("⚠ "+title)+"\n"+
				m.theme.MutedText.Render(body),
		)
	}

	top = append(top, m.renderSection("Worktree", []string{
		"branch:  " + ifEmpty(row.Branch.String, "-"),
		"path:    " + row.WorktreePath,
		"status:  " + row.Status,
	}))

	sessionID := row.AgentSessionID.String
	if sessionID == "" {
		sessionID = row.ClaudeSessionID.String
	}
	top = append(top, m.renderSection("Agent", []string{
		"tool:          " + row.Tool,
		"session:       " + ifEmpty(sessionID, "(not bound)"),
		"cli version:   " + ifEmpty(row.AgentCLIVersion, "-"),
	}))

	if subs := m.subagents[row.ID]; len(subs) > 0 {
		top = append(top, m.renderSection(
			fmt.Sprintf("Sub-agents (%d)", len(subs)),
			m.formatSubAgentLines(subs)))
	}

	topBody := strings.Join(top, "\n\n")
	topLines := strings.Count(topBody, "\n") + 1
	outputInnerHeight := innerHeight - topLines - 2
	if outputInnerHeight < 3 {
		outputInnerHeight = 3
	}
	outputBox := m.renderOutputBox(row, outputInnerHeight, innerWidth)
	return topBody + "\n\n" + outputBox
}

// renderPreviewLive shows just the tmux capture, full height.
func (m *Model) renderPreviewLive(row store.Session, innerHeight, innerWidth int) string {
	header := m.theme.Header.Render(row.Name) + "  " +
		m.theme.MutedText.Render("(live capture)")
	box := m.renderOutputBox(row, innerHeight-2, innerWidth)
	return header + "\n\n" + box
}

// renderPreviewSubAgents drops everything except the sub-agent
// table so the user can scan a long DAG without scrolling.
func (m *Model) renderPreviewSubAgents(row store.Session, innerHeight, innerWidth int) string {
	subs := m.subagents[row.ID]
	header := m.theme.Header.Render(row.Name) + "  " +
		m.theme.MutedText.Render(fmt.Sprintf("(%d sub-agents)", len(subs)))
	if len(subs) == 0 {
		return header + "\n\n" +
			m.theme.MutedText.Render("This session has no sub-agents yet.")
	}
	body := strings.Join(m.formatSubAgentLines(subs), "\n")
	return header + "\n\n" + body
}

// renderPreviewDiff renders a quick diff summary fetched from
// the session service. The full Diff View (`d`) is still the
// place to interact with hunks; this is the at-a-glance
// version.
func (m *Model) renderPreviewDiff(row store.Session, innerHeight, innerWidth int) string {
	header := m.theme.Header.Render(row.Name) + "  " +
		m.theme.MutedText.Render("(diff summary)")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res, err := m.sessions.Diff(ctx, row.Name)
	if err != nil {
		return header + "\n\n" + m.theme.AttentionTag.Render("✗ "+err.Error())
	}
	if len(res.Files) == 0 {
		return header + "\n\n" +
			m.theme.MutedText.Render("No changes against "+res.BaseRef+".")
	}
	var lines []string
	limit := innerHeight - 4
	if limit < 5 {
		limit = 5
	}
	for i, f := range res.Files {
		if i >= limit {
			lines = append(lines,
				m.theme.MutedText.Render(fmt.Sprintf("… %d more (press d)", len(res.Files)-i)))
			break
		}
		stats := formatDiffStats(f)
		lines = append(lines, fmt.Sprintf("  %s  %-40s %s",
			diffStatusGlyph(f.Status), truncate(f.Path, 40), stats))
	}
	return header + "\n\n" +
		m.theme.MutedText.Render("base: "+res.BaseRef) + "\n" +
		strings.Join(lines, "\n")
}

// subagentLabel builds a human-readable label for a sub-agent row.
// When a name is set it takes priority over the generic type, e.g.
// "explore-auth: search for patterns" instead of "Explore: search...".
// See also discordSubagentLabel in internal/discord/format.go which
// uses bracket notation instead.
func subagentLabel(sub store.Subagent) string {
	hasType, typ, name, desc := sub.LabelParts()
	switch {
	case name != "" && desc != "":
		return name + ": " + desc
	case name != "":
		return name
	case hasType && desc != "":
		return typ + ": " + desc
	case desc != "":
		return desc
	default:
		return typ
	}
}

// formatSubAgentLines is the shared formatter for sub-agent
// preview lists.
func (m *Model) formatSubAgentLines(subs []store.Subagent) []string {
	var lines []string
	for _, sub := range subs {
		icon := "·"
		switch store.SubagentStatus(sub.Status) {
		case store.SubagentRunning:
			icon = "⟳"
		case store.SubagentCompleted:
			icon = "✓"
		case store.SubagentErrored:
			icon = "✗"
		}
		tool := "(idle)"
		if sub.CurrentTool.Valid && sub.CurrentTool.String != "" {
			tool = sub.CurrentTool.String
			if sub.CurrentToolStartedAt.Valid {
				tool += " ⏱" + humanAge(sub.CurrentToolStartedAt.Time)
			}
		}
		label := subagentLabel(sub)
		lines = append(lines, fmt.Sprintf("%s %s · %s", icon, label, tool))
	}
	return lines
}

// renderOutputBox draws the "Output" sub-panel. A single
// accent bar + label acts as the section divider; the content
// flows into the parent's padding with no nested border.
func (m *Model) renderOutputBox(row store.Session, bodyHeight, innerWidth int) string {
	contentWidth := innerWidth - 2
	if contentWidth < 10 {
		contentWidth = 10
	}

	bar := lipgloss.NewStyle().
		Foreground(m.theme.Accent).
		Bold(true).
		Render("▎")
	label := lipgloss.NewStyle().
		Foreground(m.theme.Accent).
		Bold(true).
		Render("OUTPUT")
	header := bar + " " + label

	lines := m.outputSectionLines(row, bodyHeight, contentWidth)
	for len(lines) < bodyHeight {
		lines = append(lines, "")
	}

	return header + "\n" + strings.Join(lines, "\n")
}

// outputSectionLines returns the rendered body of the Output
// section: the last maxLines lines of the live tmux capture-pane
// output for the session, truncated to innerWidth. When there is
// no capture (e.g. the tick hasn't landed yet or the pane is
// empty), a muted placeholder is shown instead so the section
// still takes a consistent amount of space.
func (m *Model) outputSectionLines(row store.Session, maxLines, innerWidth int) []string {
	if maxLines <= 0 {
		maxLines = 3
	}
	// Only show capture when it belongs to this session; a stale
	// capture from the previous selection would mislead the user.
	if m.captureSessionName != row.Name || strings.TrimSpace(m.capture) == "" {
		return []string{m.theme.MutedText.Render("(waiting for tmux output...)")}
	}

	// Capture includes terminal escape codes (tmux -e) so the OSC
	// attention scanner can see them. The Output section is a plain
	// text view, so strip every escape before measuring / truncating
	// to avoid lipgloss wrapping half-open CSI/OSC fragments.
	cleaned := stripEscapes(m.capture)
	lines := strings.Split(cleaned, "\n")
	// Trim trailing empty lines from the capture so the visible
	// region is packed with content, not whitespace.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	for i, l := range lines {
		if visibleLen(l) > innerWidth-2 {
			lines[i] = truncate(l, innerWidth-2)
		}
	}
	return lines
}

func (m *Model) renderSection(title string, lines []string) string {
	// Each section gets a left-side accent bar ("▎") next to
	// its title, then the body in Text colour. Using fg-only
	// styling sidesteps the lipgloss background-padding bugs
	// that tripped up the header earlier.
	bar := lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Render("▎")
	titleStyled := lipgloss.NewStyle().
		Foreground(m.theme.Accent).
		Bold(true).
		Render(strings.ToUpper(title))
	bodyStyle := lipgloss.NewStyle().Foreground(m.theme.Text)
	body := bodyStyle.Render(strings.Join(lines, "\n"))
	return bar + " " + titleStyled + "\n" + body
}

func (m *Model) renderStatusBar() string {
	// Two-row footer keeps each line readable on terminals as
	// narrow as 100 cols. Row 1 = navigation + actions; row 2
	// = filters + secondary commands. Toast / error / selection
	// hints get prepended to row 1 because they are the most
	// time-sensitive feedback.
	keyHint := func(key, label string) string {
		return lipgloss.NewStyle().
			Foreground(m.theme.Highlight).
			Bold(true).
			Render(key) + " " +
			lipgloss.NewStyle().
				Foreground(m.theme.Text).
				Render(label)
	}

	row1 := strings.Join([]string{
		keyHint("?", "help"),
		keyHint("n", "new"),
		keyHint("⏎", "attach"),
		keyHint("Space", "mark"),
		keyHint("K", "kill"),
		keyHint("r", "respawn"),
		keyHint("f", "fork"),
		keyHint("d", "diff"),
		keyHint("c", "conductor"),
	}, "  ")

	row2 := strings.Join([]string{
		keyHint("!@#$", "filter"),
		keyHint("N", "next-attn"),
		keyHint("D", "dismiss"),
		keyHint("E", "log"),
		keyHint("m", "mcp"),
		keyHint("s", "skill"),
		keyHint(",", "settings"),
		keyHint("q", "quit"),
	}, "  ")

	// Annotations get prepended to row 1 in priority order so
	// the most urgent thing is always farthest left.
	var prefix []string
	if m.error != "" {
		prefix = append(prefix, m.theme.AttentionTag.Render("✗ "+m.error))
	}
	if n := len(m.errorLog); n > 0 {
		prefix = append(prefix, m.theme.AttentionTag.Render(fmt.Sprintf("[!%d]", n)))
	}
	if m.topToast != "" && time.Now().Before(m.topToastExpires) {
		prefix = append(prefix, m.theme.SectionTitle.Render("• "+m.topToast))
	}
	if n := len(m.selected); n > 0 {
		prefix = append(prefix, m.theme.AttentionTag.Render(fmt.Sprintf("%d selected", n)))
	}
	if len(prefix) > 0 {
		row1 = strings.Join(prefix, "  ") + "  │  " + row1
	}

	bar := m.theme.StatusBar.Render(row1) + "\n" + m.theme.StatusBar.Render(row2)
	return bar
}

// renderErrorLog draws the full-screen error history. Rows are
// listed newest-first so the most recent incidents are visible
// immediately.
func (m *Model) renderErrorLog() string {
	if len(m.errorLog) == 0 {
		body := m.theme.MutedText.Render(
			"No errors recorded yet.") + "\n\n" +
			m.theme.MutedText.Render("q / Esc to return")
		return m.theme.BorderBox.
			Width(m.width - 4).
			Height(m.height - 2).
			Render(m.theme.SectionTitle.Render("Error log") + "\n\n" + body)
	}

	var lines []string
	lines = append(lines, m.theme.SectionTitle.Render("Error log")+"\n")
	// Iterate newest-first.
	for i := len(m.errorLog) - 1; i >= 0; i-- {
		e := m.errorLog[i]
		ts := e.When.Local().Format("15:04:05")
		lvl := e.Level
		switch lvl {
		case "error":
			lvl = m.theme.AttentionTag.Render("error")
		case "warn":
			lvl = m.theme.SectionTitle.Render("warn")
		default:
			lvl = m.theme.MutedText.Render(lvl)
		}
		title := e.Title
		if title == "" {
			title = "(untitled)"
		}
		line := fmt.Sprintf("%s  %s  %s", ts, lvl, title)
		if e.Body != "" {
			line += "\n    " + m.theme.MutedText.Render(e.Body)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "",
		m.theme.MutedText.Render("q / Esc to return"))

	return m.theme.BorderBox.
		Width(m.width - 4).
		Height(m.height - 2).
		Render(strings.Join(lines, "\n"))
}

// renderConfirm draws the "are you sure?" modal. It floats in the
// centre of the screen and uses the attention colour so it reads
// as unmistakably destructive — the user should never mistake it
// for a passive informational overlay.
func (m *Model) renderConfirm() string {
	c := m.confirm
	yes := c.yesLabel
	if yes == "" {
		yes = "confirm"
	}
	lines := []string{
		m.theme.AttentionTag.Render("⚠ " + c.title),
		"",
		c.body,
		"",
		m.theme.MutedText.Render("[y] " + yes + "    [n / Esc] cancel"),
	}
	width := m.width / 2
	if width < 50 {
		width = 50
	}
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Attention).
		Padding(1, 2).
		Width(width).
		Render(strings.Join(lines, "\n"))
	// Place the box in the geometric centre of the screen so the
	// user's eyes always know where to look; lipgloss.Place pads
	// with whitespace on every side.
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) renderHelp() string {
	help := []string{
		"mosaic key bindings",
		"",
		"  j/↓     move down",
		"  k/↑     move up",
		"  h/←     collapse / parent",
		"  l/→     expand",
		"  enter/a attach to selected session",
		"  Space   mark / unmark current session",
		"  *       mark all visible sessions",
		"  K       kill selected session(s)",
		"  r       respawn stopped session(s)",
		"  R       refresh",
		"  /       filter",
		"  n       new session wizard",
		"  m       MCP manager (attach / detach servers)",
		"  s       Skill manager (attach / detach skills)",
		"  c       Conductor panel (DAG runs + tasks)",
		"  d       Diff view (l:full · s:stage · c:commit · y/Y:yank · /:search)",
		"  ,       Settings (read-only)",
		"  ?       toggle help",
		"  q/Ctrl+C quit",
		"",
		"  inside an attached tmux session:",
		"    Ctrl+Q  detach back to mosaic",
	}
	return m.theme.BorderBox.
		Width(m.width - 4).
		Height(m.height - 2).
		Render(strings.Join(help, "\n"))
}

func (m *Model) selectedSession() (store.Session, bool) {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return store.Session{}, false
	}
	if m.rows[m.cursor].IsGroup {
		return store.Session{}, false
	}
	return m.rows[m.cursor].Session, true
}

func ifEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// truncateMiddle is the middle-ellipsis variant of [truncate].
// It keeps both the beginning and the end of the string, dropping
// characters from the middle. Use it for paths where the
// distinguishing part (filename, extension) lives at the right
// edge — `packages/foo/bar/.../widget.tsx` reads better than
// `packages/foo/bar/baz/qux/…`.
func truncateMiddle(s string, n int) string {
	if n <= 1 {
		return s[:1]
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	keep := n - 1 // 1 cell for the ellipsis
	left := keep / 2
	right := keep - left
	return string(r[:left]) + "…" + string(r[len(r)-right:])
}

func humanAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
