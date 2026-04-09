package tmux

// SessionName builds a tmux session name from a mosaic session ULID.
// It is exposed at package scope so callers and tests can produce the
// same value without depending on the prefix constant directly.
func SessionName(sessionID string) string {
	return SessionPrefix + sessionID
}
