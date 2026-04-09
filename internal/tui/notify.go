package tui

import (
	"os/exec"
	"runtime"
	"strings"
)

// notifyOS fires a best-effort native notification. It shells out
// to osascript on macOS and notify-send on Linux, and silently
// returns on platforms where neither is available. The call
// never blocks the caller beyond spawning the child process —
// failures are swallowed because "no notification" must never
// take down the TUI.
func notifyOS(title, body string) {
	if title == "" {
		title = "mosaic"
	}
	switch runtime.GOOS {
	case "darwin":
		// AppleScript is the lightest path on macOS: no extra
		// install required, works in every terminal. Escape
		// double quotes in title/body so a " in the agent
		// message does not terminate the AppleScript string.
		script := "display notification \"" + escapeAppleScript(body) +
			"\" with title \"" + escapeAppleScript(title) + "\""
		_ = exec.Command("osascript", "-e", script).Start()
	case "linux":
		// notify-send is the de-facto Linux desktop notifier.
		// We prefer urgency normal so the notification auto-
		// dismisses after the DE's timeout.
		_ = exec.Command("notify-send", "--urgency=normal", title, body).Start()
	}
	// Unsupported platforms (Windows, *BSD without notify-send)
	// fall through silently.
}

// ringBell writes an ASCII BEL to stdout so the terminal either
// flashes or beeps, depending on the user's terminal settings.
// This is cheap and platform-agnostic, so it stays the default
// "audio" path rather than wiring to a sound library.
func ringBell() {
	// Writing to stderr avoids interfering with Bubble Tea's
	// stdout alt-screen buffer; terminals still honour BEL on
	// stderr.
	_, _ = exec.Command("printf", "\a").Output()
}

// escapeAppleScript escapes double quotes and backslashes so the
// value can be embedded in a "display notification ..." script.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
