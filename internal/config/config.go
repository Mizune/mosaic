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
	Live     LiveSection     `toml:"live"`
	Tmux     TmuxSection     `toml:"tmux"`
	Worktree WorktreeSection `toml:"worktree"`
	Hooks    HooksSection    `toml:"hooks"`

	// Docker holds the [docker] table that defines defaults for
	// Docker-backed session sandboxes. The block only defines
	// defaults — sandboxing is never enabled by the presence of
	// this block alone. Users opt in per-session via the wizard,
	// the --sandbox CLI flag, or the per-project `sandbox` field.
	Docker DockerSection `toml:"docker"`

	// Discord holds the [discord] table that controls the optional
	// Discord bridge. When Enabled is true and a bot token is
	// available, mosaic opens a WebSocket to Discord and streams
	// session events into per-session threads.
	Discord DiscordSection `toml:"discord"`

	// Viewer holds the [viewer] table that controls the artifact
	// viewer: how Mermaid diagrams, images and HTML files an agent
	// produced are opened (a cmux browser pane inside cmux, the OS
	// default browser elsewhere).
	Viewer ViewerSection `toml:"viewer"`

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

	// Sandbox selects the isolation mode for sessions launched in
	// this project. Accepted values: "none" (default, agent runs
	// on the host) and "docker" (agent runs inside a Docker
	// container built from [docker] defaults). An unknown or empty
	// value behaves like "none" so a typo never silently enables
	// sandboxing.
	Sandbox string `toml:"sandbox"`
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

	// Codex-specific launch flags. They map to current Codex CLI
	// options and are ignored by other tools.
	Model              string `toml:"model"`
	Profile            string `toml:"profile"`
	ProfileV2          string `toml:"profile_v2"`
	Sandbox            string `toml:"sandbox"`
	ApprovalPolicy     string `toml:"approval_policy"`
	LocalProvider      string `toml:"local_provider"`
	WebSearch          bool   `toml:"web_search"`
	OSS                bool   `toml:"oss"`
	NoAltScreen        bool   `toml:"no_alt_screen"`
	BypassCodexSandbox bool   `toml:"bypass_approvals_and_sandbox"`
}

// UISection holds the [ui] table.
type UISection struct {
	Theme              string  `toml:"theme"`
	PreviewRefreshMs   int     `toml:"preview_refresh_ms"`
	ListWidthRatio     float64 `toml:"list_width_ratio"`
	AutoExpandSubAgent bool    `toml:"auto_expand_subagents"`
	// CompletedSubagentRetentionSecs controls how long a completed
	// or errored sub-agent row stays visible in the TUI before it
	// is hidden. Positive: hide after N seconds (default 30).
	// Negative: show forever. Users wanting "hide immediately"
	// should set 1 — the 0-means-inherit convention shared with
	// every other int in this struct prevents an explicit 0 in an
	// override from reaching the final merged config.
	CompletedSubagentRetentionSecs int               `toml:"completed_subagent_retention_secs"`
	Keymap                         map[string]string `toml:"keymap"`

	// Attention controls how new attention events are surfaced
	// beyond the TUI. The zero value disables every channel so
	// users who prefer a silent UI get one by default.
	Attention AttentionSection `toml:"attention"`
}

// LiveSection holds the [live] table that tunes the background
// reconciler which drives sub-agent lifecycle transitions.
// It lives next to [ui] so the UI-layer retention knob
// (ui.completed_subagent_retention_secs) can be reasoned about
// together with the reconciler-layer staleness window.
type LiveSection struct {
	// SubagentStalenessSecs is the silence window after which a
	// running sub-agent with no pending tools and no new transcript
	// activity is assumed to have finished. The reconciler's
	// periodic sweep uses this threshold. 0 means "keep the
	// default (10s)" — matching the 0-means-inherit convention
	// used across this config.
	//
	// To avoid a terminal row flickering off-screen, keep
	// ui.completed_subagent_retention_secs >= this value.
	SubagentStalenessSecs int `toml:"subagent_staleness_secs"`
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
	Root              string                   `toml:"root"`
	DefaultBaseBranch string                   `toml:"default_base_branch"`
	AutoWorktree      bool                     `toml:"auto_worktree"`
	AutoCleanup       *bool                    `toml:"auto_cleanup"`
	DefaultSetup      string                   `toml:"default_setup"`
	Setup             map[string]WorktreeSetup `toml:"setup"`
}

