// Package mosaicpath resolves the on-disk locations mosaic uses for its
// data, configuration, runtime files, and logs.
//
// Resolution order for the data directory:
//  1. $MOSAIC_DATA_DIR if set
//  2. $XDG_DATA_HOME/mosaic if XDG_DATA_HOME is set
//  3. $HOME/.mosaic
//
// Resolution order for the global config directory:
//  1. $MOSAIC_CONFIG_DIR if set
//  2. $XDG_CONFIG_HOME/mosaic if XDG_CONFIG_HOME is set
//  3. $HOME/.config/mosaic
//
// All resolver functions are pure with respect to the environment they
// see at call time. They do not create any directories; use [EnsureAll]
// or the individual Ensure* helpers when you need the directories to
// exist on disk.
package mosaicpath

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths is the materialised set of locations mosaic uses. It is produced
// by [Resolve] and consumed by every subsystem so that paths come from a
// single source of truth.
type Paths struct {
	// DataDir is the root directory for mosaic state.
	DataDir string

	// ConfigDir is the directory containing the global config.toml.
	ConfigDir string

	// GlobalConfigFile is the path to the global config.toml.
	GlobalConfigFile string

	// ProfilesDir holds per-profile data (state.db, hooks/, etc).
	ProfilesDir string

	// LogsDir holds log files.
	LogsDir string

	// LogFile is the path to the main log file.
	LogFile string

	// RunDir holds runtime files (sockets, pid files).
	RunDir string

	// PIDFile is the path to the TUI instance lock file.
	PIDFile string

	// SocketsDir holds long-lived sockets (hook server, IPC).
	SocketsDir string

	// FirstRunMarker indicates first-run wizard completion.
	FirstRunMarker string

	// TmuxSocketName is the value to pass to `tmux -L`.
	TmuxSocketName string

	// TmuxConfigFile is the path to the mosaic-managed tmux config snippet.
	TmuxConfigFile string
}

// Resolve computes the canonical Paths from the current process
// environment and the provided home directory. Pass an empty string for
// home to use os.UserHomeDir.
func Resolve(home string) (Paths, error) {
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve home: %w", err)
		}
		home = h
	}

	dataDir := chooseDataDir(home)
	configDir := chooseConfigDir(home)

	return Paths{
		DataDir:          dataDir,
		ConfigDir:        configDir,
		GlobalConfigFile: filepath.Join(configDir, "config.toml"),
		ProfilesDir:      filepath.Join(dataDir, "profiles"),
		LogsDir:          filepath.Join(dataDir, "logs"),
		LogFile:          filepath.Join(dataDir, "logs", "mosaic.log"),
		RunDir:           filepath.Join(dataDir, "run"),
		PIDFile:          filepath.Join(dataDir, "run", "mosaic.pid"),
		SocketsDir:       filepath.Join(dataDir, "run", "sockets"),
		FirstRunMarker:   filepath.Join(dataDir, ".first-run-done"),
		TmuxSocketName:   "mosaic",
		TmuxConfigFile:   filepath.Join(dataDir, "tmux.conf"),
	}, nil
}

// ProfileDir returns the directory for the named profile.
func (p Paths) ProfileDir(profile string) string {
	if profile == "" {
		profile = "default"
	}
	return filepath.Join(p.ProfilesDir, profile)
}

// StateDB returns the SQLite path for the named profile.
func (p Paths) StateDB(profile string) string {
	return filepath.Join(p.ProfileDir(profile), "state.db")
}

// ProjectConfigFile returns the path to a project-local config.toml
// inside the given project directory.
func ProjectConfigFile(projectDir string) string {
	return filepath.Join(projectDir, ".mosaic", "config.toml")
}

// EnsureAll creates every directory required by mosaic, with mode 0700
// for runtime/sockets directories and 0755 for the rest. It is
// idempotent: missing directories are created, existing ones are left
// alone, and any other error is returned.
func (p Paths) EnsureAll() error {
	type entry struct {
		path string
		mode os.FileMode
	}
	for _, e := range []entry{
		{p.DataDir, 0o755},
		{p.ConfigDir, 0o755},
		{p.ProfilesDir, 0o755},
		{p.LogsDir, 0o755},
		{p.RunDir, 0o700},
		{p.SocketsDir, 0o700},
	} {
		if err := os.MkdirAll(e.path, e.mode); err != nil {
			return fmt.Errorf("mkdir %s: %w", e.path, err)
		}
	}
	return nil
}

// EnsureProfile creates the directory for the named profile.
func (p Paths) EnsureProfile(profile string) error {
	dir := p.ProfileDir(profile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir profile %s: %w", dir, err)
	}
	return nil
}

func chooseDataDir(home string) string {
	if v := os.Getenv("MOSAIC_DATA_DIR"); v != "" {
		return expandHome(v, home)
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "mosaic")
	}
	return filepath.Join(home, ".mosaic")
}

func chooseConfigDir(home string) string {
	if v := os.Getenv("MOSAIC_CONFIG_DIR"); v != "" {
		return expandHome(v, home)
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "mosaic")
	}
	return filepath.Join(home, ".config", "mosaic")
}

// expandHome rewrites a leading "~" to the supplied home directory.
func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if len(p) >= 2 && p[0] == '~' && p[1] == '/' {
		return filepath.Join(home, p[2:])
	}
	return p
}
