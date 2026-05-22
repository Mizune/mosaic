package live

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/internal/codexio"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/pkg/transcript"
)

// Binder is responsible for discovering, for each mosaic session, the
// Claude session id Claude Code is using inside that worktree. It
// then asks the [claudeio.Watcher] to start tailing the matching
// transcript so the Reconciler can persist live state.
//
// Discovery happens once at startup and again on every poll
// interval, so newly created mosaic sessions get bound shortly after
// the user attaches and Claude writes its first JSONL line.
type Binder struct {
	store    *store.Store
	watcher  *claudeio.Watcher
	home     string
	interval time.Duration
	log      *slog.Logger

	mu     sync.Mutex
	bound  map[string]string // mosaicSessionID → claude session id
	cancel context.CancelFunc
	done   chan struct{}
}

// NewBinder constructs a Binder. home is the value passed to mosaic's
// path resolver (so tests can isolate ~/.claude); pass "" for the
// real $HOME.
func NewBinder(s *store.Store, w *claudeio.Watcher, home string) *Binder {
	return &Binder{
		store:    s,
		watcher:  w,
		home:     home,
		interval: 2 * time.Second,
		log:      slog.With("subsys", "live.binder"),
		bound:    map[string]string{},
		done:     make(chan struct{}),
	}
}

// Start runs an initial bind sweep and then re-binds every interval
// until [Binder.Stop] is called.
func (b *Binder) Start(ctx context.Context) {
	cctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel

	// Initial sweep so the TUI sees state on the very first frame.
	_ = b.sweep(cctx)

	go func() {
		defer close(b.done)
		t := time.NewTicker(b.interval)
		defer t.Stop()
		for {
			select {
			case <-cctx.Done():
				return
			case <-t.C:
				_ = b.sweep(cctx)
			}
		}
	}()
}

// Stop halts the background sweep and waits for it to finish.
func (b *Binder) Stop() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
		<-b.done
	}
}

// sweep walks every session that does not yet have a Claude session
// id and tries to bind it. Errors are logged but never bubble up
// because the binder is best-effort.
func (b *Binder) sweep(ctx context.Context) error {
	rows, err := b.store.Sessions().List(ctx)
	if err != nil {
		return err
	}
	for _, s := range rows {
		if store.SessionTool(s.Tool) == store.ToolCodex {
			b.sweepCodex(ctx, s)
			continue
		}
		// Always ask the filesystem what the latest Claude session
		// id for this worktree is. Sticking with a stale binding —
		// which is what happens when the user `/exit`s Claude and
		// then re-attaches, spawning a fresh Claude session id —
		// means the watcher keeps tailing the old transcripts and
		// new sub-agents are invisible to the TUI.
		//
		// We only consider Claude sessions that started AFTER the
		// mosaic session row was created (with a small margin for
		// clock skew). Without this filter a fresh mosaic session
		// would inherit sub-agents from a historical Claude run
		// in the same directory — exactly the "new session shows
		// 8-minute-old sub-agent" bug.
		earliest := s.CreatedAt.Add(-30 * time.Second)
		latest, ok := b.discover(s.WorktreePath, earliest)
		if !ok {
			// No Claude transcript yet. If we already have a
			// binding, keep watching it; otherwise there is
			// nothing to do.
			if s.ClaudeSessionID.Valid && s.ClaudeSessionID.String != "" {
				b.startWatchOnce(s.WorktreePath, s.ClaudeSessionID.String)
			}
			continue
		}

		// If the discovered session matches what the DB already
		// has, just make sure the watcher is subscribed.
		if s.ClaudeSessionID.Valid && s.ClaudeSessionID.String == latest {
			b.markBound(s.ID, latest)
			b.startWatchOnce(s.WorktreePath, latest)
			continue
		}

		// Discovered session is different from the current binding
		// (or no binding yet). Rebind and start watching the new
		// transcript pair.
		if err := b.bind(ctx, s.ID, latest); err != nil {
			b.log.Warn("bind", "session", s.Name, "err", err)
			continue
		}
		b.startWatchOnce(s.WorktreePath, latest)
	}
	return nil
}

