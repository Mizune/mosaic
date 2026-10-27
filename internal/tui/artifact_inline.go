package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/viewer"
)

// inlinePreviewer is implemented by viewers that can draw an artifact
// into the terminal itself (Kitty graphics). The picker discovers it
// with a type assertion, so a browser-only artifactViewer keeps working.
type inlinePreviewer interface {
	InlineSupport() (supported bool, reason string)
	RenderInline(ctx context.Context, a store.Artifact, id, cols, rows int) (viewer.KittyPlacement, error)
}

// artifactInlineState tracks the preview drawn in the picker's right
// column. Only one image is alive at a time; switching artifacts frees
// the previous one through the host's terminal-sequence queue.
type artifactInlineState struct {
	supported  bool
	reason     string
	want       string // artifact id the cursor rests on (debounced)
	artifactID string // artifact rendered or being rendered
	placement  *viewer.KittyPlacement
	loading    bool
	err        string
	lastID     int // last kitty image id handed out
}

// inlineDebounce delays rendering while the cursor is still moving so
// a quick j/j/j does not start three browser rasterizations.
const inlineDebounce = 150 * time.Millisecond

// inlineRenderTimeout bounds one preview (page write + rasterize).
const inlineRenderTimeout = 40 * time.Second

type (
	// artifactInlineDebounceMsg fires after the cursor has rested.
	artifactInlineDebounceMsg struct{ artifactID string }
	// artifactInlineMsg carries a finished render.
	artifactInlineMsg struct {
		artifactID string
		placement  viewer.KittyPlacement
		err        string
	}
)

// initInline reads the viewer's capability once per picker open.
func (p *artifactPicker) initInline() {
	st := &p.inline
	*st = artifactInlineState{}
	if p.deps.viewer == nil {
		st.reason = "viewer disabled ([viewer] enabled = false)"
		return
	}
	previewer, ok := p.deps.viewer.(inlinePreviewer)
	if !ok {
		st.reason = "this viewer cannot draw inline; Enter opens the artifact in a browser"
		return
	}
	st.supported, st.reason = previewer.InlineSupport()
}

// scheduleInline is called whenever the selection may have changed. It
// records the wanted artifact and arms the debounce.
func (p *artifactPicker) scheduleInline() tea.Cmd {
	if !p.inline.supported || p.cursor < 0 || p.cursor >= len(p.items) {
		return nil
	}
	id := p.items[p.cursor].ID
	if id == p.inline.artifactID || id == p.inline.want {
		return nil
	}
	p.inline.want = id
	return debounceInline(id)
}

func debounceInline(id string) tea.Cmd {
	return tea.Tick(inlineDebounce, func(time.Time) tea.Msg { return artifactInlineDebounceMsg{artifactID: id} })
}

// applyInlineDebounce starts the render if the cursor still rests on
// the artifact the debounce was armed for. A closed (zeroed) picker
// has no wanted artifact, so late messages fall through harmlessly.
func (p *artifactPicker) applyInlineDebounce(v artifactInlineDebounceMsg) tea.Cmd {
	if v.artifactID == "" || v.artifactID != p.inline.want || p.cursor >= len(p.items) {
		return nil
	}
	sel := p.items[p.cursor]
	if sel.ID != v.artifactID || sel.ID == p.inline.artifactID {
		return nil
	}
	previewer, ok := p.deps.viewer.(inlinePreviewer)
	if !ok {
		return nil
	}
	if p.width == 0 || p.height == 0 {
		// The host has not told us the window size yet (a popup
		// right after start); wait another beat rather than
		// rendering into a guessed box.
		return debounceInline(v.artifactID)
	}
	var cmds []tea.Cmd
	if p.inline.placement != nil {
		cmds = append(cmds, p.deps.queueSeq(p.inline.placement.Delete))
	}
	cols, rows := p.previewBox()
	id := viewer.NextKittyID(p.inline.lastID)
	p.inline.lastID = id
	p.inline.artifactID = sel.ID
	p.inline.placement = nil
	p.inline.loading = true
	p.inline.err = ""
	cmds = append(cmds, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), inlineRenderTimeout)
		defer cancel()
		placement, err := previewer.RenderInline(ctx, sel, id, cols, rows)
		if err != nil {
			return artifactInlineMsg{artifactID: sel.ID, err: err.Error()}
		}
		return artifactInlineMsg{artifactID: sel.ID, placement: placement}
	})
	return tea.Batch(cmds...)
}

// applyInlineResult installs a finished render, ignoring results for
// an artifact the cursor has since left (or a picker since closed).
func (p *artifactPicker) applyInlineResult(v artifactInlineMsg) tea.Cmd {
	if v.artifactID == "" || v.artifactID != p.inline.artifactID {
		return nil
	}
	p.inline.loading = false
	if v.err != "" {
		p.inline.err = v.err
		return nil
	}
	placement := v.placement
	p.inline.placement = &placement
	return p.deps.queueSeq(placement.Transmit)
}

// releaseInline frees the image when the picker closes.
func (p *artifactPicker) releaseInline() tea.Cmd {
	if p.inline.placement == nil {
		return nil
	}
	seq := p.inline.placement.Delete
	p.inline.placement = nil
	p.inline.artifactID = ""
	return p.deps.queueSeq(seq)
}

// columns splits the body width between the list (left) and the
// preview (right).
func (p *artifactPicker) columns() (inner, left, right int) {
	inner = p.width - p.deps.chromeW
	if inner < 60 {
		inner = 60
	}
	left = inner * 2 / 5
	if left < 30 {
		left = 30
	}
	right = inner - left - 2
	if right < 20 {
		right = 20
	}
	return inner, left, right
}

// previewBox is the cell box the image is fitted into.
func (p *artifactPicker) previewBox() (cols, rows int) {
	_, _, right := p.columns()
	rows = p.height - p.deps.chromeH
	if rows < 5 {
		rows = 5
	}
	return right, rows
}

// renderInlinePane draws the right column: the placeholder grid when an
// image is ready, otherwise the status or the reason inline preview is
// unavailable.
func (p *artifactPicker) renderInlinePane(width int) string {
	st := p.inline
	muted := func(s string) string { return p.deps.theme.MutedText.Render(s) }
	switch {
	case !st.supported:
		return muted(wrapWords(st.reason, width))
	case st.err != "":
		return p.deps.theme.AttentionTag.Render("✗ ") + muted(wrapWords(st.err, width-2))
	case st.loading:
		return muted("rendering preview…")
	case st.placement != nil:
		return strings.Join(st.placement.Lines, "\n")
	default:
		return muted("select an artifact to preview it here")
	}
}

// wrapWords folds s at spaces so a long reason fits the column.
func wrapWords(s string, width int) string {
	if width < 10 {
		width = 10
	}
	var (
		lines []string
		cur   strings.Builder
	)
	for _, word := range strings.Fields(s) {
		if cur.Len() > 0 && cur.Len()+1+len(word) > width {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n")
}
