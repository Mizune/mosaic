package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/viewer"
)

// fakeInlineViewer is a browser viewer that can also draw inline.
type fakeInlineViewer struct {
	fakeArtifactViewer
	supported bool
	reason    string
	renders   []string // artifact ids rendered, in order
	ids       []int
	err       error
}

func (f *fakeInlineViewer) InlineSupport() (bool, string) { return f.supported, f.reason }

func (f *fakeInlineViewer) RenderInline(_ context.Context, a store.Artifact, id, cols, rows int) (viewer.KittyPlacement, error) {
	f.renders = append(f.renders, a.ID)
	f.ids = append(f.ids, id)
	if f.err != nil {
		return viewer.KittyPlacement{}, f.err
	}
	return viewer.EncodeKitty(viewer.InlineImage{PNG: []byte("png-bytes"), Width: 4, Height: 2}, id, cols, rows, 0)
}

// drive runs cmd synchronously and feeds every resulting message back
// into the model, following tea.Batch and tea.Tick as Bubble Tea would.
func drive(t *testing.T, m *Model, cmd tea.Cmd) *Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			m = drive(t, m, sub)
		}
		return m
	}
	res, next := m.Update(msg)
	m = res.(*Model)
	return drive(t, m, next)
}

func pickerWithInline(t *testing.T, fake *fakeInlineViewer, hashes ...string) *Model {
	t.Helper()
	m := freshModel(t)
	for _, h := range hashes {
		seedArtifact(t, m, "alpha", h)
	}
	m = refreshModel(t, m)
	focusSession(t, m, "alpha")
	m.SetArtifactViewer(fake)
	return m
}

func TestInlinePreview_UnsupportedShowsReason(t *testing.T) {
	fake := &fakeInlineViewer{reason: "inside tmux: run `tmux set -g allow-passthrough on`"}
	m := pickerWithInline(t, fake, "h1")
	m, cmd := press(t, m, "o")
	if cmd != nil {
		t.Fatal("unsupported inline preview must not schedule a render")
	}
	if !strings.Contains(m.View(), "allow-passthrough on") {
		t.Fatalf("reason should be shown in the right column, got:\n%s", m.View())
	}
	if len(fake.renders) != 0 {
		t.Fatal("nothing should render")
	}
}

func TestInlinePreview_RendersSelectedArtifactOnce(t *testing.T) {
	fake := &fakeInlineViewer{supported: true}
	m := pickerWithInline(t, fake, "h1")
	m, cmd := press(t, m, "o")
	if cmd == nil {
		t.Fatal("opening the picker should arm the debounce")
	}
	m = drive(t, m, cmd) // debounce → render → transmit queued → flush tick
	if len(fake.renders) != 1 || fake.ids[0] != 1 {
		t.Fatalf("renders=%v ids=%v", fake.renders, fake.ids)
	}
	st := m.artifactPanel.inline
	if st.placement == nil || st.loading || st.err != "" {
		t.Fatalf("placement should be installed: %+v", st)
	}
	out := m.View()
	if !strings.Contains(out, string(kitty.Placeholder)) {
		t.Fatal("view should contain placeholder cells")
	}
	if m.termSeq != "" {
		t.Fatalf("transmit sequence should have been flushed after its tick, still queued: %q", m.termSeq[:20])
	}
	// Staying on the same artifact never re-renders.
	m, cmd = press(t, m, "j")
	if cmd != nil {
		t.Fatal("no movement: no new render")
	}
}

func TestInlinePreview_TransmitIsInFrameUntilFlushed(t *testing.T) {
	fake := &fakeInlineViewer{supported: true}
	m := pickerWithInline(t, fake, "h1")
	m, cmd := press(t, m, "o")
	debounce := cmd()
	res, renderCmd := m.Update(debounce)
	m = res.(*Model)
	// With nothing to delete, tea.Batch collapses to the render command
	// itself; after a switch it is a real batch. Handle both.
	var flushCmd tea.Cmd
	apply := func(msg tea.Msg) {
		res, flushCmd = m.Update(msg)
		m = res.(*Model)
	}
	if batch, ok := renderCmd().(tea.BatchMsg); ok {
		for _, sub := range batch {
			if msg := sub(); msg != nil {
				apply(msg)
			}
		}
	} else {
		apply(renderCmd())
	}
	if !strings.Contains(m.termSeq, "a=T,U=1,q=2,f=100,i=1,") {
		t.Fatalf("transmit should be queued, got %q", m.termSeq)
	}
	if !strings.HasPrefix(m.View(), m.termSeq) {
		t.Fatal("queued sequence must be emitted at the very start of the frame")
	}
	res, _ = m.Update(flushCmd())
	m = res.(*Model)
	if m.termSeq != "" || strings.Contains(m.View(), "a=T,U=1") {
		t.Fatal("flushed sequence must leave the frame")
	}
	if !strings.Contains(m.View(), string(kitty.Placeholder)) {
		t.Fatal("placeholders stay after the flush")
	}
}

