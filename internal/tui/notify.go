package tui

import (
	"os"
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

// playSound plays an audible notification. When soundFile is
// non-empty, afplay (macOS) or paplay (Linux) plays that file.
// Otherwise the system default is used: osascript beep on macOS,
// terminal BEL on Linux.
func playSound(soundFile string) {
	if soundFile != "" {
		playCustomSound(soundFile)
		return
	}
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e", "beep").Start()
	case "linux":
		_, _ = os.Stderr.Write([]byte("\a"))
	}
}

// playCustomSound plays an arbitrary audio file via the platform's
// CLI player. Unrecognised platforms are silently ignored.
func playCustomSound(path string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("afplay", path).Start()
	case "linux":
		_ = exec.Command("paplay", path).Start()
	}
}

// escapeAppleScript escapes double quotes and backslashes so the
// value can be embedded in a "display notification ..." script.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
