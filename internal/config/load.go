package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// LoadOptions parameterises [Load].
type LoadOptions struct {
	// GlobalPath is the path to the global config TOML. Pass an empty
	// string to skip loading a global file.
	GlobalPath string

	// ProjectPath is the path to the project-local config TOML. Pass
	// an empty string to skip loading a project file.
	ProjectPath string
}

// LoadResult exposes the merged config plus information about which
// sources were actually consumed. The TUI uses [LoadResult.Sources] to
// render an indicator like `[hook: off]` or `[migrated]` so the user
// always knows which files contributed.
type LoadResult struct {
	// Config is defaults + global + project, fully merged.
	Config Config

	// Sources lists the files that were read, in load order.
	Sources []string
}

// Load reads the global and project configs (if present) and layers
// them on top of the built-in defaults. Missing files are not an error
// — that is the common case before first run. Any other I/O or parse
// error is wrapped and returned.
func Load(opts LoadOptions) (LoadResult, error) {
	cfg := Default()
	var sources []string

	if opts.GlobalPath != "" {
		ok, global, err := loadFile(opts.GlobalPath)
		if err != nil {
			return LoadResult{}, fmt.Errorf("load global config: %w", err)
		}
		if ok {
			cfg = Merge(cfg, global)
			sources = append(sources, opts.GlobalPath)
		}
	}

	if opts.ProjectPath != "" {
		ok, project, err := loadFile(opts.ProjectPath)
		if err != nil {
			return LoadResult{}, fmt.Errorf("load project config: %w", err)
		}
		if ok {
			cfg = Merge(cfg, project)
			sources = append(sources, opts.ProjectPath)
		}
	}

	return LoadResult{Config: cfg, Sources: sources}, nil
}

// loadFile parses one TOML file. The first return value is true when
// the file existed and was decoded; false when the file was missing.
func loadFile(path string) (bool, Config, error) {
	var c Config
	_, err := toml.DecodeFile(path, &c)
	if err == nil {
		return true, c, nil
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
		return false, Config{}, nil
	}
	return false, Config{}, err
}
