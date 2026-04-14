// Package config defines mosaic's user-facing configuration types and
// the rules for merging the global config with project-local overrides.
//
// Configuration sources, in precedence order:
//
//  1. Built-in defaults (see [Default]).
//  2. Global config file (~/.config/mosaic/config.toml or
//     $XDG_CONFIG_HOME/mosaic/config.toml).
//  3. Project config file (<project>/.mosaic/config.toml).
//
// Use [Load] to read both files and merge them, or [Merge] directly when
// you already have parsed [Config] values.
package config

// Config is the top-level configuration type that maps to mosaic's
// TOML schema.
type Config struct {
	Mosaic   MosaicSection   `toml:"mosaic"`
	UI       UISection       `toml:"ui"`
	Tmux     TmuxSection     `toml:"tmux"`
	Worktree WorktreeSection `toml:"worktree"`
	Hooks    HooksSection    `toml:"hooks"`

	// Discord holds the [discord] table that controls the optional
	// Discord bridge. When Enabled is true and a bot token is
	// available, mosaic opens a WebSocket to Discord and streams
	// session events into per-session threads.
	Discord DiscordSection `toml:"discord"`

	// Projects is a user-maintained registry of named project
	// directories. The TUI wizard and the new-session CLI both
	// consult this list so users do not have to type (or even
	// remember) the absolute path of every repository they work in.
	//
	// Example config.toml entry:
	//
	//	[[project]]
	//	name = "orchard"
	//	path = "~/Projects/orchard"
	//	default_tool = "claude"
	//	default_setup = "fast"
	//
	// mosaic automatically learns about new projects as sessions
	// are created, but the registry is the authoritative list the
	// wizard's Project step shows.
	Projects []ProjectEntry `toml:"project"`
}

// ProjectEntry is one registered project in the [[project]] array.
type ProjectEntry struct {
	// Name is the short label the TUI and wizard show the user.
	Name string `toml:"name"`

	// Path is the absolute (or ~-relative) path to the project
	// directory. mosaic expands "~" at load time.
	Path string `toml:"path"`

	// DefaultTool overrides [mosaic] default_tool for this project.
	DefaultTool string `toml:"default_tool"`

	// DefaultSetup overrides [worktree] default_setup for this
	// project.
	DefaultSetup string `toml:"default_setup"`

	// DefaultBranch overrides [worktree] default_base_branch for
	// this project.
	DefaultBranch string `toml:"default_branch"`
}

// MosaicSection holds the [mosaic] table.
type MosaicSection struct {
	Profile     string `toml:"profile"`
	DataDir     string `toml:"data_dir"`
	LogLevel    string `toml:"log_level"`
	DefaultTool string `toml:"default_tool"`

	// DisableUpdateCheck suppresses the background version check
	// that runs when the TUI starts. Set to true in config.toml
	// to opt out of update notifications entirely.
	DisableUpdateCheck bool `toml:"disable_update_check"`

	// Tools holds per-tool launch flag overrides. The map key
	// is the tool name (claude / codex / gemini / shell) and
	// the value is a flag set that mosaic appends after the
	// base launcher.
	Tools map[string]ToolLaunchFlags `toml:"tools"`
}

// ToolLaunchFlags configures the optional flags mosaic passes
// when it spawns an agent's CLI. Unset / false fields mean
// "use the CLI's defaults"; the section is intentionally
// minimal so each new flag is an explicit opt-in.
type ToolLaunchFlags struct {
	// BypassPermissions adds `--dangerously-skip-permissions`
	// to a Claude Code launch so the agent does not pause on
	// every permission prompt. Use with caution — the flag
	// trusts whatever the agent decides to run.
	BypassPermissions bool `toml:"bypass_permissions"`

	// AutoMode adds `--auto` (Claude Code's autonomous mode)
	// so the agent works without waiting for the user to
	// press Enter between turns.
	AutoMode bool `toml:"auto_mode"`

	// AutoContinue controls whether new sessions automatically
	// add `--continue` when a prior Claude transcript exists
	// in the worktree. Default false: new sessions start
	// fresh. Re-attaching to a dead session always uses
	// `--continue` regardless of this flag.
	AutoContinue bool `toml:"auto_continue"`

	// ExtraArgs is a free-form list of additional CLI flags
	// appended after the standard ones. Use this for things
	// mosaic does not yet model (e.g. `--model opus-4`).
	ExtraArgs []string `toml:"extra_args"`
}

// UISection holds the [ui] table.
type UISection struct {
	Theme              string            `toml:"theme"`
	PreviewRefreshMs   int               `toml:"preview_refresh_ms"`
	ListWidthRatio     float64           `toml:"list_width_ratio"`
	AutoExpandSubAgent bool              `toml:"auto_expand_subagents"`
	Keymap             map[string]string `toml:"keymap"`

	// Attention controls how new attention events are surfaced
	// beyond the TUI. The zero value disables every channel so
	// users who prefer a silent UI get one by default.
	Attention AttentionSection `toml:"attention"`
}

