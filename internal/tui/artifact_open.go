package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Mizune/mosaic/internal/artifact"
	"github.com/Mizune/mosaic/internal/store"
)

// artifactViewer is the seam between the TUI and the viewer backend
// (internal/viewer): something that can show one artifact outside the
// terminal UI. The TUI never shells out itself; an adapter around the
// viewer package satisfies this in production and tests inject fakes.
type artifactViewer interface {
	// Name identifies the backend ("cmux", "system") for the toast.
	Name() string
	// Open shows the artifact. It must return once ctx is done.
	Open(ctx context.Context, a store.Artifact) error
}

// SetArtifactViewer installs the backend used by the artifact picker.
// nil disables opening; the picker still lists what was detected.
func (m *Model) SetArtifactViewer(v artifactViewer) { m.artifactViewer = v }

// artifactOpenedMsg reports the outcome of one open request.
type artifactOpenedMsg struct {
	title   string
	backend string
	err     string
}

// openArtifactTimeout bounds one open request. cmux answers within
// milliseconds; a hung CLI must not wedge the command goroutine.
const openArtifactTimeout = 5 * time.Second

// openArtifactCmd returns a tea.Cmd that opens a in the configured
// viewer. The exec happens inside the command goroutine, never in
// Update, so the TUI stays responsive (same shape as forkSelected).
func (m *Model) openArtifactCmd(a store.Artifact) tea.Cmd {
	v := m.artifactViewer
	if v == nil {
		m.setTopToast("viewer disabled ([viewer] enabled = false)")
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), openArtifactTimeout)
		defer cancel()
		if err := v.Open(ctx, a); err != nil {
			return artifactOpenedMsg{title: a.Title, backend: v.Name(), err: err.Error()}
		}
		return artifactOpenedMsg{title: a.Title, backend: v.Name()}
	}
}

// applyArtifactOpened surfaces an open result as a toast, or as an
// error-log entry when the backend failed.
func (m *Model) applyArtifactOpened(v artifactOpenedMsg) {
	if v.err != "" {
		m.flagError("open artifact failed", v.err)
		return
	}
	m.setTopToast("opened " + v.title + " (" + v.backend + ")")
}

// submitManualArtifact validates the picker's text input, persists it
// as a manual artifact so it reappears in the list, and opens it.
// Validation errors stay inside the input box; the picker remains open.
func (m *Model) submitManualArtifact() tea.Cmd {
	p := &m.artifactPanel
	raw := strings.TrimSpace(p.input.value)
	if raw == "" {
		p.input.err = "path or URL is required"
		return nil
	}
	in, err := manualArtifactInput(raw, p.sessionID)
	if err != nil {
		p.input.err = err.Error()
		return nil
	}
	if m.store == nil {
		p.input.err = "store unavailable"
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	row, _, err := m.store.Artifacts().Upsert(ctx, in)
	if err != nil {
		p.input.err = err.Error()
		return nil
	}
	p.input = artifactInput{}
	m.reloadArtifactPanel()
	p.cursor = 0
	return tea.Batch(m.openArtifactCmd(row), m.scheduleInlinePreview())
}

// maxManualMermaidBytes caps a .mmd file read from the picker.
const maxManualMermaidBytes = 256 << 10

// manualArtifactInput turns what the user typed into a NewArtifact.
// http(s):// and file:// become url artifacts; anything else must be
// an existing file the viewer can show. .mmd / .mermaid files are read
// and stored as Mermaid source so they render like detected diagrams.
func manualArtifactInput(raw, sessionID string) (store.NewArtifact, error) {
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "file://") {
		return store.NewArtifact{
			SessionID: sessionID, Kind: string(artifact.KindURL), Source: string(artifact.SourceManual),
			Title: raw, Path: raw, ContentHash: artifact.ContentHash(raw),
		}, nil
	}
	path, _ := expandTilde(raw)
	abs, err := filepath.Abs(path)
	if err != nil {
		return store.NewArtifact{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return store.NewArtifact{}, err
	}
	if info.IsDir() {
		return store.NewArtifact{}, fmt.Errorf("%s is a directory", abs)
	}
	base := filepath.Base(abs)
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".mmd", ".mermaid":
		if info.Size() > maxManualMermaidBytes {
			return store.NewArtifact{}, errors.New("mermaid file is larger than 256 KiB")
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return store.NewArtifact{}, err
		}
		src := strings.TrimSpace(string(data))
		if src == "" {
			return store.NewArtifact{}, errors.New("mermaid file is empty")
		}
		return store.NewArtifact{
			SessionID: sessionID, Kind: string(artifact.KindMermaid), Source: string(artifact.SourceManual),
			Title: base, Content: src, Path: abs, ContentHash: artifact.ContentHash(src),
		}, nil
	}
	kind, ok := artifact.KindForPath(abs)
	if !ok {
		return store.NewArtifact{}, fmt.Errorf("unsupported file type %q (png, jpg, gif, webp, svg, html, mmd)", filepath.Ext(abs))
	}
	return store.NewArtifact{
		SessionID: sessionID, Kind: string(kind), Source: string(artifact.SourceManual),
		Title: base, Path: abs, ContentHash: artifact.ContentHash(abs),
	}, nil
}

// autoOpenNewArtifacts implements [viewer] auto_open: when a refresh
// brings artifact rows that the previous snapshot did not have, the
// newest one is opened without a key press. The first refresh after
// start is skipped because it carries the whole replayed transcript
// backlog, and manual rows are skipped because submitting them already
// opened them.
func (m *Model) autoOpenNewArtifacts(prev, next map[string][]store.Artifact, firstRefresh bool) tea.Cmd {
	if !m.config.Viewer.AutoOpen || m.artifactViewer == nil || firstRefresh || next == nil {
		return nil
	}
	seen := make(map[string]bool, 64)
	for _, rows := range prev {
		for _, r := range rows {
			seen[r.ID] = true
		}
	}
	var newest *store.Artifact
	for _, rows := range next {
		for i := range rows {
			r := &rows[i]
			if seen[r.ID] || r.Source == string(artifact.SourceManual) {
				continue
			}
			if newest == nil || r.CreatedAt.After(newest.CreatedAt) {
				newest = r
			}
		}
	}
	if newest == nil {
		return nil
	}
	return m.openArtifactCmd(*newest)
}
