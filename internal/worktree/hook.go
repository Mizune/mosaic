package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runCommand executes a single shell command inside the worktree dir
// with the supplied env merged on top of os.Environ().
func runCommand(ctx context.Context, dir, command string, env map[string]string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = mergeEnv(env)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// runHook executes a post-create script. The script is invoked
// directly (not through `sh -c`) so it can have its own shebang.
func runHook(ctx context.Context, dir, script string, env map[string]string) error {
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("hook script not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, script)
	cmd.Dir = dir
	cmd.Env = mergeEnv(env)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// mergeEnv overlays the supplied map on top of the current process
// environment. Existing keys in the process environment are overridden
// by the map; keys present only in the map are appended.
func mergeEnv(env map[string]string) []string {
	base := os.Environ()
	if len(env) == 0 {
		return base
	}
	override := make(map[string]string, len(env))
	for k, v := range env {
		override[k] = v
	}
	out := make([]string, 0, len(base)+len(env))
	for _, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			out = append(out, kv)
			continue
		}
		key := kv[:eq]
		if v, ok := override[key]; ok {
			out = append(out, key+"="+v)
			delete(override, key)
		} else {
			out = append(out, kv)
		}
	}
	for k, v := range override {
		out = append(out, k+"="+v)
	}
	return out
}
