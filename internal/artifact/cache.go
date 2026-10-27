package artifact

import (
	"errors"
	"os"
	"path/filepath"
)

// CacheRoot is the directory under which the viewer writes generated
// pages: <user cache dir>/mosaic/artifacts, or mosaic-artifacts under
// the system temp dir when the user cache dir is unavailable. It must
// agree with internal/viewer's default so Kill removes the right tree.
func CacheRoot() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		return filepath.Join(os.TempDir(), "mosaic-artifacts"), nil
	}
	return filepath.Join(base, "mosaic", "artifacts"), nil
}

// SessionCacheDir is the per-session subdirectory of CacheRoot.
func SessionCacheDir(sessionID string) (string, error) {
	if sessionID == "" {
		return "", errors.New("artifact: session id is required")
	}
	root, err := CacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, sessionID), nil
}

// RemoveSessionCache deletes everything the viewer generated for a
// session. It is called when the session is killed; a missing
// directory is not an error.
func RemoveSessionCache(sessionID string) error {
	dir, err := SessionCacheDir(sessionID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
