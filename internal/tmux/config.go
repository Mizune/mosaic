package tmux

import (
	"os"
	"path/filepath"
)

// Artifact viewer modes for the prefix+O binding; see [ConfigOptions].
const (
	ArtifactViewerPopup = "popup"
	ArtifactViewerSplit = "split"
)

// ConfigOptions parameterises the generated tmux config.
type ConfigOptions struct {
	// ArtifactViewer is how prefix+O opens `mosaic artifacts`: a tmux
	// popup (default) or a right-hand split.
	ArtifactViewer string
}

// DefaultConfigBody renders the tmux configuration mosaic installs so
// every session it spawns has a consistent detach binding and a few
// quality-of-life tweaks.
//
// The most important line is `bind-key -n C-q detach-client`, which
// is how the user gets back to the mosaic TUI from inside an attached
// Claude session. Without it, `q`/`Esc` just type characters into
// whatever the agent is running, and the user has no escape hatch
// short of killing the pane.
func DefaultConfigBody(opts ConfigOptions) string {
	return configBodyHead + artifactViewerBlock(opts) + configBodyTail
}

// artifactViewerBlock is the prefix+O binding. The popup variant tags
// the environment so the viewer can tell it runs inside a popup.
func artifactViewerBlock(opts ConfigOptions) string {
	const comment = "\n# Open the artifact viewer for the current session (prefix+O).\n" +
		"# 'mosaic artifacts' resolves the session the same way 'mosaic diff' does.\n"
	if opts.ArtifactViewer == ArtifactViewerSplit {
		return comment + `bind-key O split-window -h "mosaic artifacts"` + "\n"
	}
	return comment + `bind-key O display-popup -E -w 90% -h 90% -T " artifacts " -e MOSAIC_TMUX_POPUP=1 "mosaic artifacts"` + "\n"
}

const configBodyHead = `# mosaic-managed tmux config — regenerated on every startup.
# To override, edit ~/.mosaic/tmux-overlay.conf (sourced at the end).

# Detach from a mosaic session and return to the TUI.
bind-key -n C-q detach-client

# Also allow the tmux prefix version so users with a habit of
# C-b d still work.
bind-key d detach-client

# Open a side-pane diff view for the current session. Runs
# 'mosaic diff' which auto-resolves the session via the
# MOSAIC_WORKTREE_ID env var mosaic injected into this pane.
# We deliberately do NOT pipe through 'less' here: the new pane
# is a PTY, so git's own TTY detection enables --color and runs
# its configured pager (less, by default) for us. Piping ourselves
# would force --color=auto into "no color" because stdout becomes
# a pipe.
bind-key D split-window -h "mosaic diff"
`

const configBodyTail = `
# Keep ESC snappy in editors running inside a session.
set -sg escape-time 10

# Allow OSC / Kitty graphics escape sequences to pass through tmux
# to the outer terminal. Without this, Claude Code's status line and
# similar features are invisible. 'all' (not just 'on') keeps the
# artifact viewer's inline images working from a popup, whose pane
# is hidden underneath it.
set -g allow-passthrough all

# Larger scrollback so capture-pane can pick up long tool outputs.
set -g history-limit 50000

# Mouse support so the TUI's split preview is navigable.
set -g mouse on

# Status bar: show mosaic session summary in the right-hand side
# so the user can see attention / running / idle counts without
# detaching back to the TUI. The command runs every 5 seconds
# and exits silently on error so the status bar never garbles.
set -g status-right '#(mosaic statusline 2>/dev/null)'
set -g status-interval 2
set -g status-style 'bg=default,fg=default'
set -g status-right-length 60

# Source the user's override file if it exists. Errors are ignored
# so a missing file never breaks startup.
source-file -q ~/.mosaic/tmux-overlay.conf
`

// EnsureConfigFile writes the mosaic-managed tmux config to path,
// creating the parent directory if needed. It is idempotent and
// overwrites the file on every call so the authoritative version is
// whatever the currently running mosaic binary thinks it should be.
func EnsureConfigFile(path string, opts ConfigOptions) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(DefaultConfigBody(opts)), 0o644)
}
