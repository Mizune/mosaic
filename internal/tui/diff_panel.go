package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/git"
	"github.com/Mizune/mosaic/internal/service"
)

// diffPanelMode tracks which sub-view of the Diff panel is active.
type diffPanelMode int

const (
	// diffModeBrowse is the two-pane file list + diff preview.
	diffModeBrowse diffPanelMode = iota

	// diffModeFullFile is the drill-in: a single file's diff,
	// no file list, more vertical room for the content.
	diffModeFullFile

	// diffModeCommit is the commit-message input modal that
	// floats on top of the browse view.
	diffModeCommit
)

// diffFilterMode restricts the left-pane rows to a subset of the
// change set.
type diffFilterMode int

const (
	diffFilterAll diffFilterMode = iota
	diffFilterModified
	diffFilterNew
	diffFilterStaged
)

func (f diffFilterMode) label() string {
	switch f {
	case diffFilterModified:
		return "modified"
	case diffFilterNew:
		return "new"
	case diffFilterStaged:
		return "staged"
	default:
		return "all"
	}
}

// diffPanelState is the Diff View modal's payload. The state is
// re-fetched from disk on open and on `R` refresh; per-file diffs
// are loaded lazily and cached so navigation stays snappy.
type diffPanelState struct {
	sessionName string

	result  service.DiffResult
	loadErr string

	cursor     int
	scrollY    int
	fileDiffs  map[string]string // path → unified diff text
	diffLoadEr map[string]string // path → error text (shown inline)

	// hunkPreambles / fileHunks are the cached parse of each
	// visited file's diff into a preamble + hunks. The Diff
	// View uses them for hunk-level staging ([ / ] navigation,
	// H to stage). Keys match fileDiffs.
	hunkPreambles map[string]string
	fileHunks     map[string][]git.DiffHunk
	hunkCursor    int // index into the current file's hunks

	mode   diffPanelMode
	filter diffFilterMode

	// Filename filter ("/" key) — when filterInput.active is true
	// the panel renders the input line at the bottom and filters
	// the file list by substring on every keystroke.
	filterInput diffFilterInput

	// Commit modal state — only populated while mode == diffModeCommit.
	commit diffCommitState

	// Toast is a short transient message (yank, stage, commit) the
	// footer shows for a couple of frames.
	toast        string
	toastExpires time.Time
}

// diffFilterInput backs the `/` substring search over filenames.
type diffFilterInput struct {
	active bool
	query  string
}

// diffCommitState backs the inline commit modal.
type diffCommitState struct {
	message string
	amend   bool
	err     string
}

