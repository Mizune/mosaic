package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// SetGlobalEnv implements [Backend.SetGlobalEnv] with
// `set-environment -g name value`; an empty value unsets the variable
// (`-gu`). The global environment is what new panes and popups inherit,
// which is how the TUI tells a popup about the tmux it runs under.
func (c *CLI) SetGlobalEnv(ctx context.Context, name, value string) error {
	if name == "" {
		return errors.New("tmux: environment variable name is required")
	}
	args := []string{"set-environment", "-g"}
	if value == "" {
		args = append(args, "-u", name)
	} else {
		args = append(args, name, value)
	}
	_, err := c.run(ctx, args...)
	return err
}

// DisplayMessage implements [Backend.DisplayMessage] with
// `display-message -d <ms> -t <target> <text>`. A target that no longer
// exists is not an error: the message was for whoever is attached
// there, and nobody is.
func (c *CLI) DisplayMessage(ctx context.Context, target, text string, delay time.Duration) error {
	if target == "" {
		return errors.New("tmux: display-message target is required")
	}
	args := []string{"display-message"}
	if delay > 0 {
		args = append(args, "-d", strconv.Itoa(int(delay/time.Millisecond)))
	}
	args = append(args, "-t", target, text)
	_, err := c.run(ctx, args...)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// SessionNameAt returns the name of the session that target (a pane,
// window or "$N" session id) belongs to on the server behind socket.
// It is how a process inside a pane finds out which mosaic session it
// lives in when no environment variable says so: the socket comes from
// that process's own $TMUX, so the answer is about the server that
// actually hosts it, whatever socket this CLI is configured for.
func (c *CLI) SessionNameAt(ctx context.Context, socket, target string) (string, error) {
	if socket == "" || target == "" {
		return "", errors.New("tmux: socket and target are required")
	}
	cmd := exec.CommandContext(ctx, c.binary(), "-S", socket, "display-message", "-p", "-t", target, "#S")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux display-message: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
