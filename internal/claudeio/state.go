package claudeio

import (
	"sync"
	"time"

	"github.com/Mizune/mosaic/pkg/transcript"
)

// PendingTool is one tool_use that has not yet been answered by a
// matching tool_result.
type PendingTool struct {
	ID      string
	Name    string
	Started time.Time
}

// AgentSnapshot is the state machine's view of one agent (parent or
// sub-agent) at a point in time. It is the structure the TUI binds
// against.
type AgentSnapshot struct {
	// CurrentTool is the most recent tool_use without a matching
	// tool_result, or empty if the agent is idle.
	CurrentTool string

	// CurrentToolStartedAt is when CurrentTool began running.
	CurrentToolStartedAt time.Time

	// PendingCount is the number of in-flight tool_use blocks.
	PendingCount int

	// LastEventAt is the timestamp of the most recently observed
	// transcript line.
	LastEventAt time.Time

	// LastNotificationTitle / Body are populated whenever a
	// Notification entry is observed (Claude waiting for input).
	LastNotificationTitle string
	LastNotificationBody  string

	// AwaitingInput is true while the most recent Notification has
	// not been dismissed and the agent has not started a new tool.
	AwaitingInput bool
}

// StateMachine consumes [transcript.Entry] values from a single
// transcript and reports the running tool / pending count / awaiting
// flag for that agent. The zero value is ready to use, but most
// callers will instantiate it via [NewStateMachine] for clarity.
//
// StateMachine is safe for concurrent observers (the watcher pushes
// from one goroutine while the TUI reads via Snapshot from another).
type StateMachine struct {
	mu       sync.RWMutex
	pending  map[string]PendingTool // tool_use_id → pending
	snapshot AgentSnapshot
}

// NewStateMachine returns an empty machine.
func NewStateMachine() *StateMachine {
	return &StateMachine{pending: map[string]PendingTool{}}
}

// Observe folds an entry into the machine. Unrelated entries (file
// snapshots, queue ops, ...) are ignored.
func (m *StateMachine) Observe(e transcript.Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !e.Timestamp.IsZero() {
		m.snapshot.LastEventAt = e.Timestamp
	}

	if e.Message == nil {
		return
	}
	for _, c := range e.Message.Content {
		switch {
		case c.IsToolUse():
			m.pending[c.ID] = PendingTool{
				ID:      c.ID,
				Name:    c.Name,
				Started: e.Timestamp,
			}
			m.snapshot.CurrentTool = c.Name
			m.snapshot.CurrentToolStartedAt = e.Timestamp
			m.snapshot.PendingCount = len(m.pending)
			// A new tool_use means the agent is no longer waiting on
			// the user — clear the attention flag.
			m.snapshot.AwaitingInput = false

		case c.IsToolResult():
			delete(m.pending, c.ToolUseID)
			m.snapshot.PendingCount = len(m.pending)
			if len(m.pending) == 0 {
				m.snapshot.CurrentTool = ""
				m.snapshot.CurrentToolStartedAt = time.Time{}
			} else {
				// Promote the most recently started pending tool to
				// CurrentTool so the TUI keeps showing something
				// useful while the agent is in the middle of a chain
				// of tool calls.
				latest := m.latestPending()
				m.snapshot.CurrentTool = latest.Name
				m.snapshot.CurrentToolStartedAt = latest.Started
			}
		}
	}
}

// ObserveNotification folds a hook-style Notification into the
// machine: it sets AwaitingInput=true and records the title/body so
// the TUI can render an attention banner.
func (m *StateMachine) ObserveNotification(title, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot.LastNotificationTitle = title
	m.snapshot.LastNotificationBody = body
	m.snapshot.AwaitingInput = true
}

// DismissNotification clears the attention flag without affecting the
// stored title/body (so the user can still inspect the most recent
// notification through the history view).
func (m *StateMachine) DismissNotification() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot.AwaitingInput = false
}

// Snapshot returns the current view. The returned struct is a copy.
func (m *StateMachine) Snapshot() AgentSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

// latestPending returns the pending tool with the most recent
// Started timestamp. Caller must hold the lock.
func (m *StateMachine) latestPending() PendingTool {
	var latest PendingTool
	for _, p := range m.pending {
		if p.Started.After(latest.Started) {
			latest = p
		}
	}
	return latest
}
