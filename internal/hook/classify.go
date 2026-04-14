package hook

import "strings"

// IsBlockingNotification returns true when a Claude Code
// Notification payload looks like a real "user must act for the
// agent to continue" cue, as opposed to the informational "I'm
// idle, submit your next prompt" one. The classifier is
// intentionally permissive: any ambiguous case is treated as
// blocking so we err on the side of surfacing attention rather
// than hiding it.
func IsBlockingNotification(ev NotificationEvent) bool {
	text := strings.ToLower(ev.Title + " " + ev.Body)
	// Idle cues Claude Code emits after Stop — these should
	// never be treated as attention.
	idlePhrases := []string{
		"waiting for your input",
		"waiting for input",
		"ready for next",
		"awaiting your next",
	}
	for _, phrase := range idlePhrases {
		if strings.Contains(text, phrase) {
			return false
		}
	}
	// Explicit "action required" cues.
	blockingPhrases := []string{
		"permission",
		"approve",
		"approval",
		"confirm",
		"do you want",
		"proceed",
		"consent",
	}
	for _, phrase := range blockingPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	// Unknown shape → lean toward visibility: surface it so the
	// user can dismiss it manually if it turns out to be noise.
	return true
}
