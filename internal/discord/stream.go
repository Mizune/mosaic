package discord

import (
	"sync"
	"time"

	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/pkg/transcript"
)

// debounceInterval is the minimum time between consecutive messages
// to the same Discord thread. This prevents flooding when tools run
// in rapid succession.
const debounceInterval = 2 * time.Second

// threadDebouncer tracks the last message time per thread.
type threadDebouncer struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newDebouncer() *threadDebouncer {
	return &threadDebouncer{last: make(map[string]time.Time)}
}

// remove deletes a thread entry from the debouncer.
func (d *threadDebouncer) remove(threadID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.last, threadID)
}

// allow returns true if enough time has elapsed since the last
// message to this thread.
func (d *threadDebouncer) allow(threadID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	if last, ok := d.last[threadID]; ok && now.Sub(last) < debounceInterval {
		return false
	}
	d.last[threadID] = now
	return true
}

// handleTranscript streams tool_use/tool_result/text events from
// the parent transcript to the corresponding Discord thread.
func (br *Bridge) handleTranscript(ev claudeio.TranscriptEntry) {
	// Only stream parent transcripts — sub-agent events are too
	// noisy for Discord.
	if ev.AgentID != "" {
		return
	}

	if ev.Entry.Type != transcript.EntryAssistant {
		return
	}
	if ev.Entry.Message == nil {
		return
	}

	session, ok := br.lookupSessionByClaudeID(ev.SessionID)
	if !ok {
		return
	}

	threadID := br.threadFor(session.ID)
	if threadID == "" {
		return
	}

	for _, block := range ev.Entry.Message.Content {
		br.streamBlock(threadID, session, block)
	}
}

// streamBlock dispatches a single content block to Discord.
func (br *Bridge) streamBlock(threadID string, _ store.Session, block transcript.ContentBlock) {
	var msg string
	important := false

	switch {
	case block.IsToolUse():
		msg = formatToolStart(block)
	case block.IsToolResult():
		msg = formatToolResult(block)
		// Error results are always delivered — they are critical
		// signals that should never be swallowed by debouncing.
		important = block.IsError
	case block.IsText() && block.Text != "":
		msg = formatAssistantText(block.Text)
		// Assistant text is always delivered — it contains the
		// agent's reasoning and is high-value for remote users.
		important = true
	default:
		return
	}

	if msg == "" {
		return
	}

	// Debounce rapid-fire tool events, but always deliver
	// important messages (errors, assistant text).
	if br.debouncer == nil {
		return
	}
	if !important && !br.debouncer.allow(threadID) {
		return
	}

	br.sendToThread(threadID, msg)
}
