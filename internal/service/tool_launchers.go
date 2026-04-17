package service

import (
	"os"
	"strings"

	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/store"
)

// LaunchCommandForTool returns the shell command mosaic should run
// in the initial tmux pane so the requested agent starts
// automatically. Returning an empty string means "do not run
// anything" — tmux then spawns the user's login shell and the user
// can launch the agent by hand.
//
// The mapping is deliberately minimal: mosaic assumes the agent's
// CLI is already on $PATH. Users who want a custom invocation
// (e.g. `claude --dangerously-skip-permissions --resume`) can
// override it via `mosaic new --cmd "..."` or by setting
// [config.MosaicSection.ToolCommand] overrides in a future polish
// pass.
func LaunchCommandForTool(tool store.SessionTool) string {
	switch tool {
	case store.ToolClaude:
		return "claude"
	case store.ToolCodex:
		return "codex"
	case store.ToolGemini:
		return "gemini"
	case store.ToolShell, "":
		return ""
	default:
		// Unknown tools fall back to running the tool name as the
		// command, so a user who adds `tool = "aider"` to their
		// config still gets a reasonable default.
		return string(tool)
	}
}

// LaunchCommandForSession returns the launch command for the tool,
// specialised for a specific worktree. When the tool is Claude
// Code AND the worktree already has at least one historical
// transcript under ~/.claude/projects/<sanitized>/, the command
// adds `--continue` so the agent resumes the most recent session
// instead of spawning a fresh one.
//
// This is the right behaviour for mosaic sessions that were
// killed and re-attached to, because the user's expectation
// is "pick up where I left off", not "start over".
//
// For tools other than Claude Code — or when there is no
// existing transcript for this worktree — the call is identical
// to [LaunchCommandForTool].
//
// User-supplied flags from config.toml's [mosaic.tools.<name>]
// table are appended after the standard ones; mosaic never
// silently activates --dangerously-skip-permissions, so the
// user must opt in via that table.
// LaunchCommandForSession builds the tool's launch command line.
//
// The `respawn` parameter distinguishes the two call sites:
//
//   - respawn=false (Create path): a brand-new mosaic session.
//     `--continue` is ONLY added when the user opted in via
//     config `auto_continue = true`. Default is a fresh start.
//   - respawn=true  (AttachCommand path): re-attaching to a
//     session whose tmux pane died. `--continue` is ALWAYS
//     added (when a transcript exists) because the user's
//     intent is "pick up where I left off".
func LaunchCommandForSession(tool store.SessionTool, worktreePath string, flags config.ToolLaunchFlags, respawn bool) string {
	argv := LaunchArgvForSession(tool, worktreePath, flags, respawn)
	return strings.Join(argv, " ")
}

// LaunchArgvForSession is the argv equivalent of
// [LaunchCommandForSession]. Call this (instead of the string
// variant) when the resulting argv is going to be wrapped by
// another command — e.g. `docker run … claude --continue`. Emitting
// a pre-split argv avoids a needless shell re-parse that would
// mangle binds and quoted args later in the pipeline.
//
// Returns an empty slice for the shell tool, matching the string
// variant's empty-string contract.
func LaunchArgvForSession(tool store.SessionTool, worktreePath string, flags config.ToolLaunchFlags, respawn bool) []string {
	base := LaunchCommandForTool(tool)
	if base == "" {
		return nil
	}

	parts := []string{base}
	if tool == store.ToolClaude && worktreePath != "" && worktreeHasClaudeTranscript(worktreePath) {
		// Respawn always continues; Create only if configured.
		if respawn || flags.AutoContinue {
			parts = append(parts, "--continue")
		}
	}
	parts = append(parts, claudeFlagArgs(tool, flags)...)
	if len(flags.ExtraArgs) > 0 {
		parts = append(parts, flags.ExtraArgs...)
	}
	return parts
}

// claudeFlagArgs maps the typed config knobs to actual Claude
// Code CLI flags. Other tools currently expose no flag mapping
// — their entries in [mosaic.tools] still flow through
// ExtraArgs but the typed booleans are Claude-specific.
func claudeFlagArgs(tool store.SessionTool, flags config.ToolLaunchFlags) []string {
	if tool != store.ToolClaude {
		return nil
	}
	var out []string
	if flags.BypassPermissions {
		out = append(out, "--dangerously-skip-permissions")
	}
	if flags.AutoMode {
		// Claude Code's autonomous flag (`--auto`) keeps the
		// agent running without pausing for the user between
		// turns. The exact flag name varies between releases;
		// users on older Claude builds can swap to the right
		// one via ExtraArgs.
		out = append(out, "--auto")
	}
	return out
}

// worktreeHasClaudeTranscript reports whether the Claude Code
// projects directory for this worktree contains at least one
// .jsonl file. We do not parse the files — the presence of any
// transcript is enough to assume `claude --continue` will find
// something to resume.
func worktreeHasClaudeTranscript(worktreePath string) bool {
	dir := claudeio.ProjectDir("", worktreePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			return true
		}
	}
	return false
}
