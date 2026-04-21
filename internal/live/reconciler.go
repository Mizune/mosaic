// Package live wires mosaic's "live" data flow: it subscribes to
// claudeio + hook events on the bus, runs them through per-agent
// state machines, and persists the resulting current_tool / status /
// attention values into the SQLite store.
//
// Once the reconciler is running, the TUI's existing 1 Hz refresh
// loop is enough to put live sub-agent state on screen — the TUI
// itself does not need to know anything about the bus.
package live

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/internal/hook"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/pkg/transcript"
)

// Reconciler is the long-lived subscriber that turns bus events into
// SQLite writes. It owns one [claudeio.StateMachine] per
// (claude_session_id, agent_id) pair so it can keep track of every
// sub-agent independently. It also tracks `Agent` tool_use pairs
// from parent transcripts so sub-agent rows can be labelled with the
// real description and subagent_type instead of a hardcoded
// placeholder.
type Reconciler struct {
	store *store.Store
	bus   *bus.Bus
	log   *slog.Logger

	mu       sync.Mutex
	machines map[string]*claudeio.StateMachine // key: claudeSessionID + "::" + agentID

	// pendingAgentTools maps a parent's tool_use_id → the parsed
	// Agent tool input, held until the matching tool_result arrives
	// and tells us which agent_id this Agent call produced.
	pendingAgentTools map[string]pendingAgentTool

	// pendingQueue tracks unmatched Agent tool_use IDs in the order
	// they appeared, keyed by claudeSessionID. When a
	// SubagentSpawned event arrives, the first entry is popped and
	// its metadata is applied speculatively — before the definitive
	// tool_result has arrived.
	//
	// ORDERING ASSUMPTION: Claude Code processes Agent tool_use
	// blocks sequentially within a single assistant message and
	// spawns sub-agents in the same order, so FIFO matching between
	// the queue and SubagentSpawned events is correct. If this
	// assumption is ever violated (e.g. filesystem event reordering),
	// the definitive tool_result path in applyParentEntry will
	// overwrite the speculative metadata with the correct values.
	pendingQueue map[string][]string // claudeSessionID → ordered []tool_use_id

	// agentMeta maps (claudeSessionID + "::" + agentID) → resolved
	// metadata for a sub-agent. Populated lazily as parent
	// tool_results arrive, and consumed when sub-agent transcript
	// entries land so new rows get the right label.
	agentMeta map[string]agentMetadata

	// metaApplied tracks which sub-agents have had their metadata
	// successfully applied (non-empty). Used to avoid repeatedly
	// calling SetMetadata with empty values.
	metaApplied map[string]bool

	unsubscribe func()
	sweepCancel context.CancelFunc

	// staleness is the silence window used by the sweep loop to
	// decide a background sub-agent has finished. It defaults to
	// defaultStaleness but can be overridden via WithStaleness.
	staleness time.Duration
}

// Option configures a Reconciler at construction time.
type Option func(*Reconciler)

// WithStaleness overrides the default sub-agent staleness window
// used by the sweep loop. Zero or negative values are ignored.
func WithStaleness(d time.Duration) Option {
	return func(r *Reconciler) {
		if d > 0 {
			r.staleness = d
		}
	}
}

