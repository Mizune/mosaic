package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/Mizune/mosaic/internal/tui/theme"
)

func popupProgram(t *testing.T, fake *fakeInlineViewer, rows int) *ArtifactPickerProgram {
	t.Helper()
	deps := artifactPickerDeps{theme: theme.CatppuccinMocha(), fallback: fallbackRows(rows), viewer: fake, chromeW: popupChromeW, chromeH: popupChromeH}
	p := newArtifactPickerProgram(deps, "s1", "alpha", theme.CatppuccinMocha(), 50*time.Millisecond)
	res, _ := p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return res.(*ArtifactPickerProgram)
}

// runProgram feeds cmd results back into the program like Bubble Tea
// would, stopping at a QuitMsg.
func runProgram(t *testing.T, p *ArtifactPickerProgram, cmd tea.Cmd) (*ArtifactPickerProgram, bool) {
	t.Helper()
	if cmd == nil {
		return p, false
	}
	msg := cmd()
	if msg == nil {
		return p, false
	}
	if _, quit := msg.(tea.QuitMsg); quit {
		return p, true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			var quit bool
			if p, quit = runProgram(t, p, sub); quit {
				return p, true
			}
		}
		return p, false
	}
	if _, isPoll := msg.(pickerPollMsg); isPoll {
		// Polls re-arm forever; apply one and stop following it.
		res, _ := p.Update(msg)
		return res.(*ArtifactPickerProgram), false
	}
	res, next := p.Update(msg)
	return runProgram(t, res.(*ArtifactPickerProgram), next)
}

func TestArtifactPickerProgram_InitRendersAndPolls(t *testing.T) {
	fake := &fakeInlineViewer{supported: true}
	p := popupProgram(t, fake, 2)
	init := p.Init()
	if init == nil {
		t.Fatal("Init should start the preview and the poll")
	}
	p, quit := runProgram(t, p, init)
	if quit {
		t.Fatal("init must not quit")
	}
	if len(fake.renders) != 1 || p.picker.inline.placement == nil {
		t.Fatalf("renders=%v placement=%v", fake.renders, p.picker.inline.placement != nil)
	}
	if !strings.Contains(p.View(), string(kitty.Placeholder)) {
		t.Fatal("view should contain placeholder cells")
	}
	// A poll keeps the selection and re-arms itself.
	res, cmd := p.Update(pickerPollMsg{})
	p = res.(*ArtifactPickerProgram)
	if cmd == nil || p.picker.selectedID() != "a0" {
		t.Fatalf("poll should re-arm and keep selection, cmd=%v id=%q", cmd != nil, p.picker.selectedID())
	}
}

func TestArtifactPickerProgram_TransmitPrefixesFrame(t *testing.T) {
	fake := &fakeInlineViewer{supported: true}
	p := popupProgram(t, fake, 1)
	res, cmd := p.Update(artifactInlineDebounceMsg{artifactID: "a0"})
	p = res.(*ArtifactPickerProgram)
	if p.picker.inline.want == "" {
		// Init was not run; arm the want manually to mirror the host flow.
		p.picker.inline.want = "a0"
		res, cmd = p.Update(artifactInlineDebounceMsg{artifactID: "a0"})
		p = res.(*ArtifactPickerProgram)
	}
	if cmd == nil {
		t.Fatal("debounce should start a render")
	}
	res, flush := p.Update(cmd())
	p = res.(*ArtifactPickerProgram)
	if p.termSeq == "" || !strings.HasPrefix(p.View(), p.termSeq) {
		t.Fatalf("transmit must be queued and emitted first, termSeq=%q", p.termSeq[:min(len(p.termSeq), 20)])
	}
	res, _ = p.Update(flush())
	p = res.(*ArtifactPickerProgram)
	if p.termSeq != "" {
		t.Fatal("flush should clear the queue")
	}
}

func TestArtifactPickerProgram_QuitAfterDeleteFlush(t *testing.T) {
	fake := &fakeInlineViewer{supported: true}
	p := popupProgram(t, fake, 1)
	p, _ = runProgram(t, p, p.Init())
	if p.picker.inline.placement == nil {
		t.Fatal("precondition: an image is shown")
	}
	res, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	p = res.(*ArtifactPickerProgram)
	if !p.closing || cmd == nil || !strings.Contains(p.termSeq, "a=d,d=I,i=1,") {
		t.Fatalf("q must queue the delete before quitting: closing=%v termSeq=%q", p.closing, p.termSeq)
	}
	res, next := p.Update(cmd())
	p = res.(*ArtifactPickerProgram)
	if next == nil {
		t.Fatal("flush while closing must quit")
	}
	if _, quit := next().(tea.QuitMsg); !quit {
		t.Fatal("expected QuitMsg after the delete was flushed")
	}
}

func TestArtifactPickerProgram_QuitImmediatelyWithoutImage(t *testing.T) {
	p := popupProgram(t, &fakeInlineViewer{}, 1)
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected quit")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("esc with nothing to release quits at once")
	}
	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("ctrl+c quits")
	}
}

func TestArtifactPickerProgram_StatusLine(t *testing.T) {
	fake := &fakeInlineViewer{}
	fake.fakeArtifactViewer.err = errors.New("cmux: boom") // Open fails; inline stays off
	p := popupProgram(t, fake, 1)
	res, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p = res.(*ArtifactPickerProgram)
	if cmd == nil {
		t.Fatal("enter should open")
	}
	res, _ = p.Update(cmd())
	p = res.(*ArtifactPickerProgram)
	if !p.statusErr || !strings.Contains(p.status, "open artifact failed") || !strings.Contains(p.View(), "✗ open artifact failed") {
		t.Fatalf("status = %q err=%v", p.status, p.statusErr)
	}
}
