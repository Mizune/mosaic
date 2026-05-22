package sandbox

// Mode selects the isolation backend used for a session.
//
// The zero value is [ModeNone] — callers that forget to set Mode
// get the unsandboxed behaviour, which matches the pre-sandbox
// semantics so upgrades are safe.
type Mode string

const (
	// ModeNone runs the agent directly on the host. This is the
	// default for every session that does not explicitly opt in.
	ModeNone Mode = "none"

	// ModeDocker wraps the agent launch in `docker run ...` so the
	// agent sees only the worktree + explicitly mounted host
	// resources.
	ModeDocker Mode = "docker"
)

// String implements fmt.Stringer and returns the mode token used
// in config files and on the wire.
func (m Mode) String() string {
	if m == "" {
		return string(ModeNone)
	}
	return string(m)
}

// ParseMode accepts the user-facing strings ("none", "docker", or
// empty) and returns the corresponding Mode. Unknown values fall
// back to ModeNone so a typo in config does not silently enable
// sandboxing.
func ParseMode(s string) Mode {
	switch s {
	case string(ModeDocker):
		return ModeDocker
	default:
		return ModeNone
	}
}

// Spec is the resolved sandbox configuration for a single session.
// It carries everything the Docker wrapper needs to build a
// `docker run` argv.
type Spec struct {
	// Mode is the backend selection. ModeNone disables wrapping.
	Mode Mode

	// Image is the reference passed to `docker run`. Required
	// when Mode=ModeDocker.
	Image string

	// Mounts are host→container bind mounts. Entries whose host
	// path does not exist at resolve time are silently dropped so
	// a user who does not have ~/.ssh still gets a working
	// container.
	Mounts []Mount

	// ExtraArgs are appended after the standard `docker run` flags
	// and before the image reference. Use this for resource limits
	// or network mode overrides.
	ExtraArgs []string

	// AutoCleanup instructs SessionService to run `docker rm -f`
	// on the container when the session stops. The container is
	// also launched with `--rm`, so this matters mostly when the
	// container is stuck / hung and tmux hands back control before
	// the agent exits cleanly.
	AutoCleanup bool
}

// Mount describes a single bind mount that the Docker wrapper
// should add to the container.
type Mount struct {
	// Host is the absolute path on the host. Skipped silently when
	// the path does not exist so optional auth mounts do not break
	// the launch on machines where the user never set up that tool.
	Host string

	// Container is the absolute mount target inside the container.
	Container string

	// ReadOnly adds `:ro` to the bind mount spec. Recommended for
	// anything auth-shaped (~/.claude, ~/.ssh, ~/.config/gh).
	ReadOnly bool
}

// ContainerName returns the canonical `docker --name` for a given
// session ULID. Callers that need to run `docker rm` during cleanup
// reuse this helper so the name stays in sync with what Wrap emits.
func ContainerName(sessionID string) string {
	return "mosaic-" + sessionID
}

// Container-side paths for the auth passthrough bind-mounts.
// Hosted here because internal/sandbox owns the full
// container-side filesystem layout (together with /workspace
// inside wrap.go and ContainerName above). Service-layer code
// that composes mounts references these constants so any future
// relocation (different unprivileged user, different HOME) lives
// in one package.
const (
	ClaudeMountTarget = "/home/mosaic/.claude"
	CodexMountTarget  = "/home/mosaic/.codex"
	GhMountTarget     = "/home/mosaic/.config/gh"
	SSHMountTarget    = "/home/mosaic/.ssh"
)
