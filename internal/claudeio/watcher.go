package claudeio

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/pkg/transcript"
)

// Event types published on the bus by [Watcher]. They live in this
// package (instead of internal/bus) so the watcher can ship richer
// payloads without polluting the bus package with claudeio-specific
// shapes.
type (
	// TranscriptEntry is fired for every parsed JSONL line.
	TranscriptEntry struct {
		ProjectPath string
		SessionID   string
		AgentID     string // empty for parent transcripts
		Entry       transcript.Entry
	}

	// SubagentSpawned is fired when a new agent-*.jsonl file appears.
	SubagentSpawned struct {
		ProjectPath string
		SessionID   string
		AgentID     string
	}
)


// Watcher tails one Claude session and any sub-agents it spawns. The
// caller registers project + session via [Watcher.Watch], and the
// watcher publishes events on the supplied bus until [Watcher.Stop].
type Watcher struct {
	bus  *bus.Bus
	home string
	log  *slog.Logger

	mu      sync.Mutex
	cancel  context.CancelFunc
	watches map[string]*sessionWatch
	fsw     *fsnotify.Watcher
}

type sessionWatch struct {
	projectPath string
	sessionID   string
	parentTail  *Tail
	subTails    map[string]*Tail // agentID → tail
}

// NewWatcher constructs a Watcher bound to home (defaults to the user
// home dir when empty) and bus.
func NewWatcher(b *bus.Bus, home string) *Watcher {
	return &Watcher{
		bus:     b,
		home:    home,
		log:     slog.With("subsys", "claudeio.watcher"),
		watches: map[string]*sessionWatch{},
	}
}

// Start initialises the underlying fsnotify watcher. It must be
// called before [Watcher.Watch]. Stop releases the resources.
func (w *Watcher) Start(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.fsw = fsw
	w.mu.Unlock()

	cctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	// Capture fsw in the goroutine's local scope so the loop never
	// touches w.fsw directly. Stop can then nil it out without
	// racing.
	go w.loop(cctx, fsw)
	return nil
}

// Stop tears down the watcher.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.fsw != nil {
		_ = w.fsw.Close()
		w.fsw = nil
	}
}

// Watch registers a Claude session and emits an initial backlog of
// already-existing transcript lines.
func (w *Watcher) Watch(projectPath, sessionID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	key := projectPath + "::" + sessionID
	if _, exists := w.watches[key]; exists {
		return nil
	}

	parentPath := SessionTranscriptPath(w.home, projectPath, sessionID)
	sw := &sessionWatch{
		projectPath: projectPath,
		sessionID:   sessionID,
		parentTail:  NewTail(parentPath),
		subTails:    map[string]*Tail{},
	}
	w.watches[key] = sw

	dir := ProjectDir(w.home, projectPath)
	if err := w.fsw.Add(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		w.log.Warn("fsnotify add project dir", "dir", dir, "err", err)
	}
	subDir := SubagentDir(w.home, projectPath, sessionID)
	if err := os.MkdirAll(subDir, 0o755); err == nil {
		_ = w.fsw.Add(subDir)
	}

	// Drain whatever already exists so the TUI starts populated.
	w.drainSession(sw)
	return nil
}

// Snapshot returns the registered (projectPath, sessionID) pairs for
// debugging.
func (w *Watcher) Snapshot() []TranscriptEntry {
	return nil
}

func (w *Watcher) loop(ctx context.Context, fsw *fsnotify.Watcher) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-fsw.Events:
			if !ok {
				return
			}
			w.handleFSEvent(ev)
		case err, ok := <-fsw.Errors:
			if !ok {
				return
			}
			w.log.Warn("fsnotify error", "err", err)
		}
	}
}

func (w *Watcher) handleFSEvent(ev fsnotify.Event) {
	if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
		return
	}
	if !strings.HasSuffix(ev.Name, ".jsonl") {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, sw := range w.watches {
		dir := ProjectDir(w.home, sw.projectPath)
		subDir := SubagentDir(w.home, sw.projectPath, sw.sessionID)
		switch {
		case filepath.Dir(ev.Name) == dir:
			parentPath := SessionTranscriptPath(w.home, sw.projectPath, sw.sessionID)
			if ev.Name == parentPath {
				w.drainTail(sw, sw.parentTail, "")
			}
		case filepath.Dir(ev.Name) == subDir:
			base := filepath.Base(ev.Name)
			if !strings.HasPrefix(base, "agent-") {
				continue
			}
			agentID := strings.TrimSuffix(strings.TrimPrefix(base, "agent-"), ".jsonl")
			tail, ok := sw.subTails[agentID]
			if !ok {
				tail = NewTail(ev.Name)
				sw.subTails[agentID] = tail
				w.bus.Publish(SubagentSpawned{
					ProjectPath: sw.projectPath,
					SessionID:   sw.sessionID,
					AgentID:     agentID,
				})
			}
			w.drainTail(sw, tail, agentID)
		}
	}
}

// drainSession reads any backlog already present in the parent and
// sub-agent transcripts.
func (w *Watcher) drainSession(sw *sessionWatch) {
	w.drainTail(sw, sw.parentTail, "")

	subDir := SubagentDir(w.home, sw.projectPath, sw.sessionID)
	entries, err := os.ReadDir(subDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		agentID := strings.TrimSuffix(strings.TrimPrefix(name, "agent-"), ".jsonl")
		if _, ok := sw.subTails[agentID]; ok {
			continue
		}
		tail := NewTail(filepath.Join(subDir, name))
		sw.subTails[agentID] = tail
		w.bus.Publish(SubagentSpawned{
			ProjectPath: sw.projectPath,
			SessionID:   sw.sessionID,
			AgentID:     agentID,
		})
		w.drainTail(sw, tail, agentID)
	}
}

func (w *Watcher) drainTail(sw *sessionWatch, t *Tail, agentID string) {
	entries, err := t.Read()
	if err != nil {
		w.log.Warn("read tail", "path", t.Path(), "err", err)
		return
	}
	for _, e := range entries {
		w.bus.Publish(TranscriptEntry{
			ProjectPath: sw.projectPath,
			SessionID:   sw.sessionID,
			AgentID:     agentID,
			Entry:       e,
		})
	}
}