// openDiffPanel opens the Diff View modal for the session currently
// under the cursor. Group headers and sub-agent rows have no
// worktree of their own so the key is a no-op there.
func (m *Model) openDiffPanel() {
	if len(m.rows) == 0 {
		return
	}
	row := m.rows[m.cursor]
	if row.IsGroup || row.IsSubagent {
		return
	}

	state := diffPanelState{
		sessionName:   row.Session.Name,
		fileDiffs:     map[string]string{},
		diffLoadEr:    map[string]string{},
		hunkPreambles: map[string]string{},
		fileHunks:     map[string][]git.DiffHunk{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := m.sessions.Diff(ctx, row.Session.Name)
	if err != nil {
		state.loadErr = err.Error()
	} else {
		state.result = res
	}

	m.diffPanel = state
	m.screen = ScreenDiff

	// Prefetch the first file's diff so the right pane is not
	// empty on open. Everything else loads on demand.
	m.ensureDiffLoaded()
}

// closeDiffPanel returns to TOP and releases panel state.
func (m *Model) closeDiffPanel() {
	m.diffPanel = diffPanelState{}
	m.screen = ScreenTop
}

// filteredFiles returns the files array after applying the current
// filter mode and filename search. It is recomputed on demand so
// mode switches take effect in O(n) without keeping a duplicate
// slice in the state.
func (p diffPanelState) filteredFiles() []git.DiffFileStat {
	out := make([]git.DiffFileStat, 0, len(p.result.Files))
	needle := strings.ToLower(p.filterInput.query)
	for _, f := range p.result.Files {
		switch p.filter {
		case diffFilterModified:
			if f.Untracked || f.Status == "?" {
				continue
			}
		case diffFilterNew:
			if !f.Untracked && f.Status != "A" {
				continue
			}
		case diffFilterStaged:
			if !f.Staged {
				continue
			}
		}
		if needle != "" && !strings.Contains(strings.ToLower(f.Path), needle) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// currentFile returns the file the cursor is currently on *after*
// filtering. It is the only place that maps cursor → file; every
// render / keybind goes through it so cursor drift after filter
// changes is handled in one spot.
func (p diffPanelState) currentFile() (git.DiffFileStat, bool) {
	files := p.filteredFiles()
	if len(files) == 0 {
		return git.DiffFileStat{}, false
	}
	if p.cursor < 0 {
		return files[0], true
	}
	if p.cursor >= len(files) {
		return files[len(files)-1], true
	}
	return files[p.cursor], true
}

func (m *Model) updateDiffPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The commit modal owns the keyboard while it is up so j/k
	// and other shortcuts do not leak into the browse view.
	if m.diffPanel.mode == diffModeCommit {
		return m.updateDiffCommitModal(msg)
	}
	// The filename search captures printable keys while it's
	// active so users can type a query without hitting the
	// browse shortcuts.
	if m.diffPanel.filterInput.active {
		return m.updateDiffFilterInput(msg)
	}

	switch msg.String() {
	case "q", "esc":
		if m.diffPanel.mode == diffModeFullFile {
			m.diffPanel.mode = diffModeBrowse
			m.diffPanel.scrollY = 0
			return m, nil
		}
		m.closeDiffPanel()
		return m, nil

	case "j", "down":
		files := m.diffPanel.filteredFiles()
		if m.diffPanel.cursor < len(files)-1 {
			m.diffPanel.cursor++
			m.diffPanel.scrollY = 0
			m.ensureDiffLoaded()
		}
		return m, nil

	case "k", "up":
		if m.diffPanel.cursor > 0 {
			m.diffPanel.cursor--
			m.diffPanel.scrollY = 0
			m.ensureDiffLoaded()
		}
		return m, nil

	case "J", "pgdown":
		m.diffPanel.scrollY += 10
		return m, nil

	case "K", "pgup":
		m.diffPanel.scrollY -= 10
		if m.diffPanel.scrollY < 0 {
			m.diffPanel.scrollY = 0
		}
		return m, nil

	case "l", "enter":
		m.diffPanel.mode = diffModeFullFile
		m.diffPanel.scrollY = 0
		m.ensureDiffLoaded()
		return m, nil

	case "h":
		if m.diffPanel.mode == diffModeFullFile {
			m.diffPanel.mode = diffModeBrowse
			m.diffPanel.scrollY = 0
		}
		return m, nil

	case "R":
		return m, m.refreshDiffPanel()

	case "/":
		m.diffPanel.filterInput.active = true
		return m, nil

	case "a":
		m.setDiffFilter(diffFilterAll)
		return m, nil

	case "M":
		m.setDiffFilter(diffFilterModified)
		return m, nil

	case "N":
		m.setDiffFilter(diffFilterNew)
		return m, nil

	case "S":
		m.setDiffFilter(diffFilterStaged)
		return m, nil

	case "y":
		return m, m.yankCurrentFileDiff()

	case "Y":
		return m, m.yankAllDiffs()

	case "s":
		return m, m.toggleStageCurrentFile()

	case "H":
		// Stage just the hunk currently focused in the right
		// pane. Leaves the rest of the file untouched — the
		// lazygit-style progressive commit workflow.
		return m, m.stageCurrentHunk()

	case "[":
		if m.diffPanel.hunkCursor > 0 {
			m.diffPanel.hunkCursor--
			m.diffPanel.scrollY = 0
		}
		return m, nil

	case "]":
		file, ok := m.diffPanel.currentFile()
		if ok {
			if hs := m.diffPanel.fileHunks[file.Path]; m.diffPanel.hunkCursor < len(hs)-1 {
				m.diffPanel.hunkCursor++
				m.diffPanel.scrollY = 0
			}
		}
		return m, nil

	case "c":
		m.diffPanel.mode = diffModeCommit
		m.diffPanel.commit = diffCommitState{}
		return m, nil

	case "C":
		// Amend the previous commit: pre-fill with the HEAD
		// commit message so the user can edit it in place.
		m.diffPanel.mode = diffModeCommit
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		msg, err := m.sessions.LastCommitMessage(ctx, m.diffPanel.sessionName)
		if err != nil {
			m.diffPanel.commit = diffCommitState{err: err.Error()}
		} else {
			m.diffPanel.commit = diffCommitState{message: msg, amend: true}
		}
		return m, nil
	}
	return m, nil
}

// updateDiffFilterInput handles keys while the `/` filename search
// input is active.
func (m *Model) updateDiffFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.diffPanel.filterInput = diffFilterInput{}
		m.diffPanel.cursor = 0
		m.ensureDiffLoaded()
		return m, nil
	case "enter":
		m.diffPanel.filterInput.active = false
		m.diffPanel.cursor = 0
		m.ensureDiffLoaded()
		return m, nil
	case "backspace":
		if n := len(m.diffPanel.filterInput.query); n > 0 {
			m.diffPanel.filterInput.query = m.diffPanel.filterInput.query[:n-1]
			m.diffPanel.cursor = 0
		}
		return m, nil
	}
	if r := msg.String(); len(r) == 1 {
		m.diffPanel.filterInput.query += r
		m.diffPanel.cursor = 0
	}
	return m, nil
}

// updateDiffCommitModal handles keys while the inline commit modal
// is up.
func (m *Model) updateDiffCommitModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.diffPanel.mode = diffModeBrowse
		m.diffPanel.commit = diffCommitState{}
		return m, nil
	case "enter":
		return m, m.performDiffCommit()
	case "backspace":
		if n := len(m.diffPanel.commit.message); n > 0 {
			m.diffPanel.commit.message = m.diffPanel.commit.message[:n-1]
		}
		return m, nil
	}
	if r := msg.String(); len(r) == 1 {
		m.diffPanel.commit.message += r
	}
	return m, nil
}