// AttentionSection holds [ui.attention] settings.
type AttentionSection struct {
	// NotifyOS emits a native OS notification (macOS NSUser-
	// Notification via osascript, Linux notify-send) whenever a
	// fresh attention is detected.
	NotifyOS bool `toml:"notify_os"`

	// Sound plays an audible cue on new attention events.
	// On macOS the system alert sound is used by default;
	// on Linux the terminal bell is used as a fallback.
	Sound bool `toml:"sound"`

	// SoundFile overrides the default sound with a custom audio
	// file (e.g. "/System/Library/Sounds/Glass.aiff"). When set,
	// afplay (macOS) or paplay (Linux) is used to play it.
	SoundFile string `toml:"sound_file"`
}

// TmuxSection holds the [tmux] table.
type TmuxSection struct {
	SocketName    string `toml:"socket_name"`
	SessionPrefix string `toml:"session_prefix"`
}

// WorktreeSection holds the [worktree] table.
type WorktreeSection struct {
	Root              string                  `toml:"root"`
	DefaultBaseBranch string                  `toml:"default_base_branch"`
	AutoWorktree      bool                    `toml:"auto_worktree"`
	DefaultSetup      string                  `toml:"default_setup"`
	Setup             map[string]WorktreeSetup `toml:"setup"`
}

// WorktreeSetup describes a single named worktree setup recipe.
type WorktreeSetup struct {
	// Copy is the list of files to copy from the source repo into the
	// worktree. Project configs replace the global list when set.
	Copy []string `toml:"copy"`

	// CopyExtra is appended to whatever Copy resolves to. Use this to
	// extend a parent setup without restating its file list.
	CopyExtra []string `toml:"copy_extra"`

	// CopyIgnored copies .gitignore'd files (e.g. .env, build caches)
	// in addition to the explicit Copy list.
	CopyIgnored bool `toml:"copy_ignored"`

	// CopyUntracked copies untracked files in addition to the explicit
	// Copy list.
	CopyUntracked bool `toml:"copy_untracked"`

	// Commands run sequentially after files are copied. Project configs
	// replace the global list when set.
	Commands []string `toml:"commands"`

	// CommandsExtra is appended to whatever Commands resolves to.
	CommandsExtra []string `toml:"commands_extra"`

	// PostCreateHook is the path (relative to the project) to a script
	// run after Commands. Empty disables the hook.
	PostCreateHook string `toml:"post_create_hook"`

	// Env is a set of environment variables exported when running
	// Commands and the post-create hook. Maps merge key-by-key.
	Env map[string]string `toml:"env"`
}

// HooksSection holds the [hooks] table that controls Claude Code hook
// integration.
type HooksSection struct {
	// Suggested controls whether the first-run wizard offers to install
	// hooks. Once the user has answered, this is set to false.
	Suggested bool `toml:"suggested"`

	// Events lists the Claude Code hook events to install handlers for.
	Events []string `toml:"events"`
}

// DiscordSection holds the [discord] table. The bridge is disabled by
// default; users opt in by setting enabled = true and providing a bot
// token (preferably via the MOSAIC_DISCORD_TOKEN env var).
type DiscordSection struct {
	// Enabled gates the entire Discord bridge.
	Enabled bool `toml:"enabled"`

	// Token is the Discord bot token. Users should prefer the
	// MOSAIC_DISCORD_TOKEN env var over storing the token in
	// plaintext TOML.
	Token string `toml:"token"`

	// ChannelID is the parent Discord channel where the bridge
	// creates per-session threads and posts notifications.
	ChannelID string `toml:"channel_id"`

	// GuildID is the Discord guild (server) to register slash
	// commands in. When empty, commands register globally (slower
	// propagation, ~1 hour vs instant).
	GuildID string `toml:"guild_id"`

	// NotifyOnStop sends a message to the session thread when a
	// session stops.
	NotifyOnStop bool `toml:"notify_on_stop"`

	// NotifyOnAttention sends a message to the session thread when
	// a blocking notification (permission prompt, approval request)
	// is detected.
	NotifyOnAttention bool `toml:"notify_on_attention"`

	// AllowedUserIDs restricts destructive slash commands (kill,
	// send) to the listed Discord user IDs. When empty, all guild
	// members are allowed.
	AllowedUserIDs []string `toml:"allowed_user_ids"`
}

// Default returns the built-in defaults applied before any user config
// is layered on. Every Config value passed through [Load] starts from
// these defaults.
func Default() Config {
	return Config{
		Mosaic: MosaicSection{
			Profile:     "default",
			LogLevel:    "info",
			DefaultTool: "claude",
		},
		UI: UISection{
			Theme:            "catppuccin-mocha",
			PreviewRefreshMs: 250,
			ListWidthRatio:   0.35,
		},
		Tmux: TmuxSection{
			SocketName:    "mosaic",
			SessionPrefix: "mos_",
		},
		Worktree: WorktreeSection{
			DefaultBaseBranch: "main",
			AutoWorktree:      false,
			DefaultSetup:      "default",
			Setup: map[string]WorktreeSetup{
				"default": {
					Copy:     []string{".env", ".env.local", ".vscode/", ".cursor/"},
					Commands: []string{},
				},
			},
		},
		Hooks: HooksSection{
			Suggested: true,
			Events: []string{
				"SessionStart",
				"UserPromptSubmit",
				"Stop",
				"PostToolUseFailure",
				"SessionEnd",
				"Notification",
			},
		},
	}
}