// NewReconciler constructs a Reconciler. Call [Reconciler.Start]
// to begin consuming events. Pass [WithStaleness] (or other
// [Option]s) to override defaults.
func NewReconciler(s *store.Store, b *bus.Bus, opts ...Option) *Reconciler {
	r := &Reconciler{
		store:             s,
		bus:               b,
		log:               slog.With("subsys", "live.reconciler"),
		machines:          map[string]*claudeio.StateMachine{},
		pendingAgentTools: map[string]pendingAgentTool{},
		pendingQueue:      map[string][]string{},
		agentMeta:         map[string]agentMetadata{},
		metaApplied:       map[string]bool{},
		staleness:         defaultStaleness,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// sweep-loop timing knobs. The values are conservative: the
// background sub-agent case is rare enough that a 5 s poll is
// cheap, and a 10 s silence window is long enough to avoid
// flapping when Claude Code briefly pauses between tool calls.
const (
	sweepInterval    = 5 * time.Second
	sweepTimeout     = 2 * time.Second
	defaultStaleness = 10 * time.Second
)

// Start subscribes to the bus and launches a background sweep loop
// that detects stale (completed) background sub-agents. Calling
// Start twice is safe: the previous sweep goroutine is cancelled
// before the new one is launched.
func (r *Reconciler) Start() {
	r.mu.Lock()
	if r.sweepCancel != nil {
		r.sweepCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.sweepCancel = cancel
	r.mu.Unlock()

	r.unsubscribe = r.bus.Subscribe(r.handle)
	go r.sweepLoop(ctx)
}

// Stop unsubscribes the handler and tears down the sweep loop.
func (r *Reconciler) Stop() {
	r.mu.Lock()
	cancel := r.sweepCancel
	r.sweepCancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if r.unsubscribe != nil {
		r.unsubscribe()
		r.unsubscribe = nil
	}
}

// handle is the event router. It is invoked on the bus's dispatch
// goroutine, so the work it does must be reasonably fast — anything
// expensive should be enqueued elsewhere.
func (r *Reconciler) handle(e bus.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	switch v := e.(type) {
	case claudeio.SubagentSpawned:
		r.handleSpawn(ctx, v)
	case claudeio.TranscriptEntry:
		r.handleEntry(ctx, v)
	case hook.NotificationEvent:
		r.handleHookNotification(ctx, v)
	case hook.BusyEvent:
		r.handleHookBusy(ctx, v)
	}
}

func (r *Reconciler) handleSpawn(ctx context.Context, ev claudeio.SubagentSpawned) {
	parent, ok := r.lookupParent(ctx, ev.SessionID)
	if !ok {
		return
	}
	meta := r.resolveAgentMeta(ev.SessionID, ev.AgentID)
	transcriptPath := claudeio.SubagentTranscriptPath("", ev.ProjectPath, ev.SessionID, ev.AgentID)
	sub, err := r.store.Subagents().Upsert(ctx, store.NewSubagent{
		ParentSessionID: parent.ID,
		ClaudeSessionID: ev.SessionID,
		AgentID:         ev.AgentID,
		SubagentType:    defaultSubagentType(meta.SubagentType),
		Name:            meta.Name,
		Description:     meta.Description,
		TranscriptPath:  transcriptPath,
	})
	if err != nil {
		r.log.Warn("upsert subagent", "err", err)
		return
	}
	// If the row already existed with placeholder metadata, apply
	// whatever we know now.
	_ = r.store.Subagents().SetMetadata(ctx, sub.ID, store.SubagentMeta{
		Name:         meta.Name,
		Description:  meta.Description,
		SubagentType: meta.SubagentType,
	})
}

func (r *Reconciler) handleEntry(ctx context.Context, ev claudeio.TranscriptEntry) {
	if ev.AgentID == "" {
		// Parent transcript: track Agent tool_use/tool_result
		// pairs to learn sub-agent metadata, and drive the parent
		// session's own state.
		r.applyParentEntry(ctx, ev)
		return
	}

	// Sub-agent transcript: feed the matching state machine and
	// persist the result.
	parent, ok := r.lookupParent(ctx, ev.SessionID)
	if !ok {
		return
	}
	meta := r.lookupAgentMeta(ev.SessionID, ev.AgentID)
	sub, err := r.store.Subagents().Upsert(ctx, store.NewSubagent{
		ParentSessionID: parent.ID,
		ClaudeSessionID: ev.SessionID,
		AgentID:         ev.AgentID,
		SubagentType:    defaultSubagentType(meta.SubagentType),
		Name:            meta.Name,
		Description:     meta.Description,
		TranscriptPath:  ev.Entry.CWD, // best-effort fallback
	})
	if err != nil {
		r.log.Warn("upsert subagent", "err", err)
		return
	}

	// Re-apply metadata when it becomes available. The initial
	// Upsert (from SubagentSpawned) often runs before the parent's
	// tool_result has been processed, leaving the row with a
	// "general-purpose" placeholder. Once the agentMeta map is
	// populated, we push the real values.
	metaMapKey := subagentKey(ev.SessionID, ev.AgentID)
	r.mu.Lock()
	applied := r.metaApplied[metaMapKey]
	r.mu.Unlock()
	if !applied && (meta.Name != "" || meta.Description != "" || meta.SubagentType != "") {
		_ = r.store.Subagents().SetMetadata(ctx, sub.ID, store.SubagentMeta{
			Name:         meta.Name,
			Description:  meta.Description,
			SubagentType: meta.SubagentType,
		})
		r.mu.Lock()
		r.metaApplied[metaMapKey] = true
		r.mu.Unlock()
	}

	sm := r.machineFor(ev.SessionID, ev.AgentID)
	sm.Observe(ev.Entry)
	snap := sm.Snapshot()

	// Only persist tool changes when a new tool starts. Avoid
	// clearing current_tool between rapid tool calls — the TUI's
	// 1 Hz refresh almost always misses the momentary non-idle
	// window, resulting in permanent "(idle)" for fast agents.
	// MarkCompleted / MarkErrored handle the final cleanup.
	if snap.CurrentTool != "" {
		_ = r.store.Subagents().UpdateRunningTool(ctx, sub.ID, snap.CurrentTool, snap.CurrentToolStartedAt)
	} else {
		// No active tool, but we still need the spawning → running
		// status transition so the TUI shows the correct icon for
		// agents whose first transcript entries are text-only.
		_ = r.store.Subagents().PromoteSpawning(ctx, sub.ID)
	}
}

// applyParentEntry processes a parent-session transcript entry. It
// does two things:
//
//  1. Tracks `Agent` tool_use/tool_result pairs so sub-agent rows
//     can be labelled with the real description / subagent_type
//     from the parent's Task tool input.
//  2. Forwards the entry to a parent state machine so we can update
//     the *parent* session's status when the assistant runs a tool.
func (r *Reconciler) applyParentEntry(ctx context.Context, ev claudeio.TranscriptEntry) {
	if ev.Entry.Type != transcript.EntryAssistant && ev.Entry.Type != transcript.EntryUser {
		return
	}

	// Agent tool_use / tool_result tracking. Walk each content
	// block once and funnel it into the metadata map so a
	// subsequent sub-agent transcript entry can pick up the right
	// label.
	if ev.Entry.Message != nil {
		for _, block := range ev.Entry.Message.Content {
			switch {
			case block.IsToolUse() && block.Name == "Agent":
				in, err := transcript.ParseSubAgentInput(block.Input)
				if err != nil {
					continue
				}
				r.mu.Lock()
				r.pendingAgentTools[block.ID] = pendingAgentTool{
					ClaudeSessionID: ev.SessionID,
					Input:           in,
					CreatedAt:       time.Now(),
				}
				r.pendingQueue[ev.SessionID] = append(
					r.pendingQueue[ev.SessionID], block.ID,
				)
				r.mu.Unlock()

			case block.IsToolResult() && block.ToolUseID != "":
				r.mu.Lock()
				pending, ok := r.pendingAgentTools[block.ToolUseID]
				if !ok {
					r.mu.Unlock()
					continue
				}
				delete(r.pendingAgentTools, block.ToolUseID)
				// Also remove from the speculative queue if it
				// was not already consumed by speculativeMatch.
				r.removeFromQueue(pending.ClaudeSessionID, block.ToolUseID)
				r.mu.Unlock()

				agentID := extractAgentID(block.Content)
				if agentID == "" {
					continue
				}
				meta := agentMetadata{
					Name:            pending.Input.Name,
					Description:     pending.Input.Description,
					SubagentType:    pending.Input.SubagentType,
					RunInBackground: pending.Input.RunInBackground,
				}
				r.storeAgentMeta(pending.ClaudeSessionID, agentID, meta)

				// Pre-insert the sub-agent row so the TUI sees
				// it the instant the parent finishes the Agent
				// call, even before the sub-agent writes its
				// own first transcript line.
				if parent, pok := r.lookupParent(ctx, pending.ClaudeSessionID); pok {
					transcriptPath := claudeio.SubagentTranscriptPath(
						"", parent.WorktreePath, pending.ClaudeSessionID, agentID,
					)
					sub, err := r.store.Subagents().Upsert(ctx, store.NewSubagent{
						ParentSessionID: parent.ID,
						ClaudeSessionID: pending.ClaudeSessionID,
						AgentID:         agentID,
						SubagentType:    defaultSubagentType(meta.SubagentType),
						Name:            meta.Name,
						Description:     meta.Description,
						TranscriptPath:  transcriptPath,
					})
					if err == nil {
						_ = r.store.Subagents().SetMetadata(ctx, sub.ID, store.SubagentMeta{
							Name:         meta.Name,
							Description:  meta.Description,
							SubagentType: meta.SubagentType,
						})
						metaMapKey := subagentKey(pending.ClaudeSessionID, agentID)
						r.mu.Lock()
						r.metaApplied[metaMapKey] = true
						r.mu.Unlock()

						// For foreground agents, the tool_result
						// means the agent has finished — route
						// through markSubagentDone so caches are
						// pruned alongside the status flip.
						if !pending.Input.RunInBackground {
							r.markSubagentDone(ctx, sub.ID,
								pending.ClaudeSessionID, agentID, block.IsError)
						}
					}
				}
			}
		}
	}

	parent, ok := r.lookupParent(ctx, ev.SessionID)
	if !ok {
		return
	}
	sm := r.machineFor(ev.SessionID, "")
	sm.Observe(ev.Entry)
	snap := sm.Snapshot()
	// Flip the parent's status to track the state machine: a
	// currently-running tool means the agent is actively working,
	// an empty CurrentTool means the last tool_result just arrived
	// and the agent is waiting for the next prompt. We only touch
	// `running` and `idle` here — `waiting_input` / `stopped` /
	// `error` are owned by other code paths and must not be
	// stomped on.
	//
	// Without this else branch, any session that ever ran a tool
	// would be stuck at "running" forever whenever Claude Code
	// hooks are not installed.
	current := parent.Status
	if snap.CurrentTool != "" {
		if current != string(store.StatusRunning) {
			_ = r.store.Sessions().UpdateStatus(ctx, parent.ID, store.StatusRunning)
		}
	} else {
		if current == string(store.StatusRunning) {
			_ = r.store.Sessions().UpdateStatus(ctx, parent.ID, store.StatusIdle)
		}
	}
}

// lookupAgentMeta returns the cached metadata for a sub-agent, or
// the zero value when the matching parent tool_result has not
// arrived yet.
func (r *Reconciler) lookupAgentMeta(claudeSessionID, agentID string) agentMetadata {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agentMeta[claudeSessionID+"::"+agentID]
}

// storeAgentMeta writes a resolved metadata entry.
func (r *Reconciler) storeAgentMeta(claudeSessionID, agentID string, meta agentMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentMeta[claudeSessionID+"::"+agentID] = meta
}

// resolveAgentMeta returns metadata for a sub-agent under a single
// lock acquisition. It first checks agentMeta (populated when the
// parent's tool_result arrives). If no entry exists, it falls back
// to speculative FIFO matching against the pending Agent tool_use
// queue — Claude Code spawns sub-agents in the same order the
// tool_use blocks appeared in the parent transcript.
func (r *Reconciler) resolveAgentMeta(claudeSessionID, agentID string) agentMetadata {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Fast path: metadata already resolved by the tool_result handler.
	key := claudeSessionID + "::" + agentID
	if meta, ok := r.agentMeta[key]; ok {
		return meta
	}

	// Slow path: speculative FIFO match against the pending queue.
	queue := r.pendingQueue[claudeSessionID]
	now := time.Now()

	// Compact: drop consumed or stale entries from both the queue
	// and pendingAgentTools so subsequent calls never re-scan them.
	j := 0
	for _, id := range queue {
		p, ok := r.pendingAgentTools[id]
		if !ok {
			continue // consumed by tool_result handler
		}
		if !p.CreatedAt.IsZero() && now.Sub(p.CreatedAt) > pendingToolExpiry {
			delete(r.pendingAgentTools, id)
			continue // stale
		}
		queue[j] = id
		j++
	}
	queue = queue[:j]

	// Match: take the first remaining entry.
	if len(queue) == 0 {
		r.pendingQueue[claudeSessionID] = queue
		return agentMetadata{}
	}

	toolUseID := queue[0]
	pending := r.pendingAgentTools[toolUseID]
	meta := agentMetadata{
		Name:            pending.Input.Name,
		Description:     pending.Input.Description,
		SubagentType:    pending.Input.SubagentType,
		RunInBackground: pending.Input.RunInBackground,
	}
	// Store the metadata so subsequent handleEntry calls find it.
	r.agentMeta[key] = meta

	// Remove the matched entry from queue but NOT from
	// pendingAgentTools — the tool_result handler still needs the
	// entry to extract the definitive agent_id and confirm the match.
	r.pendingQueue[claudeSessionID] = queue[1:]
	return meta
}

// removeFromQueue removes a single tool_use_id from the per-session
// pending queue. Caller must hold r.mu.
func (r *Reconciler) removeFromQueue(claudeSessionID, toolUseID string) {
	q, ok := r.pendingQueue[claudeSessionID]
	if !ok {
		return
	}
	for i, id := range q {
		if id == toolUseID {
			r.pendingQueue[claudeSessionID] = append(q[:i], q[i+1:]...)
			return
		}
	}
}

func (r *Reconciler) handleHookBusy(ctx context.Context, ev hook.BusyEvent) {
	parent, ok := r.lookupParentByWorktreeID(ctx, ev.WorktreeID)
	if !ok {
		return
	}
	if ev.Active {
		// UserPromptSubmit arrived → the user is actively
		// engaging the agent. Any attention state left over
		// from a prior cycle (stale permission prompt, idle
		// notification that never cleared) is by definition
		// resolved, so wipe it before flipping status.
		_ = r.store.Sessions().SetAttention(ctx, parent.ID,
			store.AttentionNone, "", "", "")
		_ = r.store.Sessions().UpdateStatus(ctx, parent.ID, store.StatusRunning)
	} else {
		// Stop / PostToolUseFailure / SessionEnd → agent is
		// done for now. Clear any hook-sourced attention too
		// because the immediately-following Notification hook
		// (if any) will re-set it only for real permission
		// requests, not the routine "waiting for input" cue.
		_ = r.store.Sessions().SetAttention(ctx, parent.ID,
			store.AttentionNone, "", "", "")
		_ = r.store.Sessions().UpdateStatus(ctx, parent.ID, store.StatusIdle)
	}
}

func (r *Reconciler) handleHookNotification(ctx context.Context, ev hook.NotificationEvent) {
	parent, ok := r.lookupParentByWorktreeID(ctx, ev.WorktreeID)
	if !ok {
		return
	}
	// Record every notification for the history popover — even
	// the idle-state ones are useful when auditing what the agent
	// has been doing.
	if _, err := r.store.Notifications().Insert(ctx, store.NewNotification{
		SessionID: parent.ID,
		Source:    store.NotificationSourceHook,
		Agent:     ev.Agent,
		Event:     ev.Event,
		Title:     ev.Title,
		Body:      ev.Body,
	}); err != nil {
		r.log.Warn("insert notification", "err", err)
	}
	// Only the subset of Notifications that actually block the
	// agent (permission requests, approvals, explicit confirmation
	// prompts) should flip the session into waiting_input. Claude
	// Code's routine "Claude is waiting for your input" cue fires
	// on every idle transition and would otherwise pin the ⚠
	// indicator indefinitely.
	if !hook.IsBlockingNotification(ev) {
		return
	}
	_ = r.store.Sessions().SetAttention(ctx, parent.ID,
		store.AttentionWaitingInput, ev.Title, ev.Body, "hook")
}

// lookupParent finds the mosaic session that owns the given Claude
// session id. Returns ok=false when no binding exists yet.
func (r *Reconciler) lookupParent(ctx context.Context, claudeSessionID string) (store.Session, bool) {
	if claudeSessionID == "" {
		return store.Session{}, false
	}
	rows, err := r.store.Sessions().List(ctx)
	if err != nil {
		return store.Session{}, false
	}
	for _, s := range rows {
		if s.ClaudeSessionID.Valid && s.ClaudeSessionID.String == claudeSessionID {
			return s, true
		}
	}
	return store.Session{}, false
}

// lookupParentByWorktreeID does the same lookup but uses the
// worktree id field that hook payloads carry. mosaic uses the
// session ULID as the worktree id, so we can look up directly.
func (r *Reconciler) lookupParentByWorktreeID(ctx context.Context, worktreeID string) (store.Session, bool) {
	if worktreeID == "" {
		return store.Session{}, false
	}
	row, err := r.store.Sessions().GetByID(ctx, worktreeID)
	if err == nil {
		return row, true
	}
	return store.Session{}, false
}

func (r *Reconciler) machineFor(sessionID, agentID string) *claudeio.StateMachine {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := subagentKey(sessionID, agentID)
	sm, ok := r.machines[key]
	if !ok {
		sm = claudeio.NewStateMachine()
		r.machines[key] = sm
	}
	return sm
}

// subagentKey is the single source of truth for how the reconciler
// composes (claudeSessionID, agentID) pairs into map keys.
func subagentKey(claudeSessionID, agentID string) string {
	return claudeSessionID + "::" + agentID
}

// markSubagentDone transitions a sub-agent row to a terminal state
// (completed or errored) and drops the per-(session,agent) caches
// that would otherwise grow unboundedly across the process
// lifetime. Both the foreground tool_result path and the background
// sweep path go through this helper so the lifecycle logic lives in
// one place.
func (r *Reconciler) markSubagentDone(ctx context.Context, subID, claudeSessionID, agentID string, isError bool) {
	if isError {
		_ = r.store.Subagents().MarkErrored(ctx, subID)
	} else {
		_ = r.store.Subagents().MarkCompleted(ctx, subID)
	}
	r.forgetSubagent(claudeSessionID, agentID)
}

// forgetSubagent removes per-agent entries from the reconciler's
// in-memory maps. Safe to call multiple times.
func (r *Reconciler) forgetSubagent(claudeSessionID, agentID string) {
	key := subagentKey(claudeSessionID, agentID)
	r.mu.Lock()
	delete(r.machines, key)
	delete(r.agentMeta, key)
	delete(r.metaApplied, key)
	r.mu.Unlock()
}

// sweepLoop runs a periodic check for stale background sub-agents
// that have stopped producing transcript entries.
func (r *Reconciler) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, sweepTimeout)
			r.sweepStaleSubagents(sweepCtx, r.staleness)
			r.pruneStalePendingTools()
			cancel()
		}
	}
}

// sweepStaleSubagents checks all running sub-agents whose state
// machines show no pending tools and no recent activity, and marks
// them as completed. This handles background agents whose parent
// tool_result arrives on launch, not completion.
//
// It queries the store directly for running rows (bounded read)
// rather than walking every session, and falls back to the row's
// StartedAt when the state machine has never observed an entry so
// an agent that terminated without writing any transcript still
// gets cleaned up.
func (r *Reconciler) sweepStaleSubagents(ctx context.Context, staleness time.Duration) {
	r.mu.Lock()
	machines := make(map[string]*claudeio.StateMachine, len(r.machines))
	for k, v := range r.machines {
		machines[k] = v
	}
	r.mu.Unlock()

	subs, err := r.store.Subagents().ListRunning(ctx)
	if err != nil {
		return
	}
	for _, sub := range subs {
		key := subagentKey(sub.ClaudeSessionID, sub.AgentID)
		sm, ok := machines[key]

		// Determine the most recent signal of activity. If no
		// state machine exists (e.g. the watcher never received
		// an entry for this agent), fall back to StartedAt so
		// agents that terminated before producing transcript
		// output are still cleaned up.
		var lastActive time.Time
		pendingCount := 0
		awaitingInput := false
		if ok {
			snap := sm.Snapshot()
			lastActive = snap.LastEventAt
			pendingCount = snap.PendingCount
			awaitingInput = snap.AwaitingInput
		}
		if lastActive.IsZero() {
			lastActive = sub.StartedAt
		}

		if pendingCount == 0 &&
			!awaitingInput &&
			!lastActive.IsZero() &&
			time.Since(lastActive) > staleness {
			r.markSubagentDone(ctx, sub.ID,
				sub.ClaudeSessionID, sub.AgentID, false)
		}
	}
}

// pruneStalePendingTools removes expired entries from
// pendingAgentTools and pendingQueue. An entry expires when its
// tool_result never arrived within pendingToolExpiry.
func (r *Reconciler) pruneStalePendingTools() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for id, p := range r.pendingAgentTools {
		if !p.CreatedAt.IsZero() && now.Sub(p.CreatedAt) > pendingToolExpiry {
			delete(r.pendingAgentTools, id)
		}
	}
	for sid, queue := range r.pendingQueue {
		filtered := queue[:0] // in-place filter reusing the backing array
		for _, id := range queue {
			if _, ok := r.pendingAgentTools[id]; ok {
				filtered = append(filtered, id)
			}
		}
		if len(filtered) == 0 {
			delete(r.pendingQueue, sid)
		} else {
			r.pendingQueue[sid] = filtered
		}
	}
}