// setDiffFilter switches the filter mode, resets the cursor so the
// right pane does not point at a file that just got filtered out,
// and kicks the prefetch.
func (m *Model) setDiffFilter(mode diffFilterMode) {
	if m.diffPanel.filter == mode {
		return
	}
	m.diffPanel.filter = mode
	m.diffPanel.cursor = 0
	m.diffPanel.scrollY = 0
	m.ensureDiffLoaded()
	m.setDiffToast("filter: " + mode.label())
}

// setDiffToast displays a short message at the bottom of the Diff
// View for a couple of seconds. It is used for yank / stage /
// commit feedback so the user sees a confirmation.
func (m *Model) setDiffToast(text string) {
	m.diffPanel.toast = text
	m.diffPanel.toastExpires = time.Now().Add(3 * time.Second)
}

// refreshDiffPanel reloads the Diff View from disk while preserving
// the cursor and filter state.
func (m *Model) refreshDiffPanel() tea.Cmd {
	name := m.diffPanel.sessionName
	cursor := m.diffPanel.cursor
	filter := m.diffPanel.filter
	search := m.diffPanel.filterInput

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		newState := diffPanelState{
			sessionName: name,
			fileDiffs:   map[string]string{},
			diffLoadEr:  map[string]string{},
			cursor:      cursor,
			filter:      filter,
			filterInput: search,
		}
		res, err := m.sessions.Diff(ctx, name)
		if err != nil {
			newState.loadErr = err.Error()
		} else {
			newState.result = res
			if newState.cursor >= len(newState.filteredFiles()) {
				newState.cursor = 0
			}
		}
		return diffPanelRefreshedMsg{state: newState}
	}
}

// diffPanelRefreshedMsg carries a rebuilt panel state back from
// the refresh goroutine.
type diffPanelRefreshedMsg struct {
	state diffPanelState
}

