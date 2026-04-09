package config

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolvedPath returns the project's Path with a leading "~" expanded
// to $HOME and the result made absolute. The original entry is not
// mutated.
func (p ProjectEntry) ResolvedPath() (string, error) {
	path := strings.TrimSpace(p.Path)
	if path == "" {
		return "", nil
	}
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	return filepath.Abs(path)
}

// FindProjectByName returns the first project whose Name matches the
// argument, or (ProjectEntry{}, false) when no match exists.
func (c Config) FindProjectByName(name string) (ProjectEntry, bool) {
	for _, p := range c.Projects {
		if p.Name == name {
			return p, true
		}
	}
	return ProjectEntry{}, false
}
