package viewer

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// Inline modes accepted by [viewer] inline.
const (
	InlineAuto = "auto"
	InlineOn   = "on"
	InlineOff  = "off"
)

// InlineSupport is the result of [DetectInline]: whether the artifact
// picker may draw images into the terminal with the Kitty graphics
// protocol, and when it may not, the reason phrased as the fix.
type InlineSupport struct {
	Supported bool
	InTmux    bool
	Reason    string
}

// Environment abstracts the probes DetectInline needs so tests can
// script them. Zero fields fall back to the real process environment
// and the tmux CLI.
type Environment struct {
	Getenv     func(string) string
	TmuxOption func(ctx context.Context, name string) (string, error)
}

// DefaultEnvironment probes the real process.
func DefaultEnvironment() Environment {
	return Environment{Getenv: os.Getenv, TmuxOption: tmuxGlobalOption}
}

// tmuxGlobalOption reads a global option from the tmux server named by
// $TMUX (the one hosting the TUI, not mosaic's own `-L mosaic` server).
func tmuxGlobalOption(ctx context.Context, name string) (string, error) {
	out, err := exec.CommandContext(ctx, "tmux", "show-options", "-gv", name).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// DetectInline decides whether inline preview can work here. mode is
// the [viewer] inline value: "off" disables it, "on" trusts the terminal
// without checking, "auto" (or anything else) requires a terminal that
// advertises Kitty graphics. Inside tmux the server must also have
// allow-passthrough set to on or all; the Kitty escape sequences cannot
// reach the outer terminal otherwise.
func DetectInline(ctx context.Context, mode string, env Environment) InlineSupport {
	if env.Getenv == nil {
		env.Getenv = os.Getenv
	}
	if env.TmuxOption == nil {
		env.TmuxOption = tmuxGlobalOption
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case InlineOff:
		return InlineSupport{Reason: `inline preview is off ([viewer] inline = "off"); Enter opens the artifact in a browser`}
	case InlineOn:
	default:
		if !kittyTerminal(env.Getenv) {
			return InlineSupport{Reason: "this terminal does not advertise Kitty graphics (Ghostty, kitty, WezTerm); Enter opens the artifact in a browser"}
		}
	}
	s := InlineSupport{Supported: true}
	if env.Getenv("TMUX") == "" {
		return s
	}
	s.InTmux = true
	v, err := env.TmuxOption(ctx, "allow-passthrough")
	if err != nil || (v != "on" && v != "all") {
		s.Supported = false
		s.Reason = "inside tmux: run `tmux set -g allow-passthrough on` to enable inline preview"
	}
	return s
}

// kittyTerminal reports whether the environment points at a terminal
// that implements the Kitty graphics protocol with Unicode placeholders.
// The Ghostty and kitty variables survive a tmux hop because the tmux
// server inherits them from the client that started it.
func kittyTerminal(getenv func(string) string) bool {
	for _, key := range []string{"GHOSTTY_RESOURCES_DIR", "KITTY_WINDOW_ID", "WEZTERM_EXECUTABLE"} {
		if getenv(key) != "" {
			return true
		}
	}
	switch strings.ToLower(getenv("TERM_PROGRAM")) {
	case "ghostty", "kitty", "wezterm":
		return true
	}
	switch getenv("TERM") {
	case "xterm-kitty", "xterm-ghostty":
		return true
	}
	return false
}