func (b *Binder) sweepCodex(ctx context.Context, s store.Session) {
	earliest := s.CreatedAt.Add(-30 * time.Second)
	meta, ok := codexio.FindLatestSession(b.home, s.WorktreePath, earliest)
	if !ok {
		return
	}
	if s.AgentSessionID.Valid && s.AgentSessionID.String == meta.ID {
		b.markBound(s.ID, meta.ID)
		return
	}
	if err := b.store.Sessions().BindAgentSession(ctx, s.ID, store.ToolCodex, meta.ID, meta.Path, meta.CLIVersion); err != nil {
		b.log.Warn("bind codex", "session", s.Name, "err", err)
		return
	}
	b.markBound(s.ID, meta.ID)
}

// discover walks ~/.claude/projects/<sanitized(worktreePath)>/ and
// returns the Claude session id that most plausibly corresponds to
// the mosaic session's current run. Candidates are filtered by the
// first-entry timestamp so historical jsonl files in the same
// project directory do not hijack a fresh mosaic session.
//
// earliest is the floor: only jsonl files whose first timestamped
// entry is at or after earliest are considered. Pass the zero value
// to disable the filter.
func (b *Binder) discover(worktreePath string, earliest time.Time) (string, bool) {
	dir := claudeio.ProjectDir(b.home, worktreePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	type candidate struct {
		name  string
		first time.Time
		cwd   string
	}
	var found []candidate
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		path := filepath.Join(dir, name)
		first, cwd, ok := readFirstEntry(path)
		if !ok {
			continue
		}
		// Reject transcripts that started before the mosaic
		// session row was created — they can only belong to a
		// previous Claude run in the same directory.
		if !earliest.IsZero() && first.Before(earliest) {
			continue
		}
		// Verify the cwd is in the same tree as the worktree.
		// HasPrefix both ways tolerates trailing/leading
		// separators and macOS /private prefixes.
		if cwd != "" && !strings.HasPrefix(cwd, worktreePath) && !strings.HasPrefix(worktreePath, cwd) {
			continue
		}
		found = append(found, candidate{name: name, first: first, cwd: cwd})
	}
	if len(found) == 0 {
		return "", false
	}
	// Pick the candidate with the LATEST first-entry timestamp:
	// among post-creation sessions, the newest one is the one the
	// user is actively interacting with.
	best := found[0]
	for _, c := range found[1:] {
		if c.first.After(best.first) {
			best = c
		}
	}
	return strings.TrimSuffix(best.name, ".jsonl"), true
}

// readFirstEntry returns the first timestamped entry's timestamp
// and cwd from a transcript file. It skips over the leading
// file-history-snapshot rows Claude Code emits before the real
// session events, so the returned timestamp corresponds to the
// actual start of the Claude session.
func readFirstEntry(path string) (time.Time, string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, "", false
	}
	defer f.Close()
	dec := transcript.NewDecoder(f)
	for {
		e, err := dec.Next()
		if err != nil {
			return time.Time{}, "", false
		}
		if !e.Timestamp.IsZero() {
			return e.Timestamp, e.CWD, true
		}
	}
}

// bind persists the discovered Claude session id on the mosaic
// session row.
func (b *Binder) bind(ctx context.Context, mosaicSessionID, claudeSessionID string) error {
	row, err := b.store.Sessions().GetByID(ctx, mosaicSessionID)
	if err != nil {
		return err
	}
	if row.ClaudeSessionID.Valid && row.ClaudeSessionID.String == claudeSessionID {
		b.markBound(mosaicSessionID, claudeSessionID)
		return nil
	}
	if err := b.store.Sessions().BindClaudeSession(ctx, mosaicSessionID, claudeSessionID); err != nil {
		return err
	}
	b.markBound(mosaicSessionID, claudeSessionID)
	return nil
}

func (b *Binder) markBound(sessionID, claudeSessionID string) {
	b.mu.Lock()
	b.bound[sessionID] = claudeSessionID
	b.mu.Unlock()
}

// startWatchOnce is idempotent: it asks the watcher to track this
// (project, session) pair only the first time we see it.
func (b *Binder) startWatchOnce(worktreePath, claudeSessionID string) {
	if b.watcher == nil {
		return
	}
	_ = b.watcher.Watch(worktreePath, claudeSessionID)
}