// ensureDiffLoaded populates fileDiffs (and the hunk parse) for
// the currently selected file if it has not been fetched yet.
// Errors are stashed into diffLoadEr so the right pane can
// explain what went wrong instead of looking empty.
func (m *Model) ensureDiffLoaded() {
	p := &m.diffPanel
	file, ok := p.currentFile()
	if !ok {
		return
	}
	if _, loaded := p.fileDiffs[file.Path]; loaded {
		// Already cached; reset hunk cursor so the right pane
		// starts at the first hunk of the newly selected file.
		p.hunkCursor = 0
		return
	}
	if _, failed := p.diffLoadEr[file.Path]; failed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	diff, err := m.sessions.DiffFile(ctx, p.sessionName, file.Path, file.Untracked)
	if err != nil {
		p.diffLoadEr[file.Path] = err.Error()
		return
	}
	p.fileDiffs[file.Path] = diff

	preamble, hunks := git.ParseDiffHunks(diff)
	if p.hunkPreambles == nil {
		p.hunkPreambles = map[string]string{}
	}
	if p.fileHunks == nil {
		p.fileHunks = map[string][]git.DiffHunk{}
	}
	p.hunkPreambles[file.Path] = preamble
	p.fileHunks[file.Path] = hunks
	p.hunkCursor = 0
}

// yankCurrentFileDiff copies the currently selected file's diff to
// the OS clipboard via atotto/clipboard (shells out to pbcopy /
// xclip / wl-copy depending on the platform).
func (m *Model) yankCurrentFileDiff() tea.Cmd {
	file, ok := m.diffPanel.currentFile()
	if !ok {
		return nil
	}
	body, loaded := m.diffPanel.fileDiffs[file.Path]
	if !loaded {
		m.setDiffToast("diff still loading…")
		return nil
	}
	if err := clipboard.WriteAll(body); err != nil {
		m.setDiffToast("yank failed: " + err.Error())
		return nil
	}
	m.setDiffToast("yanked " + file.Path)
	return nil
}

// yankAllDiffs concatenates every filtered file's cached diff into
// one blob and copies it to the clipboard. Files the user has not
// visited yet are loaded synchronously.
func (m *Model) yankAllDiffs() tea.Cmd {
	files := m.diffPanel.filteredFiles()
	if len(files) == 0 {
		return nil
	}
	var b strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, f := range files {
		body, ok := m.diffPanel.fileDiffs[f.Path]
		if !ok {
			loaded, err := m.sessions.DiffFile(ctx, m.diffPanel.sessionName, f.Path, f.Untracked)
			if err != nil {
				m.diffPanel.diffLoadEr[f.Path] = err.Error()
				continue
			}
			m.diffPanel.fileDiffs[f.Path] = loaded
			body = loaded
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
	}
	if err := clipboard.WriteAll(b.String()); err != nil {
		m.setDiffToast("yank failed: " + err.Error())
		return nil
	}
	m.setDiffToast(fmt.Sprintf("yanked %d files", len(files)))
	return nil
}

// stageCurrentHunk pipes a synthetic mini-patch containing just
// the currently focused hunk to `git apply --cached`. File-level
// staging (`s`) covers the common case; `H` is for the lazygit-
// style "commit only this change" workflow.
func (m *Model) stageCurrentHunk() tea.Cmd {
	file, ok := m.diffPanel.currentFile()
	if !ok {
		return nil
	}
	hunks := m.diffPanel.fileHunks[file.Path]
	if len(hunks) == 0 {
		m.setDiffToast("no hunks to stage")
		return nil
	}
	idx := m.diffPanel.hunkCursor
	if idx < 0 || idx >= len(hunks) {
		return nil
	}
	hunk := hunks[idx]
	preamble := m.diffPanel.hunkPreambles[file.Path]
	name := m.diffPanel.sessionName

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.sessions.StageHunk(ctx, name, preamble, hunk); err != nil {
			return diffPanelToastMsg{text: "stage hunk: " + err.Error()}
		}
		return m.refreshDiffPanel()()
	}
}

// toggleStageCurrentFile stages the current file when it is
// unstaged, or unstages it when it is already staged. Either way
// we trigger a refresh so the +/-/staged markers update.
func (m *Model) toggleStageCurrentFile() tea.Cmd {
	file, ok := m.diffPanel.currentFile()
	if !ok {
		return nil
	}
	name := m.diffPanel.sessionName

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		var err error
		if file.Staged {
			err = m.sessions.UnstageFile(ctx, name, file.Path)
		} else {
			err = m.sessions.StageFile(ctx, name, file.Path)
		}
		if err != nil {
			return diffPanelToastMsg{text: "stage failed: " + err.Error()}
		}
		return m.refreshDiffPanel()()
	}
}

