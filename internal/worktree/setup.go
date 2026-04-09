package worktree

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/git"
)

// copyDeclaredFiles applies the Copy / CopyExtra / CopyIgnored /
// CopyUntracked rules from a [config.WorktreeSetup] by copying files
// from src into dst.
//
// The function returns the list of relative paths it copied so the
// caller can render them in a progress UI. Any error short-circuits
// the walk; partial results are still returned.
func copyDeclaredFiles(ctx context.Context, g *git.Client, src, dst string, setup config.WorktreeSetup) ([]string, error) {
	seen := map[string]struct{}{}
	add := func(rel string) {
		seen[filepath.Clean(rel)] = struct{}{}
	}

	for _, p := range setup.Copy {
		add(p)
	}
	for _, p := range setup.CopyExtra {
		add(p)
	}

	if setup.CopyIgnored {
		ignored, err := g.ListIgnored(ctx, src)
		if err != nil {
			return nil, fmt.Errorf("list ignored: %w", err)
		}
		for _, p := range ignored {
			add(p)
		}
	}
	if setup.CopyUntracked {
		untracked, err := g.ListUntracked(ctx, src)
		if err != nil {
			return nil, fmt.Errorf("list untracked: %w", err)
		}
		for _, p := range untracked {
			add(p)
		}
	}

	// Sort the seen set to make the output deterministic, which makes
	// the progress UI easier to follow and the tests stable.
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sortStrings(paths)

	var copied []string
	for _, rel := range paths {
		if rel == "" || rel == "." {
			continue
		}
		// Defence in depth against `..` traversal in user-supplied
		// config: only allow paths that, after Clean, do not start
		// with "..".
		if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return copied, fmt.Errorf("worktree copy: refusing unsafe path %q", rel)
		}
		srcPath := filepath.Join(src, rel)
		dstPath := filepath.Join(dst, rel)
		info, err := os.Lstat(srcPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Skip missing entries quietly: a setup that lists
				// `.env` is fine even on a fresh clone that does not
				// yet have one.
				continue
			}
			return copied, err
		}
		if info.IsDir() {
			if err := copyTree(srcPath, dstPath); err != nil {
				return copied, err
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				return copied, err
			}
			if err := copyFile(srcPath, dstPath); err != nil {
				return copied, err
			}
		}
		copied = append(copied, rel)
	}
	return copied, nil
}

// copyTree mirrors a directory tree, skipping .git/ subdirs (which are
// always per-worktree state).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if info.IsDir() {
			if rel == ".git" || strings.HasPrefix(rel, ".git/") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), info.Mode().Perm())
		}
		// Symlinks: copy as plain files for simplicity.
		return copyFile(path, filepath.Join(dst, rel))
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if info, err := os.Stat(src); err == nil {
		_ = os.Chmod(dst, info.Mode().Perm())
	}
	return nil
}

func sortStrings(s []string) {
	// Inline insertion sort to avoid pulling in `sort` for such a
	// small slice; the slices are typically <100 elements.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
