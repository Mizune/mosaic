package tmux

import (
	"context"
	"errors"
	"fmt"
)

// SplitDirection identifies how a new pane is positioned relative to
// the focused pane.
type SplitDirection string

const (
	SplitRight SplitDirection = "right"
	SplitDown  SplitDirection = "down"
)

// SplitPaneOptions configures [CLI.SplitPane].
type SplitPaneOptions struct {
	Target     string         // session:window.pane reference
	Direction  SplitDirection // right or down
	WorkingDir string
	Command    string
}

// SplitPane runs `tmux split-window` to open secondary panes for
// sub-agent visibility.
func (c *CLI) SplitPane(ctx context.Context, opts SplitPaneOptions) error {
	if opts.Target == "" {
		return errors.New("tmux: split target is required")
	}
	args := []string{"split-window", "-d"}
	switch opts.Direction {
	case SplitRight, "":
		args = append(args, "-h")
	case SplitDown:
		args = append(args, "-v")
	default:
		return fmt.Errorf("tmux: unknown split direction %q", opts.Direction)
	}
	args = append(args, "-t", opts.Target)
	if opts.WorkingDir != "" {
		args = append(args, "-c", opts.WorkingDir)
	}
	if opts.Command != "" {
		args = append(args, opts.Command)
	}
	_, err := c.run(ctx, args...)
	return err
}

// SendKeys runs `tmux send-keys -t <target> <keys> Enter` (Enter only
// when EnterAfter is true).
func (c *CLI) SendKeys(ctx context.Context, target, keys string, enterAfter bool) error {
	if target == "" {
		return errors.New("tmux: target is required")
	}
	args := []string{"send-keys", "-t", target, keys}
	if enterAfter {
		args = append(args, "Enter")
	}
	_, err := c.run(ctx, args...)
	return err
}