// diffPanelToastMsg is the Cmd return type for stage/commit
// failures that need the main Update to surface a toast without
// kicking a full refresh.
type diffPanelToastMsg struct {
	text string
}

// performDiffCommit runs `git commit -m <message>` (or
// `git commit --amend -m <message>` in amend mode) with whatever
// the user typed into the commit modal, and refreshes the file
// list so the newly-committed files disappear from "staged" view.
func (m *Model) performDiffCommit() tea.Cmd {
	message := strings.TrimSpace(m.diffPanel.commit.message)
	if message == "" {
		m.diffPanel.commit.err = "commit message is required"
		return nil
	}
	name := m.diffPanel.sessionName
	amend := m.diffPanel.commit.amend

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var err error
		if amend {
			err = m.sessions.CommitAmend(ctx, name, message)
		} else {
			err = m.sessions.Commit(ctx, name, message)
		}
		if err != nil {
			return diffPanelCommitResultMsg{err: err.Error()}
		}
		return diffPanelCommitResultMsg{success: true}
	}
}

// diffPanelCommitResultMsg carries the outcome of an inline commit
// so the main Update can flip the panel mode + refresh.
type diffPanelCommitResultMsg struct {
	success bool
	err     string
}

// renderDiffPanel draws the Diff View modal in whichever mode is
// currently active.
func (m *Model) renderDiffPanel() string {
	switch m.diffPanel.mode {
	case diffModeFullFile:
		return m.renderDiffFullFile()
	case diffModeCommit:
		return m.renderDiffCommitModal()
	default:
		return m.renderDiffBrowse()
	}
}

// renderDiffBrowse draws the two-pane browse view.
func (m *Model) renderDiffBrowse() string {
	p := m.diffPanel

	title := m.theme.SectionTitle.Render("Diff: " + p.sessionName)

	if p.loadErr != "" {
		body := m.theme.AttentionTag.Render("✗ "+p.loadErr) + "\n\n" +
			m.theme.MutedText.Render("q / Esc to return")
		return m.renderMCPBox(title + "\n\n" + body)
	}

	files := p.filteredFiles()
	if len(files) == 0 {
		msg := "No changes against " + p.result.BaseRef + "."
		if p.filter != diffFilterAll || p.filterInput.query != "" {
			msg = fmt.Sprintf("No files match filter %q / search %q.",
				p.filter.label(), p.filterInput.query)
		}
		body := m.theme.MutedText.Render(msg) +
			"\n\n" + m.theme.MutedText.Render("q / Esc to return")
		return m.renderMCPBox(title + "\n\n" + body + "\n\n" + m.renderDiffSearchLine())
	}

	// 40% / 60% split favours the file list slightly so long
	// monorepo-style paths (`packages/foo/bar/.../widget.tsx`)
	// stay readable, while the diff body still has plenty of
	// horizontal real estate for code.
	leftWidth := m.width * 2 / 5
	if leftWidth < 36 {
		leftWidth = 36
	}
	rightWidth := m.width - leftWidth - 10
	if rightWidth < 30 {
		rightWidth = 30
	}

	left := renderDiffFileList(m, p, files, leftWidth)
	right := renderDiffBody(m, p, files, rightWidth, m.height-12)

	leftBox := lipgloss.NewStyle().Width(leftWidth).Render(left)
	rightBox := lipgloss.NewStyle().Width(rightWidth).Render(right)
	cols := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	footer := m.theme.MutedText.Render(
		fmt.Sprintf(
			"base %s  •  %s  •  a/M/N/S filter · /:search · l:full · s:stage · H:hunk · [/]:nav · c:commit · C:amend · y/Y:yank · R:refresh",
			p.result.BaseRef, p.filter.label()))
	toast := m.renderDiffToast()
	search := m.renderDiffSearchLine()

	body := title + "\n\n" + cols + "\n\n" + footer
	if search != "" {
		body += "\n" + search
	}
	if toast != "" {
		body += "\n" + toast
	}
	return m.renderMCPBox(body)
}

