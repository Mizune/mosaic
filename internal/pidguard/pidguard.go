// Package pidguard provides a simple PID-file-based guard that
// prevents multiple mosaic TUI instances from running simultaneously.
// It uses advisory file locking (syscall.Flock) to avoid TOCTOU races
// between concurrent starters.
package pidguard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// ErrAlreadyRunning is returned when another mosaic instance owns the
// PID file and is still alive.
var ErrAlreadyRunning = errors.New("mosaic is already running")

// Acquire creates a PID file at path after verifying no live mosaic
// process already owns it. An advisory file lock (Flock) is held for
// the lifetime of the guard to prevent TOCTOU races between concurrent
// starters. On success it returns a release function that unlocks and
// removes the file; callers should defer it (typically via
// runtime.Close).
//
// If the file exists but the recorded process is dead, the stale file
// is replaced.
func Acquire(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open pid file: %w", err)
	}

	// Try a non-blocking exclusive lock. If another live mosaic holds
	// it, we fail immediately instead of blocking.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		// Could not lock — read the PID for the error message.
		if existing, readErr := readPID(path); readErr == nil {
			return nil, fmt.Errorf("%w (pid %d)", ErrAlreadyRunning, existing)
		}
		return nil, fmt.Errorf("%w", ErrAlreadyRunning)
	}

	// We hold the lock. Write our PID.
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("truncate pid file: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("seek pid file: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%d", os.Getpid()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write pid file: %w", err)
	}
	// Flush but keep the file open to retain the lock.
	_ = f.Sync()

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		_ = os.Remove(path)
	}, nil
}

// readPID reads and parses the PID from the given file.
func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// IsStale reports whether a PID file exists but its process is dead.
func IsStale(path string) bool {
	pid, err := readPID(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false
		}
		return true // unreadable → treat as stale
	}
	return !isAlive(pid)
}

// RemoveStale removes the PID file only if the recorded process is
// no longer alive. It is safe to call even when the file does not
// exist.
func RemoveStale(path string) bool {
	if IsStale(path) {
		_ = os.Remove(path)
		return true
	}
	return false
}

// isAlive checks if the given PID corresponds to a running process.
func isAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
