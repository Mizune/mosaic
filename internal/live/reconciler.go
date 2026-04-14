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

	// agentMeta maps (claudeSessionID + "::" + agentID) → resolved
	// metadata for a sub-agent. Populated lazily as parent
	// tool_results arrive, and consumed when sub-agent transcript
	// entries land so new rows get the right label.
	agentMeta map[string]agentMetadata

	unsubscribe func()
}

// NewReconciler constructs a Reconciler. Call [Reconciler.Start]
// to begin consuming events.
func NewReconciler(s *store.Store, b *bus.Bus) *Reconciler {
	return &Reconciler{
		store:             s,
		bus:               b,
		log:               slog.With("subsys", "live.reconciler"),
		machines:          map[string]*claudeio.StateMachine{},
		pendingAgentTools: map[string]pendingAgentTool{},
		agentMeta:         map[string]agentMetadata{},
	}
}

// Start subscribes to the bus. The returned cleanup func unsubscribes
// the handler so callers can `defer r.Stop()` from a TUI shutdown.
func (r *Reconciler) Start() {
	r.unsubscribe = r.bus.Subscribe(r.handle)
}

// Stop unsubscribes the handler.
func (r *Reconciler) Stop() {
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
	meta := r.lookupAgentMeta(ev.SessionID, ev.AgentID)
	transcriptPath := claudeio.SubagentTranscriptPath("", ev.ProjectPath, ev.SessionID, ev.AgentID)
	sub, err := r.store.Subagents().Upsert(ctx, store.NewSubagent{
		ParentSessionID: parent.ID,
		ClaudeSessionID: ev.SessionID,
		AgentID:         ev.AgentID,
		SubagentType:    defaultSubagentType(meta.SubagentType),
		Description:     meta.Description,
		TranscriptPath:  transcriptPath,
	})
	if err != nil {
		r.log.Warn("upsert subagent", "err", err)
		return
	}
	// If the row already existed with placeholder metadata, apply
	// whatever we know now.
	_ = r.store.Subagents().SetMetadata(ctx, sub.ID, meta.Description, meta.SubagentType)
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
		Description:     meta.Description,
		TranscriptPath:  ev.Entry.CWD, // best-effort fallback
	})
	if err != nil {
		r.log.Warn("upsert subagent", "err", err)
		return
	}
	_ = r.store.Subagents().SetMetadata(ctx, sub.ID, meta.Description, meta.SubagentType)

	sm := r.machineFor(ev.SessionID, ev.AgentID)
	sm.Observe(ev.Entry)
	snap := sm.Snapshot()

	if snap.CurrentTool == "" {
		_ = r.store.Subagents().UpdateRunningTool(ctx, sub.ID, "", time.Time{})
	} else {
		_ = r.store.Subagents().UpdateRunningTool(ctx, sub.ID, snap.CurrentTool, snap.CurrentToolStartedAt)
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
				}
				r.mu.Unlock()

			case block.IsToolResult() && block.ToolUseID != "":
				r.mu.Lock()
				pending, ok := r.pendingAgentTools[block.ToolUseID]
				if !ok {
					r.mu.Unlock()
					continue
				}
				delete(r.pendingAgentTools, block.ToolUseID)
				r.mu.Unlock()

				agentID := extractAgentID(block.Content)
				if agentID == "" {
					continue
				}
				meta := agentMetadata{
					Description:  pending.Input.Description,
					SubagentType: pending.Input.SubagentType,
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
						Description:     meta.Description,
						TranscriptPath:  transcriptPath,
					})
					if err == nil {
						_ = r.store.Subagents().SetMetadata(ctx, sub.ID, meta.Description, meta.SubagentType)
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
	key := sessionID + "::" + agentID
	sm, ok := r.machines[key]
	if !ok {
		sm = claudeio.NewStateMachine()
		r.machines[key] = sm
	}
	return sm
}
