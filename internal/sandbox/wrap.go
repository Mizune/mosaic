package sandbox

import (
	"os"
	"path/filepath"
	"sort"
)

// Input is the per-session data Wrap needs to produce a final argv.
// It is a distinct struct so callers do not have to build a Spec
// with mutable per-session fields mixed in.
type Input struct {
	// SessionID is the mosaic session ULID. Used to build the
	// container name.
	SessionID string

	// WorkingDir is the host path the agent expects to run in. For
	// Docker mode it is bind-mounted to /workspace; for the
	// unsandboxed mode it is ignored here (tmux sets it separately).
	WorkingDir string

	// Env is the MOSAIC_* environment map that the hook server
	// expects to reach the agent. Wrap forwards it into the
	// container via `-e` and, when MOSAIC_SOCKET_PATH is set,
	// additionally bind-mounts the socket directory so the unix
	// socket is reachable.
	Env map[string]string

	// Argv is the unwrapped agent argv — e.g. ["claude",
	// "--dangerously-skip-permissions"]. Exact slice is returned
	// as-is when Mode=ModeNone.
	Argv []string
}

// Wrap turns the agent argv into the argv tmux should actually
// spawn. For Mode=ModeNone it returns in.Argv unchanged. For
// Mode=ModeDocker it emits a full `docker run ...` invocation.
//
// Wrap never talks to the Docker daemon or checks whether the
// image exists — that is the caller's responsibility (typically
// "docker" is assumed to be on PATH and the image is already
// pulled).
func Wrap(spec Spec, in Input) []string {
	switch spec.Mode {
	case ModeDocker:
		return dockerArgv(spec, in)
	default:
		return in.Argv
	}
}

// dockerArgv builds the full `docker run ...` argv.
//
// Order matters: every `-v` / `-e` comes before the image
// reference, which in turn comes before the trailing command.
// Mounts and env keys are sorted so two Wraps with identical
// input produce byte-identical output — this keeps tests and
// logs reproducible.
func dockerArgv(spec Spec, in Input) []string {
	args := []string{"docker", "run", "--rm", "-i", "--init"}
	args = append(args, "--name", ContainerName(in.SessionID))

	// Workspace bind: the worktree is the only RW mount by default.
	// The agent sees it at /workspace so container-internal paths
	// are stable across machines.
	if in.WorkingDir != "" {
		args = append(args, "-w", "/workspace", "-v", in.WorkingDir+":/workspace")
	}

	// Optional auth / socket mounts. Host paths that do not exist
	// are skipped so a machine without ~/.ssh still launches
	// successfully — the user just loses the corresponding
	// capability inside the container.
	for _, m := range spec.Mounts {
		if m.Host == "" || m.Container == "" {
			continue
		}
		if !hostPathExists(m.Host) {
			continue
		}
		spec := m.Host + ":" + m.Container
		if m.ReadOnly {
			spec += ":ro"
		}
		args = append(args, "-v", spec)
	}

	// Mosaic's hook unix socket lives in a directory on the host.
	// Mount the whole directory (not the socket file) so the hook
	// server can recreate the socket without breaking the mount.
	if sp, ok := in.Env["MOSAIC_SOCKET_PATH"]; ok && sp != "" {
		dir := filepath.Dir(sp)
		if hostPathExists(dir) {
			args = append(args, "-v", dir+":"+dir)
		}
	}

	// Sorted env so identical input → identical output. Matches the
	// convention used in internal/tmux when it emits `-e KEY=VAL`.
	if len(in.Env) > 0 {
		keys := make([]string, 0, len(in.Env))
		for k := range in.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "-e", k+"="+in.Env[k])
		}
	}

	// User-supplied extra args (resource limits, --network=…).
	args = append(args, spec.ExtraArgs...)

	// Image reference is mandatory in Docker mode; callers that
	// forget it get a `docker run` that fails fast.
	args = append(args, spec.Image)

	// The agent argv runs as the final command. Empty Argv (shell
	// tool) falls through to the image's default CMD.
	//
	// The shell-tool contract is "give me my default shell", so
	// delegating to the image's CMD is the right behavior: the
	// reference image declares `CMD ["bash"]` and users who attach
	// land in an interactive bash. Users supplying a custom image
	// with a non-interactive ENTRYPOINT must either (a) pick a
	// different tool (claude/codex/gemini, whose launchers provide
	// an explicit argv) or (b) override CMD in their image so the
	// container exposes an interactive shell when called with no
	// arguments.
	args = append(args, in.Argv...)
	return args
}

// hostPathExists is overridable in tests so mount-skipping can be
// exercised without creating real files on the developer's machine.
var hostPathExists = func(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
