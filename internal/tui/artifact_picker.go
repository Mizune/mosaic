package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tui/theme"
)

// artifactPicker is the artifact list + inline preview + manual input,
// written so two hosts can drive it: the TUI's ScreenArtifacts modal
// and the standalone popup program behind `mosaic artifacts`. It owns
// no screen switching, no toasts and no terminal output of its own;
// those come back to the host as a pickerAction, a pickerNotice and
// the host-provided sequence queue.
type artifactPicker struct {
	deps        artifactPickerDeps
	sessionID   string
	sessionName string
	items       []store.Artifact
	cursor      int
	input       artifactInput
	loadErr     string
	inline      artifactInlineState
	width       int
	height      int
}

// artifactPickerDeps is everything the picker borrows from its host.
type artifactPickerDeps struct {
	theme *theme.Theme
	// store is read for the list and written for manual rows; nil
	// means "list only from fallback".
	store    *store.Store
	fallback []store.Artifact
	// viewer opens artifacts; nil means Enter only shows a notice.
	viewer artifactViewer
	// queueSeq schedules raw terminal sequences through the host's
	// frame (see termSeqQueue).
	queueSeq func(seq string) tea.Cmd
	// chromeW / chromeH are the cells the host's frame takes around
	// the picker body; zero means the TUI modal frame.
	chromeW int
	chromeH int
}

// Frame insets of the TUI modal (renderModalFrame): width-8 outer box,
// border and padding, plus the title, blank lines and footer.
const (
	modalChromeW = 14
	modalChromeH = 13
)

// artifactInput is the hand-rolled text field behind `+`: open a path
// or URL that was not detected automatically (the only route for
// Codex / Gemini sessions, whose transcripts mosaic does not parse).
type artifactInput struct {
	active     bool
	value      string
	candidates []string
	err        string
}

// pickerAction tells the host what to do after a key.
type pickerAction int

const (
	pickerActionNone pickerAction = iota
	// pickerActionClose asks the host to leave the picker. The
	// accompanying cmd (if any) carries the Kitty delete and must
	// still run.
	pickerActionClose
)

// pickerNotice is a message for the host's status surface.
type pickerNotice struct {
	level pickerNoticeLevel
	title string
	body  string
}

type pickerNoticeLevel int

const (
	noticeNone pickerNoticeLevel = iota
	noticeInfo
	noticeError
)

func infoNotice(title string) pickerNotice { return pickerNotice{level: noticeInfo, title: title} }
func errorNotice(title, body string) pickerNotice {
	return pickerNotice{level: noticeError, title: title, body: body}
}

// newArtifactPicker builds a picker for one session, loads its rows and
// returns the command that starts the first inline preview.
func newArtifactPicker(deps artifactPickerDeps, sessionID, sessionName string) (artifactPicker, tea.Cmd) {
	if deps.chromeW == 0 {
		deps.chromeW = modalChromeW
	}
	if deps.chromeH == 0 {
		deps.chromeH = modalChromeH
	}
	p := artifactPicker{deps: deps, sessionID: sessionID, sessionName: sessionName}
	p.reload()
	p.initInline()
	return p, p.scheduleInline()
}

func (p *artifactPicker) setSize(width, height int) {
	p.width, p.height = width, height
}

