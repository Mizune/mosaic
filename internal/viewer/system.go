package viewer

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// System opens URLs with the operating system's default browser: `open`
// on macOS and `xdg-open` on Linux. It is the fallback outside cmux.
type System struct{}

// Name implements Launcher.
func (System) Name() string { return "system" }

// Open implements Launcher. It starts the platform opener and returns
// as soon as the process has been spawned; the opener hands the URL to
// the browser and exits on its own. A missing binary surfaces as the
// exec.LookPath error so callers can tell "no opener" from "opener
// failed".
//
// The child is deliberately started with exec.Command rather than
// exec.CommandContext: callers cancel ctx as soon as Open returns
// (`defer cancel()`), and a context-bound child would be killed before
// `open` had finished handing the URL to the browser. ctx is still
// honoured for the spawn itself. The opener is reaped in the background
// so it does not linger as a zombie.
func (System) Open(ctx context.Context, url string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("viewer: %w", err)
	}
	name, err := openerFor(runtime.GOOS)
	if err != nil {
		return err
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("viewer: %w", err)
	}
	cmd := exec.Command(bin, url)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("viewer: start %s: %w", name, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// openerFor names the URL opener for a GOOS value.
func openerFor(goos string) (string, error) {
	switch goos {
	case "darwin":
		return "open", nil
	case "linux":
		return "xdg-open", nil
	default:
		return "", fmt.Errorf("viewer: no system browser opener for %s", goos)
	}
}