// renderDiffFullFile draws the drill-in view for a single file —
// no file list, the whole modal body is the unified diff.
func (m *Model) renderDiffFullFile() string {
	p := m.diffPanel
	file, ok := p.currentFile()
	if !ok {
		return m.renderDiffBrowse()
	}

	title := m.theme.SectionTitle.Render("Diff: " + p.sessionName + " — " + file.Path)

	diff, loaded := p.fileDiffs[file.Path]
	if err, failed := p.diffLoadEr[file.Path]; failed {
		body := m.theme.AttentionTag.Render("load failed: " + err)
		return m.renderMCPBox(title + "\n\n" + body)
	}
	if !loaded {
		return m.renderMCPBox(title + "\n\n" + m.theme.MutedText.Render("(loading...)"))
	}

	maxLines := m.height - 10
	if maxLines < 10 {
		maxLines = 10
	}
	lines := strings.Split(diff, "\n")
	if p.scrollY >= len(lines) {
		p.scrollY = max(0, len(lines)-1)
	}
	lines = lines[p.scrollY:]
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for i, line := range lines {
		lines[i] = colourDiffLine(m, line, file.Path, m.width-12)
	}
	body := strings.Join(lines, "\n")
	total := len(strings.Split(diff, "\n"))
	if p.scrollY+maxLines < total {
		body += "\n" + m.theme.MutedText.Render(
			fmt.Sprintf("… %d more lines (J to scroll)", total-(p.scrollY+maxLines)))
	}

	footer := m.theme.MutedText.Render(
		"J/K: scroll   y: yank   s: stage   h/q/Esc: back")
	out := title + "\n\n" + body + "\n\n" + footer
	if t := m.renderDiffToast(); t != "" {
		out += "\n" + t
	}
	return m.renderMCPBox(out)
}

// renderDiffCommitModal draws the inline commit message input.
func (m *Model) renderDiffCommitModal() string {
	p := m.diffPanel

	titleText := "Commit staged changes"
	if p.commit.amend {
		titleText = "Amend last commit"
	}
	title := m.theme.SectionTitle.Render(titleText)

	input := "> " + p.commit.message + "_"
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Accent).
		Padding(0, 1).
		Width(m.width / 2).
		Render(input)

	lines := []string{
		title,
		"",
		m.theme.MutedText.Render("Type the commit message. Enter to commit, Esc to cancel."),
		"",
		box,
	}
	if p.commit.err != "" {
		lines = append(lines, "", m.theme.AttentionTag.Render("✗ "+p.commit.err))
	}
	body := strings.Join(lines, "\n")
	return m.renderMCPBox(body)
}