// reload re-reads the session's artifacts from the store (not the
// refresh cache, so a just-added manual row shows at once) and clamps
// the cursor into range.
func (p *artifactPicker) reload() {
	p.loadErr = ""
	if p.deps.store == nil {
		p.items = p.deps.fallback
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rows, err := p.deps.store.Artifacts().ListBySession(ctx, p.sessionID)
		if err != nil {
			p.loadErr = err.Error()
			return
		}
		p.items = rows
	}
	if p.cursor >= len(p.items) {
		p.cursor = len(p.items) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// reloadKeepingSelection reloads and keeps the cursor on the artifact
// it was on, so a poll that prepends new rows does not jump the
// preview. It returns a preview command only when the selection moved.
func (p *artifactPicker) reloadKeepingSelection() tea.Cmd {
	selected := p.selectedID()
	p.reload()
	if selected != "" {
		for i, a := range p.items {
			if a.ID == selected {
				p.cursor = i
				break
			}
		}
	}
	return p.scheduleInline()
}

func (p *artifactPicker) selectedID() string {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return ""
	}
	return p.items[p.cursor].ID
}

// updateKey handles one key while the picker is open.
func (p *artifactPicker) updateKey(msg tea.KeyMsg) (pickerAction, pickerNotice, tea.Cmd) {
	if p.input.active {
		n, cmd := p.updateInputKey(msg)
		return pickerActionNone, n, cmd
	}
	switch msg.String() {
	case "q", "esc":
		return pickerActionClose, pickerNotice{}, p.close()
	case "j", "down":
		if p.cursor < len(p.items)-1 {
			p.cursor++
		}
		return pickerActionNone, pickerNotice{}, p.scheduleInline()
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
		return pickerActionNone, pickerNotice{}, p.scheduleInline()
	case "R":
		p.reload()
		return pickerActionNone, pickerNotice{}, p.scheduleInline()
	case "+":
		p.input = artifactInput{active: true}
	case "enter":
		if p.cursor >= 0 && p.cursor < len(p.items) {
			n, cmd := p.openCmd(p.items[p.cursor])
			return pickerActionNone, n, cmd
		}
	}
	return pickerActionNone, pickerNotice{}, nil
}

// updateInputKey handles keystrokes inside the `+` text field. Tab
// completes against the filesystem like the wizard's project field;
// Ctrl+U clears like the Settings editor.
func (p *artifactPicker) updateInputKey(msg tea.KeyMsg) (pickerNotice, tea.Cmd) {
	in := &p.input
	switch msg.String() {
	case "esc":
		*in = artifactInput{}
	case "enter":
		return p.submitManual()
	case "backspace":
		if runes := []rune(in.value); len(runes) > 0 {
			in.value = string(runes[:len(runes)-1])
		}
		in.candidates, in.err = nil, ""
	case "ctrl+u":
		in.value, in.candidates, in.err = "", nil, ""
	case "tab":
		completed, candidates := completePath(in.value)
		in.value = completed
		in.candidates = nil
		if len(candidates) > 1 {
			in.candidates = candidates
		}
	case " ":
		in.value += " "
	default:
		if msg.Type == tea.KeyRunes && !msg.Alt {
			in.value += string(msg.Runes)
			in.candidates, in.err = nil, ""
		}
	}
	return pickerNotice{}, nil
}

// updateMsg handles the picker's own asynchronous messages. handled is
// false for messages that are not the picker's.
func (p *artifactPicker) updateMsg(msg tea.Msg) (handled bool, notice pickerNotice, cmd tea.Cmd) {
	switch v := msg.(type) {
	case artifactOpenedMsg:
		return true, noticeForOpened(v), nil
	case artifactInlineDebounceMsg:
		return true, pickerNotice{}, p.applyInlineDebounce(v)
	case artifactInlineMsg:
		return true, pickerNotice{}, p.applyInlineResult(v)
	}
	return false, pickerNotice{}, nil
}

// close frees the inline image. The host drops the picker afterwards.
func (p *artifactPicker) close() tea.Cmd {
	return p.releaseInline()
}

// openCmd opens a through the host's viewer, or explains why it cannot.
func (p *artifactPicker) openCmd(a store.Artifact) (pickerNotice, tea.Cmd) {
	if p.deps.viewer == nil {
		return infoNotice("viewer disabled ([viewer] enabled = false)"), nil
	}
	return pickerNotice{}, openArtifactCmd(p.deps.viewer, a)
}

// noticeForOpened turns an open result into a toast or an error entry.
func noticeForOpened(v artifactOpenedMsg) pickerNotice {
	if v.err != "" {
		return errorNotice("open artifact failed", v.err)
	}
	return infoNotice("opened " + v.title + " (" + v.backend + ")")
}

// submitManual validates the `+` input, persists it as a manual
// artifact so it reappears in the list, and opens it. Validation
// errors stay inside the input box; the picker remains open.
func (p *artifactPicker) submitManual() (pickerNotice, tea.Cmd) {
	raw := strings.TrimSpace(p.input.value)
	if raw == "" {
		p.input.err = "path or URL is required"
		return pickerNotice{}, nil
	}
	in, err := manualArtifactInput(raw, p.sessionID)
	if err != nil {
		p.input.err = err.Error()
		return pickerNotice{}, nil
	}
	if p.deps.store == nil {
		p.input.err = "store unavailable"
		return pickerNotice{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	row, _, err := p.deps.store.Artifacts().Upsert(ctx, in)
	if err != nil {
		p.input.err = err.Error()
		return pickerNotice{}, nil
	}
	p.input = artifactInput{}
	p.reload()
	p.cursor = 0
	n, open := p.openCmd(row)
	return n, tea.Batch(open, p.scheduleInline())
}