// WorktreeSetup describes a single named worktree setup recipe.
//
// Every field is tagged `omitempty` so configwriter-emitted fragments
// only list the knobs the user actually set, keeping the resulting
// TOML compact and readable like something a human would write.
//
// This aligns with merge semantics defined in internal/config/merge.go:
// zero/empty override values are treated as "inherit from base" (merge
// only replaces when the override is non-zero / non-empty), so emitting
// them as omitted is equivalent to emitting them as explicit empties.
// Concretely, a user-written `copy = []` does NOT round-trip through
// decode-marshal — it comes back omitted — but because merge already
// ignores an empty override slice, there is no observable behavior
// change.
//
// "Explicit empty override" is therefore not a supported pattern. Use
// CopyExtra / CommandsExtra when you want to extend the parent setup,
// or set Copy / Commands to a concrete replacement list.
type WorktreeSetup struct {
	// Copy is the list of files to copy from the source repo into the
	// worktree. Project configs replace the global list when set.
	Copy []string `toml:"copy,omitempty"`

	// CopyExtra is appended to whatever Copy resolves to. Use this to
	// extend a parent setup without restating its file list.
	CopyExtra []string `toml:"copy_extra,omitempty"`

	// CopyIgnored copies .gitignore'd files (e.g. .env, build caches)
	// in addition to the explicit Copy list.
	CopyIgnored bool `toml:"copy_ignored,omitempty"`

	// CopyUntracked copies untracked files in addition to the explicit
	// Copy list.
	CopyUntracked bool `toml:"copy_untracked,omitempty"`

	// Commands run sequentially after files are copied. Project configs
	// replace the global list when set.
	Commands []string `toml:"commands,omitempty"`

	// CommandsExtra is appended to whatever Commands resolves to.
	CommandsExtra []string `toml:"commands_extra,omitempty"`

	// PostCreateHook is the path (relative to the project) to a script
	// run after Commands. Empty disables the hook.
	PostCreateHook string `toml:"post_create_hook,omitempty"`

	// Env is a set of environment variables exported when running
	// Commands and the post-create hook. Maps merge key-by-key.
	Env map[string]string `toml:"env,omitempty"`
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

// DockerSection holds the [docker] table that configures the Docker
// session sandbox. Field names deliberately mirror agent-deck's
// config keys so users migrating from agent-deck get a familiar
// surface.
//
// Sandboxing is never auto-enabled by the mere presence of this
// table. The fields below define defaults that take effect only
// when the user asks for a Docker-backed session (via `mosaic new
// --sandbox=docker`, the wizard, or `sandbox = "docker"` on a
// [[project]] entry).
type DockerSection struct {
	// Image is the reference (`repo:tag` or `repo@digest`) passed
	// to `docker run`. Users are expected to supply this — mosaic
	// ships a reference Dockerfile under docker/sandbox/ but does
	// not auto-pull from a registry.
	Image string `toml:"image"`

	// ExtraArgs is a free-form list of flags appended to the
	// `docker run` command after the standard `-v` / `-e` set and
	// before the image reference. Good fit for resource limits
	// (`--cpus=2`, `--memory=4g`) or `--network=…`.
	ExtraArgs []string `toml:"extra_args"`

	// DefaultMounts is a list of user-defined bind mounts, one
	// string per mount using the docker `host:container[:ro]`
	// syntax. They are appended after the auth mounts (MountClaude
	// / MountGh / MountSSH). Host paths that do not exist at
	// launch time are silently dropped so a mount missing on one
	// developer's machine does not break everyone else's.
	DefaultMounts []string `toml:"default_mounts"`

	// MountClaude bind-mounts ~/.claude into the container
	// read-only so Claude Code's authentication tokens flow
	// through without re-login. Defaults to true.
	//
	// Nullable (*bool) so TOML can distinguish "key absent"
	// (inherit the default) from "explicit false" (opt out).
	// Read via [BoolOr] so call sites carry the default next
	// to the consumption site.
	MountClaude *bool `toml:"mount_claude"`

	// MountCodex bind-mounts ~/.codex into the container read-only
	// so Codex authentication, config, and history are available in
	// Docker-backed sessions. Defaults to true.
	MountCodex *bool `toml:"mount_codex"`

	// MountGh bind-mounts ~/.config/gh read-only so `gh` and
	// session hooks that shell out to gh (PR creation, etc.) work
	// without a fresh `gh auth login`. Defaults to true.
	MountGh *bool `toml:"mount_gh"`

	// MountSSH bind-mounts ~/.ssh read-only so the agent can push
	// to git remotes that require key auth. Defaults to true.
	MountSSH *bool `toml:"mount_ssh"`

	// AutoCleanup controls whether the service issues a best-
	// effort `docker rm -f mosaic-<id>` when a session stops or
	// is deleted. The container is also launched with `--rm`, so
	// AutoCleanup matters mostly when the container is wedged or
	// tmux hands back control before the agent exits. Defaults
	// to true.
	AutoCleanup *bool `toml:"auto_cleanup"`

	// FirewallAllowlist is forwarded to the reference image's
	// init-firewall.sh via the MOSAIC_FW_ALLOWLIST env var as a
	// comma-separated list. Hosts (or CIDRs) on the list are the
	// only outbound destinations the container can reach; every
	// other connection is dropped. The list augments the small
	// built-in set hardcoded in the init script.
	FirewallAllowlist []string `toml:"firewall_allowlist"`
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

// DefaultMermaidJSURL is the Mermaid build generated diagram pages
// load when [viewer] mermaid_js_url is not set. It must be the classic
// (UMD) bundle: file:// pages cannot import ES modules in WebKit.
const DefaultMermaidJSURL = "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"

// ViewerSection holds the [viewer] table that configures the artifact
// viewer. Inside cmux (CMUX_SOCKET_PATH set and the cmux CLI on PATH)
// artifacts open in a cmux browser pane; elsewhere they open in the OS
// default browser via `open` / `xdg-open`.
type ViewerSection struct {
	// Enabled gates the whole feature. When false the artifact key
	// only shows a "viewer disabled" toast. Nullable (*bool) so a
	// project file can set `enabled = false` without the TOML decoder
	// confusing it with "key absent"; read via [BoolOr] with a true
	// fallback.
	Enabled *bool `toml:"enabled"`

	// AutoOpen opens every newly detected artifact without a key
	// press. Artifacts replayed from a transcript at startup never
	// auto-open. Defaults to false.
	AutoOpen bool `toml:"auto_open"`

	// MermaidJSURL is the script URL generated Mermaid pages load.
	// Point it at a local file:// copy of mermaid.min.js (the UMD
	// build, not the ESM one) to render diagrams offline. Defaults to
	// [DefaultMermaidJSURL].
	MermaidJSURL string `toml:"mermaid_js_url"`

	// Placement is where cmux opens the browser pane: "dock" (the
	// right-hand sidebar Dock, default), "right" or "down" (a split
	// of the current workspace). Unknown values behave like "dock".
	// Ignored outside cmux.
	Placement string `toml:"placement"`

	// Focus moves keyboard focus to the browser pane when it opens.
	// Defaults to false so the TUI keeps the keyboard.
	Focus bool `toml:"focus"`

	// Inline controls the Kitty-graphics preview inside the artifact
	// picker: "auto" (default) enables it when the terminal advertises
	// Kitty graphics (Ghostty, kitty, WezTerm) and, inside tmux,
	// allow-passthrough is on; "on" skips the terminal check; "off"
	// disables the preview.
	Inline string `toml:"inline"`

	// ChromePath overrides the Chrome / Chromium binary that rasterizes
	// Mermaid, HTML, SVG and URL artifacts for the inline preview.
	// Empty means auto-detect.
	ChromePath string `toml:"chrome_path"`

	// TmuxViewer selects how prefix+O opens the artifact viewer inside
	// an attached session: "popup" (tmux display-popup, default) or
	// "split" (a right-hand split pane).
	TmuxViewer string `toml:"tmux_viewer"`

	// NotifyTmux shows a short tmux status-line message in the session
	// when a new artifact is detected while you are attached. Nullable
	// like Enabled; defaults to true.
	NotifyTmux *bool `toml:"notify_tmux"`
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
			Theme:                          "catppuccin-mocha",
			PreviewRefreshMs:               250,
			ListWidthRatio:                 0.35,
			CompletedSubagentRetentionSecs: 30,
		},
		Live: LiveSection{
			SubagentStalenessSecs: 10,
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
		Docker: DockerSection{
			// Sensible secure-by-default auth passthrough so
			// opting into Docker mode does not also require
			// re-authenticating every tool inside the container.
			// Stored as *bool with an explicit true so a project
			// override can set e.g. mount_ssh = false to opt out
			// without also having to pin every other field.
			MountClaude: BoolPtr(true),
			MountCodex:  BoolPtr(true),
			MountGh:     BoolPtr(true),
			MountSSH:    BoolPtr(true),
			AutoCleanup: BoolPtr(true),
		},
		Viewer: ViewerSection{
			// Enabled is a *bool for the same reason as the
			// [docker] toggles: a project override can turn the
			// viewer off without restating every other key.
			Enabled:      BoolPtr(true),
			MermaidJSURL: DefaultMermaidJSURL,
			Placement:    "dock",
			Inline:       "auto",
			TmuxViewer:   "popup",
			NotifyTmux:   BoolPtr(true),
		},
	}
}
