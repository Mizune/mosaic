package viewer

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// runner executes an external command and returns its standard output.
// It exists so Cmux can be exercised with a recording fake; execRunner
// is the only production implementation.
type runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout string, err error)
}

// execRunner runs commands through os/exec with the arguments passed as
// a slice — never through a shell.
type execRunner struct{}

// Run waits for the command to exit so ctx bounds the whole call. On a
// non-zero exit the trimmed stderr is folded into the error so a toast
// can show what cmux complained about.
func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if msg := strings.TrimSpace(string(exitErr.Stderr)); msg != "" {
				return "", fmt.Errorf("%s: %w: %s", describe(name, args), err, msg)
			}
		}
		return "", fmt.Errorf("%s: %w", describe(name, args), err)
	}
	return string(out), nil
}

// describe names a command by its binary and subcommand ("cmux new-pane")
// without echoing the full argument list, which may contain a long URL.
func describe(name string, args []string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + args[0]
}
