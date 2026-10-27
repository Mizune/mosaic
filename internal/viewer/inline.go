package viewer

import (
	"context"
	"fmt"
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
	// TmuxDepth is how many tmux servers sit between the process and
	// the terminal: 0 outside tmux, 1 inside one, 2 when the TUI itself
	// runs under the user's tmux and the process is in mosaic's own
	// server (a popup or split inside an attached session). Each layer
	// needs its own passthrough wrapper.
	TmuxDepth int
	Reason    string
}

// Environment abstracts the probes DetectInline needs so tests can
// script them. Zero fields fall back to the real process environment
// and the tmux CLI.
type Environment struct {
	Getenv     func(string) string
	TmuxOption func(ctx context.Context, name string) (string, error)
	// TmuxOptionAt reads a global option from the server at socket
	// (the outer tmux named by MOSAIC_OUTER_TMUX).
	TmuxOptionAt func(ctx context.Context, socket, name string) (string, error)
}

// DefaultEnvironment probes the real process.
func DefaultEnvironment() Environment {
	return Environment{Getenv: os.Getenv, TmuxOption: tmuxGlobalOption, TmuxOptionAt: tmuxGlobalOptionAt}
}

// tmuxGlobalOptionAt reads a global option from the server behind socket.
func tmuxGlobalOptionAt(ctx context.Context, socket, name string) (string, error) {
	out, err := exec.CommandContext(ctx, "tmux", "-S", socket, "show-options", "-gv", name).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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
	if env.TmuxOptionAt == nil {
		env.TmuxOptionAt = tmuxGlobalOptionAt
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
	s.TmuxDepth = 1
	v, err := env.TmuxOption(ctx, "allow-passthrough")
	if err != nil || !passthroughEnabled(v) {
		s.Supported = false
		s.Reason = "inside tmux: run `tmux set -g allow-passthrough on` to enable inline preview"
		return s
	}
	// MOSAIC_OUTER_TMUX is set on mosaic's server by the TUI at attach
	// time when the TUI itself runs inside tmux. A popup or split in
	// the attached session then has two servers to cross.
	outer := strings.TrimSpace(env.Getenv("MOSAIC_OUTER_TMUX"))
	if outer == "" {
		return s
	}
	s.TmuxDepth = 2
	socket, _, _ := strings.Cut(outer, ",")
	v, err = env.TmuxOptionAt(ctx, socket, "allow-passthrough")
	if err != nil || !passthroughEnabled(v) {
		s.Supported = false
		s.Reason = fmt.Sprintf("the outer tmux (%s) needs `tmux set -g allow-passthrough on` to enable inline preview", socket)
	}
	return s
}

func passthroughEnabled(v string) bool {
	return v == "on" || v == "all"
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
