// Package tmux wraps the tmux command line in a small typed API used by
// mosaic's session service. mosaic always runs tmux against a private
// socket (`tmux -L mosaic`) so it cannot interfere with the user's
// regular tmux sessions, and it namespaces every session it creates
// with a `mos_` prefix.
//
// The package exposes a [Backend] interface so the rest of mosaic can
// be tested with a fake implementation; tests that exercise the real
// tmux binary live behind the `tmux` build tag.
package tmux

// DefaultSocket is the value passed to `tmux -L`. mosaic uses a single
// dedicated socket so it never collides with the user's main tmux.
const DefaultSocket = "mosaic"

// SessionPrefix is prepended to every tmux session name mosaic spawns.
// Combined with the session ULID it produces values like
// `mos_01HV8Z7E2TGM4S9D6X1A1Q0BNH` that are easy to grep for.
const SessionPrefix = "mos_"