func TestInlinePreview_SwitchingArtifactsDeletesPrevious(t *testing.T) {
	fake := &fakeInlineViewer{supported: true}
	m := pickerWithInline(t, fake, "h1", "h2")
	m, cmd := press(t, m, "o")
	m = drive(t, m, cmd)
	m, cmd = press(t, m, "j")
	if cmd == nil {
		t.Fatal("moving to another artifact should arm the debounce")
	}
	debounce := cmd()
	res, renderCmd := m.Update(debounce)
	m = res.(*Model)
	batch := renderCmd().(tea.BatchMsg)
	// The delete for image id 1 is queued synchronously by the debounce.
	if !strings.Contains(m.termSeq, "a=d,d=I,i=1,") {
		t.Fatalf("previous image should be deleted when switching, got %q", m.termSeq)
	}
	for _, sub := range batch {
		if msg := sub(); msg != nil {
			res, _ = m.Update(msg)
			m = res.(*Model)
		}
	}
	if len(fake.renders) != 2 || fake.ids[1] != 2 {
		t.Fatalf("renders=%v ids=%v", fake.renders, fake.ids)
	}
	if m.artifactPanel.inline.placement == nil || m.artifactPanel.inline.placement.ID != 2 {
		t.Fatalf("placement should now be image 2: %+v", m.artifactPanel.inline)
	}
}

func TestInlinePreview_StaleResultIgnored(t *testing.T) {
	fake := &fakeInlineViewer{supported: true}
	m := pickerWithInline(t, fake, "h1", "h2")
	m, _ = press(t, m, "o")
	res, _ := m.Update(artifactInlineMsg{artifactID: "not-the-current-one", placement: viewer.KittyPlacement{ID: 9}})
	m = res.(*Model)
	if m.artifactPanel.inline.placement != nil || m.termSeq != "" {
		t.Fatal("a result for another artifact must be ignored")
	}
}

func TestInlinePreview_ErrorShown(t *testing.T) {
	fake := &fakeInlineViewer{supported: true, err: errors.New("viewer: no Chrome or Chromium found")}
	m := pickerWithInline(t, fake, "h1")
	m, cmd := press(t, m, "o")
	m = drive(t, m, cmd)
	st := m.artifactPanel.inline
	if st.loading || st.err == "" || st.placement != nil {
		t.Fatalf("error should be recorded: %+v", st)
	}
	if !strings.Contains(m.View(), "no Chrome") {
		t.Fatalf("error should be visible, got:\n%s", m.View())
	}
	if m.screen != ScreenArtifacts {
		t.Fatal("picker stays open on a preview error")
	}
}

func TestInlinePreview_CloseReleasesImage(t *testing.T) {
	fake := &fakeInlineViewer{supported: true}
	m := pickerWithInline(t, fake, "h1")
	m, cmd := press(t, m, "o")
	m = drive(t, m, cmd)
	m, cmd = press(t, m, "esc")
	if m.screen != ScreenTop {
		t.Fatalf("screen=%d", m.screen)
	}
	if !strings.Contains(m.termSeq, "a=d,d=I,i=1,") || !strings.HasPrefix(m.View(), m.termSeq) {
		t.Fatalf("closing should queue the delete and emit it on the TOP frame, got %q", m.termSeq)
	}
	res, _ := m.Update(cmd())
	m = res.(*Model)
	if m.termSeq != "" {
		t.Fatal("delete should be flushed")
	}
}

func TestWrapWords(t *testing.T) {
	got := wrapWords("inside tmux: run tmux set -g allow-passthrough on", 20)
	for _, line := range strings.Split(got, "\n") {
		if len(line) > 20 {
			t.Fatalf("line too long: %q", line)
		}
	}
	if strings.Count(got, "\n") < 1 {
		t.Fatalf("expected wrapping, got %q", got)
	}
}
