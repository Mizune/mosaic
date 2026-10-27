package tui

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tui/theme"
	"github.com/Mizune/mosaic/internal/viewer"
)

// ArtifactPickerProgram is a standalone Bubble Tea model that runs only
// the artifact picker. It backs `mosaic artifacts`, which the tmux
// prefix+O binding opens in a popup (or split) inside an attached
// session, so the user can look at diagrams and screenshots without
// detaching to the TUI. Like [RecipeWizardProgram] it does not touch
// the bus or the event loop; it polls the store instead.
type ArtifactPickerProgram struct {
	termSeqQueue
	picker    artifactPicker
	theme     *theme.Theme
	width     int
	height    int
	initCmd   tea.Cmd
	poll      time.Duration
	status    string
	statusErr bool
	closing   bool
}

// pickerPollInterval is how often the popup re-reads the artifact list
// so rows detected while it is open show up.
const pickerPollInterval = 2 * time.Second

// Chrome of the popup frame: padding(1,2) plus the status line.
const (
	popupChromeW = 6
	popupChromeH = 8
)

// pickerPollMsg triggers one reload.
type pickerPollMsg struct{}

// NewArtifactPickerProgram builds the popup picker for one session.
// outerTmux is the TUI's own $TMUX (from MOSAIC_OUTER_TMUX) when the
// popup sits under two tmux servers; empty falls back to the process
// environment.
func NewArtifactPickerProgram(st *store.Store, sessionID, sessionName string, t *theme.Theme, cfg config.ViewerSection, outerTmux string) *ArtifactPickerProgram {
	if t == nil {
		t = theme.CatppuccinMocha()
	}
	env := viewer.DefaultEnvironment()
	if outerTmux != "" {
		env.Getenv = func(key string) string {
			if key == "MOSAIC_OUTER_TMUX" {
				return outerTmux
			}
			return os.Getenv(key)
		}
	}
	var v artifactViewer
	if config.BoolOr(cfg.Enabled, true) {
		v = newViewerAdapterEnv(cfg, env)
	}
	deps := artifactPickerDeps{theme: t, store: st, viewer: v, chromeW: popupChromeW, chromeH: popupChromeH}
	return newArtifactPickerProgram(deps, sessionID, sessionName, t, pickerPollInterval)
}

// newArtifactPickerProgram is the dependency-injected constructor the
// tests use.
func newArtifactPickerProgram(deps artifactPickerDeps, sessionID, sessionName string, t *theme.Theme, poll time.Duration) *ArtifactPickerProgram {
	p := &ArtifactPickerProgram{theme: t, poll: poll}
	deps.queueSeq = p.queue
	p.picker, p.initCmd = newArtifactPicker(deps, sessionID, sessionName)
	return p
}

// Init satisfies tea.Model: start the first preview and the poll.
func (p *ArtifactPickerProgram) Init() tea.Cmd {
	return tea.Batch(p.initCmd, p.pollCmd())
}

func (p *ArtifactPickerProgram) pollCmd() tea.Cmd {
	if p.poll <= 0 {
		return nil
	}
	return tea.Tick(p.poll, func(time.Time) tea.Msg { return pickerPollMsg{} })
}

// Update satisfies tea.Model.
func (p *ArtifactPickerProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = v.Width, v.Height
		p.picker.setSize(v.Width, v.Height)
		return p, nil

	case tea.KeyMsg:
		if v.String() == "ctrl+c" {
			return p, tea.Quit
		}
		if p.closing {
			return p, nil
		}
		action, notice, cmd := p.picker.updateKey(v)
		p.applyNotice(notice)
		if action == pickerActionClose {
			if cmd == nil {
				return p, tea.Quit
			}
			// Let the queued Kitty delete reach the terminal (one
			// frame) before the popup disappears.
			p.closing = true
			return p, cmd
		}
		return p, cmd

	case termSeqFlushedMsg:
		p.applyFlushed(v)
		if p.closing {
			return p, tea.Quit
		}
		return p, nil

	case pickerPollMsg:
		if p.closing {
			return p, nil
		}
		return p, tea.Batch(p.picker.reloadKeepingSelection(), p.pollCmd())
	}

	if handled, notice, cmd := p.picker.updateMsg(msg); handled {
		p.applyNotice(notice)
		return p, cmd
	}
	return p, nil
}

func (p *ArtifactPickerProgram) applyNotice(n pickerNotice) {
	switch n.level {
	case noticeInfo:
		p.status, p.statusErr = n.title, false
	case noticeError:
		p.status, p.statusErr = n.title+": "+n.body, true
	}
}

// View satisfies tea.Model. The popup draws its own border, so the
// body only gets padding and a status line.
func (p *ArtifactPickerProgram) View() string {
	if p.width == 0 {
		return "loading..."
	}
	body := lipgloss.NewStyle().Padding(1, 2).Render(p.picker.view())
	status := ""
	if p.status != "" {
		if p.statusErr {
			status = p.theme.AttentionTag.Render("✗ " + p.status)
		} else {
			status = p.theme.MutedText.Render("• " + p.status)
		}
	}
	return p.prefix(body + "\n  " + status)
}
