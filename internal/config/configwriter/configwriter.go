// Package configwriter persists small, targeted edits back into a
// mosaic TOML config file.
//
// Unlike [config.Load], which layers defaults + global + project for
// runtime use, this package performs *surgical* edits: it reads the
// file as raw bytes, replaces (or appends) a single TOML table block,
// and writes the result back atomically.
//
// Surgical editing means every line OUTSIDE the target block is
// preserved byte-for-byte. That includes blank lines, the user's
// own comments, and unrelated sections. The trade-off is that we
// never re-flow or re-order user content — we only touch the table
// we were asked to touch.
//
// # Why not a full decode / re-encode?
//
// Decoding into [config.Config] and marshaling the whole struct back
// loses every comment in the file (no Go TOML library preserves them
// round-trip today) and emits zero-valued tables for sections the
// user never set. Config files are user-owned artifacts; treating
// them as opaque data regresses the editing UX. The splice-based
// approach in splice.go side-steps both problems.
//
// # Atomic write
//
// We write to `<path>.tmp` and rename onto the final path so readers
// never observe a partial file. Permissions are inherited from the
// existing file when present, and default to [defaultConfigFileMode]
// (0600) for fresh files — recipe Env values may contain secrets.
package configwriter

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Mizune/mosaic/internal/config"
)

// defaultConfigFileMode is the permission applied to a newly created
// config file. It intentionally excludes group/other access because
// recipe Env values may contain secrets.
const defaultConfigFileMode os.FileMode = 0o600

// Section is a single table upsert: the dot-separated TablePath
// (e.g. "worktree.setup.alpha") paired with a TOML-marshalable Value.
// UpsertSections accepts a slice of these so callers can apply
// several edits to one file atomically.
type Section struct {
	TablePath string
	Value     any
}

// UpsertSection replaces (or appends) the `[tablePath]` section in
// the TOML file at path with the TOML encoding of value. Every line
// OUTSIDE the target section — comments, blank lines, and unrelated
// tables — is preserved byte-for-byte.
//
// tablePath must be a dot-separated nested path (e.g. "mosaic" or
// "worktree.setup.alpha"). A single-component path writes a
// top-level table; a multi-component path writes a sub-table and
// its descendants.
//
// value can be any TOML-marshalable type (typically a struct from
// the config package). Fields tagged with `omitempty` are skipped
// when the value is zero, which keeps the emitted section compact.
//
// A missing file is treated as empty — the fragment is written to a
// fresh file after creating the enclosing directory.
func UpsertSection(path, tablePath string, value any) error {
	return UpsertSections(path, []Section{{TablePath: tablePath, Value: value}})
}

// UpsertSections applies multiple section upserts to the file at
// path in a single read-modify-write cycle. Each Section is spliced
// into the in-memory buffer in order; the combined result is written
// once, atomically. This gives callers all-or-nothing semantics:
// either every section lands on disk, or the file is untouched.
//
// An empty sections slice is a no-op. A zero-value TablePath in any
// entry is rejected, matching the single-section UpsertSection.
func UpsertSections(path string, sections []Section) error {
	if path == "" {
		return errors.New("configwriter: path is required")
	}
	if len(sections) == 0 {
		return nil
	}
	for _, s := range sections {
		if s.TablePath == "" {
			return errors.New("configwriter: tablePath is required")
		}
	}

	content, err := readFileOrEmpty(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	for _, s := range sections {
		fragment, err := RenderFragment(s.TablePath, s.Value)
		if err != nil {
			return fmt.Errorf("render section fragment %q: %w", s.TablePath, err)
		}
		content = splicedSection(content, s.TablePath, fragment)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return writeAtomic(path, content)
}

// UpsertWorktreeSetup replaces (or appends) the
// `[worktree.setup.<name>]` section in the TOML file at path with
// the supplied recipe. Every other line in the file is preserved
// verbatim, including comments.
//
// An empty name is rejected: TOML table keys must be non-empty and
// the caller almost certainly made a mistake if they reached here
// with "".
func UpsertWorktreeSetup(path, name string, recipe config.WorktreeSetup) error {
	if name == "" {
		return errors.New("configwriter: recipe name is required")
	}
	return UpsertSection(path, "worktree.setup."+name, recipe)
}

// readFileOrEmpty returns the file contents, or an empty byte slice
// when the file does not exist. Any other error is propagated.
func readFileOrEmpty(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err == nil {
		return content, nil
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return nil, err
}

// writeAtomic writes data to `<path>.tmp` and renames it onto path
// once the write succeeds, so readers never observe a partial file.
//
// Permissions: if path already exists, the tmp file inherits the
// existing file's mode so atomic-write does not widen or narrow
// access. Otherwise the tmp file is created with
// [defaultConfigFileMode] so recipe Env values (potential secrets)
// are not world-readable.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	mode := defaultConfigFileMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	// Remove any leftover tmp from a crashed previous run. OpenFile
	// with O_CREATE|O_TRUNC does NOT reapply `mode` to an existing
	// file, so a stale tmp would leak its old permissions onto the
	// renamed config after the Rename below. Removing first ensures
	// the file is created fresh with the computed mode.
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}