// renderDiffFileList builds the left pane: status glyph, path,
// and +N/-N counts. filtered is the slice filteredFiles() returned.
func renderDiffFileList(m *Model, p diffPanelState, filtered []git.DiffFileStat, width int) string {
	lines := make([]string, 0, len(filtered))
	for i, f := range filtered {
		glyph := diffStatusGlyph(f.Status)
		stats := formatDiffStats(f)
		stageMark := " "
		if f.Staged {
			stageMark = "●"
		}
		statsLen := visibleLen(stats)
		pathBudget := width - 4 /*indent*/ - 2 /*glyph+mark*/ - statsLen - 3
		if pathBudget < 8 {
			pathBudget = 8
		}
		path := f.Path
		if visibleLen(path) > pathBudget {
			// Middle-ellipsis so the filename (the part the
			// user actually distinguishes files by) is always
			// visible: `packages/foo/.../widget.tsx` instead
			// of `packages/foo/bar/baz/qu…`.
			path = truncateMiddle(path, pathBudget)
		}
		padding := strings.Repeat(" ", max(0, pathBudget-visibleLen(path)))
		text := glyph + stageMark + " " + path + padding + " " + stats
		if i == p.cursor {
			text = m.theme.AttentionTag.Render("▶ " + text)
		} else {
			text = "  " + text
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

// renderDiffBody builds the right pane: the unified diff for the
// selected file, coloured with green/red for +/- lines and muted
// for hunk headers. It respects the scroll offset and a max line
// budget so the modal never overflows vertically.
func renderDiffBody(m *Model, p diffPanelState, filtered []git.DiffFileStat, width, maxLines int) string {
	if len(filtered) == 0 {
		return ""
	}
	file := filtered[p.cursor]
	if err, ok := p.diffLoadEr[file.Path]; ok {
		return m.theme.AttentionTag.Render("load failed: " + err)
	}
	diff, ok := p.fileDiffs[file.Path]
	if !ok {
		return m.theme.MutedText.Render("(loading...)")
	}
	if strings.TrimSpace(diff) == "" {
		return m.theme.MutedText.Render("(no textual diff — probably a binary or metadata-only change)")
	}

	if maxLines < 10 {
		maxLines = 10
	}
	lines := strings.Split(diff, "\n")
	if p.scrollY >= len(lines) {
		p.scrollY = max(0, len(lines)-1)
	}
	lines = lines[p.scrollY:]
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for i, line := range lines {
		lines[i] = colourDiffLine(m, line, file.Path, width-2)
	}
	body := strings.Join(lines, "\n")
	total := len(strings.Split(diff, "\n"))
	if p.scrollY+maxLines < total {
		body += "\n" + m.theme.MutedText.Render(
			fmt.Sprintf("… %d more lines (J to scroll)", total-(p.scrollY+maxLines)))
	}
	return body
}

// renderDiffToast returns the toast line when one is active, or an
// empty string otherwise. Callers stitch it into the modal body.
func (m *Model) renderDiffToast() string {
	if m.diffPanel.toast == "" {
		return ""
	}
	if time.Now().After(m.diffPanel.toastExpires) {
		return ""
	}
	return m.theme.SectionTitle.Render("• " + m.diffPanel.toast)
}

// renderDiffSearchLine returns the `/` search prompt when active.
func (m *Model) renderDiffSearchLine() string {
	if !m.diffPanel.filterInput.active {
		if q := m.diffPanel.filterInput.query; q != "" {
			return m.theme.MutedText.Render("search: " + q + " (/ to edit)")
		}
		return ""
	}
	return m.theme.SectionTitle.Render("search: ") + m.diffPanel.filterInput.query + "_"
}

// colourDiffLine applies a foreground colour to a single diff line.
// Hunk headers (@@ … @@) and file markers (+++ / ---) use the muted
// text style so they visually separate from the content lines. The
// filename is carried through so chroma can pick the right lexer
// when syntax highlighting is enabled.
func colourDiffLine(m *Model, line, path string, width int) string {
	plain := line
	if visibleLen(plain) > width {
		plain = truncate(plain, width)
	}
	switch {
	case strings.HasPrefix(plain, "+++") || strings.HasPrefix(plain, "---"):
		return m.theme.MutedText.Render(plain)
	case strings.HasPrefix(plain, "@@"):
		return m.theme.SectionTitle.Render(plain)
	case strings.HasPrefix(plain, "+"):
		code := plain[1:]
		return m.theme.RunningTag.Render("+") + highlightDiffContent(code, path, m.theme.Name)
	case strings.HasPrefix(plain, "-"):
		code := plain[1:]
		return m.theme.AttentionTag.Render("-") + highlightDiffContent(code, path, m.theme.Name)
	default:
		return plain
	}
}

// diffStatusGlyph returns the single-character badge for a file's
// change status. The glyph is themed via colour at the row level.
func diffStatusGlyph(status string) string {
	switch status {
	case "M":
		return "M"
	case "A":
		return "A"
	case "D":
		return "D"
	case "R":
		return "R"
	case "?":
		return "?"
	default:
		return "·"
	}
}

// formatDiffStats renders the +N/-N column. Binary files and
// untracked files show "new" / em-dash so the column stays short.
func formatDiffStats(f git.DiffFileStat) string {
	if f.Untracked {
		return "new"
	}
	if f.Inserted == 0 && f.Deleted == 0 {
		return "—"
	}
	return fmt.Sprintf("+%d/-%d", f.Inserted, f.Deleted)
}

// lexerName maps a file path to the chroma lexer alias that should
// highlight its content. The mapping is deliberately small — the
// Diff View is not a full code editor, so a handful of popular
// languages and a `fallback` path is enough to look good.
func lexerName(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".sh", ".bash":
		return "bash"
	case ".sql":
		return "sql"
	case ".html":
		return "html"
	case ".css":
		return "css"
	}
	return ""
}
