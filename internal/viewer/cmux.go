package viewer

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"sync"
)

// Cmux shows artifacts in a cmux browser pane.
//
// The first Open creates the pane with `cmux new-pane --type browser`
// and remembers the surface reference cmux prints; later artifacts
// navigate that same pane with `cmux browser --surface <ref> goto <url>`.
// When the navigation fails — typically because the user closed the
// pane — the reference is dropped and a fresh pane is created. When
// cmux prints no reference at all, every Open creates a new pane; that
// is a documented degradation, not an error.
type Cmux struct {
	run       runner
	placement string
	focus     bool

	mu      sync.Mutex
	surface string
}

// Placement values understood by Cmux. Anything else is treated as the
// dock.
const (
	placementDock  = "dock"
	placementRight = "right"
	placementDown  = "down"
)

func newCmux(run runner, opts Options) *Cmux {
	return &Cmux{
		run:       run,
		placement: normalizePlacement(opts.Placement),
		focus:     opts.Focus,
	}
}

func normalizePlacement(p string) string {
	switch p {
	case placementRight, placementDown:
		return p
	default:
		return placementDock
	}
}

// Name implements Launcher.
func (c *Cmux) Name() string { return "cmux" }

// Open implements Launcher. Calls are serialised so two artifacts opened
// back to back cannot race on the remembered surface.
func (c *Cmux) Open(ctx context.Context, url string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var gotoErr error
	if c.surface != "" {
		_, gotoErr = c.run.Run(ctx, "cmux", "browser", "--surface", c.surface, "goto", url)
		if gotoErr == nil {
			return nil
		}
		c.surface = ""
	}

	out, err := c.run.Run(ctx, "cmux", c.newPaneArgs(url)...)
	if err != nil {
		if gotoErr != nil {
			return fmt.Errorf("viewer: cmux new-pane after goto failed (%v): %w", gotoErr, err)
		}
		return fmt.Errorf("viewer: cmux new-pane: %w", err)
	}
	c.surface = parseSurfaceRef(out)
	return nil
}

// newPaneArgs builds the `cmux new-pane` argument list for url. The dock
// placement splits cmux's right-hand Dock; "right" and "down" split the
// current workspace in that direction instead.
func (c *Cmux) newPaneArgs(url string) []string {
	args := []string{
		"new-pane",
		"--type", "browser",
		"--url", url,
		"--focus", strconv.FormatBool(c.focus),
	}
	switch c.placement {
	case placementRight, placementDown:
		return append(args, "--placement", "workspace", "--direction", c.placement)
	default:
		return append(args, "--placement", placementDock)
	}
}

// surfaceRefPattern matches the short surface reference cmux prints for
// a new pane, e.g. "surface:4". The surrounding text is deliberately
// unconstrained because the exact stdout format is not part of cmux's
// documented contract.
var surfaceRefPattern = regexp.MustCompile(`surface:\d+`)

// parseSurfaceRef extracts the first surface reference from cmux output,
// or "" when there is none.
func parseSurfaceRef(stdout string) string {
	return surfaceRefPattern.FindString(stdout)
}
