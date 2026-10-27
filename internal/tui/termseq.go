package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// termSeqFlushDelay is how long a queued terminal sequence stays in the
// frame. Bubble Tea flushes at 60 fps, so one frame is enough; a few
// extra frames cost nothing because unchanged lines are not re-sent.
const termSeqFlushDelay = 250 * time.Millisecond

// termSeqFlushedMsg clears a queued terminal sequence.
type termSeqFlushedMsg struct{ gen int }

// termSeqQueue holds raw terminal sequences (Kitty graphics transmit /
// delete) that a host emits at the start of its next frames, then
// drops. Going through View keeps the write inside the renderer instead
// of racing it from another goroutine. Hosts embed it so `termSeq` is
// a field of theirs.
type termSeqQueue struct {
	termSeq    string
	termSeqGen int
}

// queue appends seq and returns the tick that will clear it. A late
// flush for an older generation leaves newer sequences in place.
func (q *termSeqQueue) queue(seq string) tea.Cmd {
	if seq == "" {
		return nil
	}
	q.termSeq += seq
	q.termSeqGen++
	gen := q.termSeqGen
	return tea.Tick(termSeqFlushDelay, func(time.Time) tea.Msg { return termSeqFlushedMsg{gen: gen} })
}

func (q *termSeqQueue) applyFlushed(v termSeqFlushedMsg) {
	if v.gen == q.termSeqGen {
		q.termSeq = ""
	}
}

// prefix puts the queued sequences in front of a rendered frame. They
// are zero-width, so the layout is unaffected.
func (q *termSeqQueue) prefix(frame string) string {
	if q.termSeq == "" {
		return frame
	}
	return q.termSeq + frame
}
