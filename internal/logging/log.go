// Package logging configures mosaic's structured logger.
//
// mosaic uses log/slog for structured logging. There is one logger
// shared by every subsystem; subsystems should call slog.With(...) to
// derive named child loggers (e.g. slog.With("subsys", "tmux")).
//
// Currently the only sink is a JSON file under
// $DATA_DIR/logs/mosaic.log. Log rotation, debug overlays, and CLI
// flags for log filtering may be added in a future release.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Setup configures the global slog logger to write JSON records to the
// given file path. The directory containing the file is created if it
// does not already exist. Setup returns the io.Closer that owns the
// file handle so callers can defer Close.
//
// Pass an empty path to log to stderr instead of a file.
func Setup(path, level string) (io.Closer, error) {
	var w io.Writer = os.Stderr
	var closer io.Closer = noopCloser{}

	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create log dir: %w", err)
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		w = f
		closer = f
	}

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	slog.SetDefault(slog.New(handler))
	return closer, nil
}

// parseLevel converts a config string to a slog.Level. Unknown values
// fall back to info.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }
