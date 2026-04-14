// Package selfupdate checks for and applies mosaic binary updates
// from GitHub Releases.
package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Sentinel errors returned by Check and Apply.
var (
	ErrRateLimited    = errors.New("GitHub API rate limit exceeded")
	ErrNoAsset        = errors.New("no matching release asset for this platform")
	ErrChecksumFailed = errors.New("checksum verification failed")
)

// Release is a minimal representation of a GitHub release.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is a single downloadable file in a GitHub release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckResult holds the outcome of a version comparison.
type CheckResult struct {
	Current     string
	Latest      string
	UpdateAvail bool
	Release     *Release
}

// Updater checks for and applies updates from GitHub Releases.
type Updater struct {
	Owner      string
	Repo       string
	CurrentVer string
	HTTPClient *http.Client
}

// New creates an Updater for the Mizune/mosaic repository.
func New(currentVersion string) *Updater {
	return &Updater{
		Owner:      "Mizune",
		Repo:       "mosaic",
		CurrentVer: currentVersion,
		HTTPClient: http.DefaultClient,
	}
}

// Check fetches the latest GitHub release and compares it against
// the current version. Returns UpdateAvail=false immediately when
// the current version is "dev" (no API call).
func (u *Updater) Check(ctx context.Context) (CheckResult, error) {
	if u.CurrentVer == "dev" {
		return CheckResult{Current: "dev"}, nil
	}

	current, err := semver.NewVersion(u.CurrentVer)
	if err != nil {
		return CheckResult{Current: u.CurrentVer}, fmt.Errorf("parse current version %q: %w", u.CurrentVer, err)
	}

	rel, err := u.fetchLatestRelease(ctx)
	if err != nil {
		return CheckResult{Current: u.CurrentVer}, err
	}

	tag := strings.TrimPrefix(rel.TagName, "v")
	latest, err := semver.NewVersion(tag)
	if err != nil {
		return CheckResult{Current: u.CurrentVer, Latest: tag}, fmt.Errorf("parse latest version %q: %w", tag, err)
	}

	return CheckResult{
		Current:     current.String(),
		Latest:      latest.String(),
		UpdateAvail: latest.GreaterThan(current),
		Release:     rel,
	}, nil
}

// Apply downloads the release binary for the current OS/arch,
// verifies its SHA-256 checksum, and atomically replaces the
// running executable.
func (u *Updater) Apply(ctx context.Context, rel *Release) error {
	ver := strings.TrimPrefix(rel.TagName, "v")
	wantName := AssetName(ver, runtime.GOOS, runtime.GOARCH)

	var assetURL string
	var checksumURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case wantName:
			assetURL = a.BrowserDownloadURL
		case "checksums.txt":
			checksumURL = a.BrowserDownloadURL
		}
	}
	if assetURL == "" {
		return fmt.Errorf("%w: expected %s", ErrNoAsset, wantName)
	}

	// Download the archive.
	archiveData, err := u.download(ctx, assetURL)
	if err != nil {
		return fmt.Errorf("download asset: %w", err)
	}

	// Verify checksum if available.
	if checksumURL != "" {
		checksumData, err := u.download(ctx, checksumURL)
		if err != nil {
			return fmt.Errorf("download checksums: %w", err)
		}
		if err := verifyChecksum(archiveData, checksumData, wantName); err != nil {
			return err
		}
	}

	// Extract the mosaic binary from the tar.gz archive.
	binary, err := extractBinary(archiveData)
	if err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}

	// Atomically replace the running executable.
	return replaceBinary(binary)
}

// fetchLatestRelease calls the GitHub Releases API.
func (u *Updater) fetchLatestRelease(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", u.Owner, u.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, ErrRateLimited
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &rel, nil
}

// download fetches a URL and returns its body as bytes.
func (u *Updater) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// AssetName returns the expected archive filename for a given
// version, OS and architecture. Exported for testing.
func AssetName(version, goos, goarch string) string {
	return fmt.Sprintf("mosaic_%s_%s_%s.tar.gz", version, goos, goarch)
}

// verifyChecksum checks the SHA-256 of data against the line for
// wantName in the checksums file.
func verifyChecksum(data, checksumFile []byte, wantName string) error {
	h := sha256.Sum256(data)
	got := hex.EncodeToString(h[:])

	for _, line := range strings.Split(string(checksumFile), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == wantName {
			if !strings.EqualFold(got, parts[0]) {
				return fmt.Errorf("%w: expected %s, got %s", ErrChecksumFailed, parts[0], got)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: no entry for %s in checksums.txt", ErrChecksumFailed, wantName)
}

// extractBinary reads a tar.gz archive and returns the content of
// the "mosaic" entry (the compiled binary).
func extractBinary(archive []byte) ([]byte, error) {
	gr, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		// The binary can be at the root or inside a directory
		// (goreleaser default: mosaic_VERSION_OS_ARCH/mosaic).
		base := filepath.Base(hdr.Name)
		if base == "mosaic" && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("mosaic binary not found in archive")
}

// replaceBinary atomically replaces the current executable with
// the new binary data. It writes to a temp file in the same
// directory and renames to ensure atomicity on POSIX.
func replaceBinary(binary []byte) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	dir := filepath.Dir(execPath)
	tmp, err := os.CreateTemp(dir, ".mosaic-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Clean up the temp file on any error path.
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp binary: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod temp binary: %w", err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	// Rename succeeded; prevent deferred cleanup.
	tmpPath = ""
	return nil
}
