// Package bus provides an in-process publish/subscribe event bus that
// decouples mosaic's subsystems from each other and from the TUI.
//
// Producers publish typed [Event] values; consumers subscribe with a
// callback that runs on a dedicated dispatch goroutine. Subscribers
// have a buffered channel each so a slow consumer cannot block the
// publisher beyond the buffer size; if the buffer fills up the bus
// drops the oldest event for that subscriber and increments a counter
// (visible via [Bus.Stats]).
package bus

import "time"

// Event is any value published on the bus. mosaic uses empty
// interface (rather than a sealed marker method) so that other
// packages — claudeio, conductor, hook — can define their own typed
// payloads without changing this file. Subscribers branch on event
// type via a Go type switch.
type Event = any

// SessionStarted fires after a session row is created and the tmux
// session has been spawned.
type SessionStarted struct {
	SessionID string
	At        time.Time
}

// SessionStopped fires when a session is killed or its underlying
// process exits.
type SessionStopped struct {
	SessionID string
	At        time.Time
}

// SessionStatus fires whenever a session changes status (idle ->
// running -> waiting -> error etc.).
type SessionStatus struct {
	SessionID string
	Status    string
	At        time.Time
}
