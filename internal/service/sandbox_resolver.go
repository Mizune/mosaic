package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/sandbox"
)

// resolveSandboxSpec turns (CLI input + per-project config +
// global defaults) into the [sandbox.Spec] used by the Docker
// wrapper.
//
// Precedence (highest first):
//  1. in.SandboxMode when the caller set it explicitly.
//  2. The matching [[project]] entry's `sandbox` field.
//  3. [sandbox.ModeNone] (never inferred from a [docker] block
//     alone — having defaults configured is not the same as
//     asking for sandboxing).
//
// Errors are only raised when the selected mode is incompatible
// with the rest of the config (e.g. docker mode with no image).
func (s *SessionService) resolveSandboxSpec(in CreateInput) (sandbox.Spec, error) {
	mode := in.SandboxMode
	if mode == "" {
		mode = projectSandboxMode(s.Cfg.Projects, in.ProjectPath)
	}
	if mode == "" {
		mode = sandbox.ModeNone
	}
	if mode == sandbox.ModeNone {
		return sandbox.Spec{Mode: sandbox.ModeNone}, nil
	}
	if mode != sandbox.ModeDocker {
		return sandbox.Spec{}, fmt.Errorf("sandbox: unsupported mode %q", mode)
	}

	image := strings.TrimSpace(in.SandboxImage)
	if image == "" {
		image = strings.TrimSpace(s.Cfg.Docker.Image)
	}
	if image == "" {
		return sandbox.Spec{}, errors.New("sandbox: docker mode requires an image (set [docker].image in config.toml or pass --sandbox-image)")
	}

	spec := sandbox.Spec{
		Mode:        sandbox.ModeDocker,
		Image:       image,
		ExtraArgs:   append([]string(nil), s.Cfg.Docker.ExtraArgs...),
		AutoCleanup: config.BoolOr(s.Cfg.Docker.AutoCleanup, true),
	}
	spec.Mounts = buildMounts(s.Cfg.Docker)
	return spec, nil
}

// projectSandboxMode looks up the per-project `sandbox` opt-in
// from the config's [[project]] array. Returns an empty string
// when the project path does not match any entry so the caller
// can fall back to the default.
func projectSandboxMode(projects []config.ProjectEntry, projectPath string) sandbox.Mode {
	if projectPath == "" || len(projects) == 0 {
		return ""
	}
	for _, p := range projects {
		resolved, err := p.ResolvedPath()
		if err != nil || resolved == "" {
			continue
		}
		if samePath(resolved, projectPath) {
			return sandbox.ParseMode(p.Sandbox)
		}
	}
	return ""
}

// samePath compares two filesystem paths with symlink resolution
// skipped — we only EvalSymlinks when both paths exist, so
// imported configs with aspirational paths still match after
// Abs+Clean. Falls back to string equality on the normalised
// forms.
func samePath(a, b string) bool {
	ca, _ := filepath.Abs(a)
	cb, _ := filepath.Abs(b)
	return filepath.Clean(ca) == filepath.Clean(cb)
}

// buildMounts translates the [docker] boolean toggles into the
// concrete [sandbox.Mount] list Wrap consumes. Mounts whose host
// path does not exist on this machine still enter the list;
// Wrap itself drops them silently at emit time, which keeps this
// function free of side effects and easy to test.
func buildMounts(d config.DockerSection) []sandbox.Mount {
	home, _ := os.UserHomeDir()
	var mounts []sandbox.Mount
	// Auth mounts default to true; callers who want to opt out
	// write e.g. `mount_ssh = false` in their config.toml, which
	// the *bool field records as "explicit false" rather than
	// indistinguishable-from-unset.
	if config.BoolOr(d.MountClaude, true) && home != "" {
		mounts = append(mounts, sandbox.Mount{
			Host:      filepath.Join(home, ".claude"),
			Container: sandbox.ClaudeMountTarget,
			ReadOnly:  true,
		})
	}
	if config.BoolOr(d.MountCodex, true) && home != "" {
		mounts = append(mounts, sandbox.Mount{
			Host:      filepath.Join(home, ".codex"),
			Container: sandbox.CodexMountTarget,
			ReadOnly:  true,
		})
	}
	if config.BoolOr(d.MountGh, true) && home != "" {
		mounts = append(mounts, sandbox.Mount{
			Host:      filepath.Join(home, ".config", "gh"),
			Container: sandbox.GhMountTarget,
			ReadOnly:  true,
		})
	}
	if config.BoolOr(d.MountSSH, true) && home != "" {
		mounts = append(mounts, sandbox.Mount{
			Host:      filepath.Join(home, ".ssh"),
			Container: sandbox.SSHMountTarget,
			ReadOnly:  true,
		})
	}
	// User-supplied strings use the docker bind spec:
	//   host:container[:ro]
	// We parse leniently: unknown forms are skipped so a typo
	// in config does not abort the launch.
	for _, m := range d.DefaultMounts {
		if parsed, ok := parseBindSpec(m); ok {
			mounts = append(mounts, parsed)
		}
	}
	return mounts
}

// splitCommandString splits a free-form shell command string into
// argv tokens on whitespace. It is intentionally simple: quoting
// and shell metacharacters are NOT honoured. Callers who need
// precise quoting should pass SandboxMode=none (the legacy path
// preserves the single-string Command as-is) or build their own
// argv.
func splitCommandString(cmd string) []string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// joinArgv is the inverse of splitCommandString used on the
// non-sandboxed path to preserve the pre-sandbox wire format
// (a single whitespace-joined string passed to tmux's Command).
func joinArgv(argv []string) string {
	return strings.Join(argv, " ")
}

// removeSandboxContainer issues a best-effort `docker rm -f` for
// a session's container. It swallows every error because the
// operation is a cleanup hint, not a correctness boundary —
// `--rm` on the initial `docker run` already takes care of the
// happy path, and a container that has already exited will not
// be there to remove.
//
// A zero-value container name (non-docker sessions) returns
// without doing anything.
func removeSandboxContainer(name string) {
	if name == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()
}

// parseBindSpec parses "host:container[:ro]" into a Mount. "~"
// in the host path is expanded against $HOME. Malformed entries
// yield ok=false so callers can skip them.
func parseBindSpec(spec string) (sandbox.Mount, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return sandbox.Mount{}, false
	}
	parts := strings.Split(spec, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return sandbox.Mount{}, false
	}
	host := parts[0]
	if strings.HasPrefix(host, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			host = filepath.Join(home, host[2:])
		}
	} else if host == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			host = home
		}
	}
	m := sandbox.Mount{Host: host, Container: parts[1]}
	if len(parts) == 3 {
		m.ReadOnly = parts[2] == "ro"
	}
	return m, true
}
