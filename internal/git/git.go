// Package git wraps the git CLI in a tiny typed API. mosaic shells out
// to git rather than linking go-git so that the user's actual git
// configuration (credentials, custom hooks, alternate remotes,
// signing) keeps working without surprises.
//
// Every operation accepts an absolute repository root so the wrapper
// is reusable from any caller without first cd'ing.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)


// Client is the typed entry point. It is safe for concurrent use; the
// underlying git binary is invoked once per call.
type Client struct {
	// Binary overrides the git binary path. Empty looks up "git" on
	// $PATH.
	Binary string
}

// New constructs a Client that uses the system git binary.
func New() *Client { return &Client{} }

// RepoRoot returns the absolute path to the top-level directory of the
// repository that contains path.
func (c *Client) RepoRoot(ctx context.Context, path string) (string, error) {
	out, err := c.run(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CurrentBranch returns the current branch name (or "HEAD" when
// detached).
func (c *Client) CurrentBranch(ctx context.Context, path string) (string, error) {
	out, err := c.run(ctx, path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// BranchExists reports whether the named ref resolves to a branch.
func (c *Client) BranchExists(ctx context.Context, repo, branch string) (bool, error) {
	_, err := c.run(ctx, repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

// WorktreeAddOptions configures [Client.WorktreeAdd].
type WorktreeAddOptions struct {
	// Repo is the absolute path to the source repository.
	Repo string

	// Path is the absolute destination path for the worktree.
	Path string

	// Branch is the branch to check out in the new worktree. If
	// CreateBranch is true, the branch is created from BaseRef.
	Branch       string
	CreateBranch bool
	BaseRef      string

	// Force allows overwriting an existing path or recreating a
	// worktree that git still has metadata for.
	Force bool
}

// WorktreeAdd runs `git worktree add`. The destination directory must
// not already exist (unless Force is set).
func (c *Client) WorktreeAdd(ctx context.Context, opts WorktreeAddOptions) error {
	if opts.Repo == "" || opts.Path == "" || opts.Branch == "" {
		return errors.New("git worktree add: repo, path and branch are required")
	}
	args := []string{"worktree", "add"}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.CreateBranch {
		args = append(args, "-b", opts.Branch)
	}
	args = append(args, opts.Path)
	if opts.CreateBranch && opts.BaseRef != "" {
		args = append(args, opts.BaseRef)
	} else if !opts.CreateBranch {
		args = append(args, opts.Branch)
	}
	_, err := c.run(ctx, opts.Repo, args...)
	return err
}

// WorktreeRemove runs `git worktree remove`.
func (c *Client) WorktreeRemove(ctx context.Context, repo, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := c.run(ctx, repo, args...)
	return err
}

// ListIgnored returns paths matched by .gitignore in the given
// directory. Paths are relative to repo.
func (c *Client) ListIgnored(ctx context.Context, repo string) ([]string, error) {
	out, err := c.run(ctx, repo, "ls-files", "--others", "-i", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// ListUntracked returns untracked paths in the working directory.
func (c *Client) ListUntracked(ctx context.Context, repo string) ([]string, error) {
	out, err := c.run(ctx, repo, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// DiffShortstat parses `git diff --shortstat <ref>...HEAD` into the
// number of files / inserted / deleted lines.
type DiffShortstat struct {
	Files    int
	Inserted int
	Deleted  int
}

// DiffFileStat describes one file's change summary between a base
// ref and the current worktree (including unstaged and untracked
// modifications). It is the row shape of the Diff View's left
// pane.
type DiffFileStat struct {
	// Path is the repository-relative file path.
	Path string

	// Status is a single-character summary:
	//   M - modified
	//   A - added (tracked, new since base)
	//   D - deleted
	//   R - renamed
	//   ? - untracked (new, not yet staged)
	Status string

	// Inserted / Deleted are the numstat line counts. Both are 0
	// for untracked files (git has no reference to diff against)
	// and for binary files (git returns "-\t-").
	Inserted int
	Deleted  int

	// Untracked is true for files that have never been tracked by
	// git. The Diff View uses this to branch rendering (we cat the
	// file contents instead of asking git for a unified diff).
	Untracked bool

	// Staged reflects whether the index has staged changes for
	// this path. Populated by DiffAgainst via a second git query;
	// the Diff View shows a marker next to staged rows.
	Staged bool
}

// DiffAgainst returns every file that has changed between baseRef
// and the current state of the worktree (committed + unstaged),
// plus every untracked file. Each row carries numstat counts when
// git can compute them.
//
// baseRef is typically a branch name like "main"; DiffAgainst does
// NOT run `git fetch`, so the caller is responsible for keeping
// the local ref up to date if they want to compare against the
// remote tip. When the requested baseRef does not resolve in the
// repo (no `main` branch, brand-new clone, ...), DiffAgainst
// transparently falls back through `master`, `origin/HEAD`, and
// finally `HEAD` so the user always sees something sensible
// instead of a fatal "ambiguous argument" error.
func (c *Client) DiffAgainst(ctx context.Context, repo, baseRef string) ([]DiffFileStat, error) {
	resolved, err := c.resolveDiffBase(ctx, repo, baseRef)
	if err != nil {
		return nil, err
	}
	baseRef = resolved

	// --numstat prints "<add>\t<del>\t<path>" per file, binary
	// files as "-\t-\t<path>". Rename entries look like
	// "1\t2\toldname => newname" which we normalise below.
	out, err := c.run(ctx, repo, "diff", "--numstat", baseRef)
	if err != nil {
		return nil, err
	}
	rows := parseNumstat(out)

	// --name-status carries the single-letter code per file; we
	// merge it into the numstat rows by path. If a file appears
	// in one list but not the other (renames, unusual modes) we
	// still keep whatever we have.
	nsOut, err := c.run(ctx, repo, "diff", "--name-status", baseRef)
	if err != nil {
		return nil, err
	}
	statusByPath := parseNameStatus(nsOut)
	for i := range rows {
		if code, ok := statusByPath[rows[i].Path]; ok {
			rows[i].Status = code
		}
	}
	// Files from --name-status that --numstat did not report
	// (typically pure renames without line changes).
	for path, code := range statusByPath {
		if !containsPath(rows, path) {
			rows = append(rows, DiffFileStat{Path: path, Status: code})
		}
	}

	// Append untracked files. These aren't in git's diff output.
	untracked, err := c.ListUntracked(ctx, repo)
	if err == nil {
		for _, path := range untracked {
			rows = append(rows, DiffFileStat{
				Path:      path,
				Status:    "?",
				Untracked: true,
			})
		}
	}

	// Flag staged rows in a single `git diff --cached --name-only`
	// call so the Diff View can render a marker without looping
	// one query per file.
	stagedOut, err := c.run(ctx, repo, "diff", "--cached", "--name-only")
	if err == nil {
		staged := map[string]bool{}
		for _, p := range splitLines(stagedOut) {
			staged[p] = true
		}
		for i := range rows {
			if staged[rows[i].Path] {
				rows[i].Staged = true
			}
		}
	}

	// Stable order: by path so the Diff View is predictable.
	sortDiffRows(rows)
	return rows, nil
}

// DiffFile returns the unified diff for a single file, compared
// against baseRef (so committed + unstaged changes both appear in
// one patch). Untracked files need a different treatment — see
// ShowFile, which cats the raw contents.
//
// As with [Client.DiffAgainst], the base ref falls back through a
// short list of likely candidates so the call never produces an
// "ambiguous argument" error in repositories that don't follow
// the main / master convention.
func (c *Client) DiffFile(ctx context.Context, repo, baseRef, path string) (string, error) {
	resolved, err := c.resolveDiffBase(ctx, repo, baseRef)
	if err != nil {
		return "", err
	}
	// Passing `--` and the path guards against baseRef collisions
	// with files of the same name.
	return c.run(ctx, repo, "diff", resolved, "--", path)
}

// ResolveDiffBase exposes the internal resolveDiffBase chain so
// callers (e.g. the session service) can label the Diff View with
// the ref git actually used after the fallback walk.
func (c *Client) ResolveDiffBase(ctx context.Context, repo, preferred string) (string, error) {
	return c.resolveDiffBase(ctx, repo, preferred)
}

// resolveDiffBase walks a small list of candidate refs and
// returns the first one git resolves successfully. The order is
// (caller's preferred ref) → main → master → origin/HEAD → HEAD.
// HEAD always resolves in a working repo so the chain never
// returns an error in practice; the only failure mode is the
// caller pointing at a directory that isn't a git repo at all.
func (c *Client) resolveDiffBase(ctx context.Context, repo, preferred string) (string, error) {
	candidates := []string{preferred, "main", "master", "origin/HEAD", "HEAD"}
	seen := map[string]bool{}
	for _, ref := range candidates {
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		if _, err := c.run(ctx, repo, "rev-parse", "--verify", "--quiet", ref); err == nil {
			return ref, nil
		}
	}
	return "", fmt.Errorf("git diff: no usable base ref in %s (tried %v)",
		repo, candidates)
}

// ShowFile returns the raw contents of a file in the working tree
// as a fake unified diff (every line prefixed with `+`). It is the
// right display for untracked files the Diff View lists alongside
// real diffs.
func (c *Client) ShowFile(ctx context.Context, repo, path string) (string, error) {
	out, err := c.run(ctx, repo, "show", ":./"+path)
	if err == nil {
		return out, nil
	}
	// Untracked files don't exist in the index; fall back to
	// reading the working tree directly via cat-file-ish cat.
	// We shell out to `cat` inside the repo via the plain file
	// system — the caller passes repo + path.
	return readWorkingFile(repo, path)
}

// StageFile runs `git add -- <path>`, making the file (or the
// tracked changes within it) part of the next commit. Works for
// tracked modifications, new files, and deletions alike.
func (c *Client) StageFile(ctx context.Context, repo, path string) error {
	_, err := c.run(ctx, repo, "add", "--", path)
	return err
}

// UnstageFile runs `git reset HEAD -- <path>`, moving the file
// back to the unstaged side without touching the working tree.
// On a repo that has no HEAD yet (brand-new, no commits), it
// falls back to `git rm --cached --force`, which gives the same
// "unstage" semantics from the user's perspective.
func (c *Client) UnstageFile(ctx context.Context, repo, path string) error {
	if _, err := c.run(ctx, repo, "reset", "HEAD", "--", path); err == nil {
		return nil
	}
	_, err := c.run(ctx, repo, "rm", "--cached", "--force", "--", path)
	return err
}

// IsStaged reports whether a file has any staged changes (additions,
// modifications, or deletions) in the index relative to HEAD.
func (c *Client) IsStaged(ctx context.Context, repo, path string) (bool, error) {
	out, err := c.run(ctx, repo, "diff", "--cached", "--name-only", "--", path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Commit runs `git commit -m <message>`. The commit is always
// attributed to the user running mosaic — we never --no-verify,
// never skip signing, and never bypass hooks.
func (c *Client) Commit(ctx context.Context, repo, message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("git commit: message is required")
	}
	_, err := c.run(ctx, repo, "commit", "-m", message)
	return err
}

// CommitAmend runs `git commit --amend -m <message>`. It is the
// Diff View's `C` path — the user typed a new message for the
// last commit, and wants to replace it without adding a new
// commit on top.
//
// Like [Client.Commit], amend never skips hooks or bypasses
// signing. Empty messages are rejected so the user cannot
// accidentally wipe the existing commit message.
func (c *Client) CommitAmend(ctx context.Context, repo, message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("git commit --amend: message is required")
	}
	_, err := c.run(ctx, repo, "commit", "--amend", "-m", message)
	return err
}

// LastCommitMessage returns the first line of the repo's HEAD
// commit message — useful for pre-filling the commit input in
// amend mode.
func (c *Client) LastCommitMessage(ctx context.Context, repo string) (string, error) {
	out, err := c.run(ctx, repo, "log", "-1", "--pretty=%B")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// DiffHunk is one @@-delimited hunk inside a unified diff. The
// Diff View slices a file diff into these so the user can stage
// one change at a time without reaching for `git add -p`.
type DiffHunk struct {
	Header string   // the "@@ -a,b +c,d @@ ..." line
	Body   []string // context + added/removed lines that follow
}

// ParseDiffHunks splits a single-file unified diff into its
// preamble (the "--- a/..." / "+++ b/..." lines, plus any "diff
// --git" prologue) and the list of hunks. Lines that come
// before the first @@ are treated as the preamble; everything
// after is partitioned by subsequent @@ markers.
func ParseDiffHunks(diff string) (preamble string, hunks []DiffHunk) {
	if diff == "" {
		return "", nil
	}
	lines := strings.Split(diff, "\n")
	var pre []string
	i := 0
	for i < len(lines) && !strings.HasPrefix(lines[i], "@@") {
		pre = append(pre, lines[i])
		i++
	}
	var current *DiffHunk
	for ; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "@@") {
			if current != nil {
				hunks = append(hunks, *current)
			}
			current = &DiffHunk{Header: line}
			continue
		}
		if current != nil {
			current.Body = append(current.Body, line)
		}
	}
	if current != nil {
		hunks = append(hunks, *current)
	}
	return strings.Join(pre, "\n"), hunks
}

// ApplyHunk stages a single hunk by feeding a synthetic patch
// to `git apply --cached`. preamble is the "--- a/..." / "+++
// b/..." header block returned by [ParseDiffHunks]; hunk is one
// of the hunks from the same call.
//
// The caller is responsible for ensuring the hunk still applies
// cleanly — git will reject it otherwise, and we surface the
// stderr as the error text.
func (c *Client) ApplyHunk(ctx context.Context, repo, preamble string, hunk DiffHunk) error {
	var patch bytes.Buffer
	if preamble != "" {
		patch.WriteString(preamble)
		if !strings.HasSuffix(preamble, "\n") {
			patch.WriteByte('\n')
		}
	}
	patch.WriteString(hunk.Header)
	patch.WriteByte('\n')
	for _, line := range hunk.Body {
		patch.WriteString(line)
		patch.WriteByte('\n')
	}

	cmd := exec.CommandContext(ctx, c.binary(),
		"-C", repo, "apply", "--cached", "-")
	cmd.Stdin = &patch
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git apply --cached: %w: %s",
			err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// DiffShortstatAgainst returns a DiffShortstat between baseRef and
// HEAD plus the working tree. The base ref is resolved through
// the same fallback chain as [Client.DiffAgainst].
func (c *Client) DiffShortstatAgainst(ctx context.Context, repo, baseRef string) (DiffShortstat, error) {
	resolved, err := c.resolveDiffBase(ctx, repo, baseRef)
	if err != nil {
		return DiffShortstat{}, err
	}
	out, err := c.run(ctx, repo, "diff", "--shortstat", resolved)
	if err != nil {
		return DiffShortstat{}, err
	}
	return parseShortstat(out), nil
}

func parseShortstat(out string) DiffShortstat {
	out = strings.TrimSpace(out)
	if out == "" {
		return DiffShortstat{}
	}
	var stat DiffShortstat
	for _, part := range strings.Split(out, ",") {
		part = strings.TrimSpace(part)
		fields := strings.Fields(part)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(fields[1], "file"):
			stat.Files = n
		case strings.HasPrefix(fields[1], "insertion"):
			stat.Inserted = n
		case strings.HasPrefix(fields[1], "deletion"):
			stat.Deleted = n
		}
	}
	return stat
}

func splitLines(out string) []string {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	parts := strings.Split(out, "\n")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			res = append(res, p)
		}
	}
	return res
}

// parseNumstat turns git's --numstat output into DiffFileStat rows.
// Binary files ("-\t-\t<path>") get zeroed counters; rename entries
// ("oldname => newname") get the destination path.
func parseNumstat(out string) []DiffFileStat {
	var rows []DiffFileStat
	for _, line := range splitLines(out) {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		row := DiffFileStat{Status: "M", Path: renamedDest(fields[2])}
		if add, err := strconv.Atoi(fields[0]); err == nil {
			row.Inserted = add
		}
		if del, err := strconv.Atoi(fields[1]); err == nil {
			row.Deleted = del
		}
		rows = append(rows, row)
	}
	return rows
}

// parseNameStatus returns a map of path → single-letter status
// code from `git diff --name-status` output.
func parseNameStatus(out string) map[string]string {
	statuses := map[string]string{}
	for _, line := range splitLines(out) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		code := fields[0]
		path := fields[len(fields)-1]
		if len(code) > 1 {
			// e.g. "R100" for a 100%-rename.
			code = code[:1]
		}
		statuses[path] = code
	}
	return statuses
}

// renamedDest collapses git's "old => new" rename syntax to the
// destination side, which is what the user actually wants to see.
func renamedDest(path string) string {
	if idx := strings.Index(path, " => "); idx >= 0 {
		return path[idx+len(" => "):]
	}
	return path
}

// containsPath reports whether any existing row already has the
// given path (so we don't duplicate it when merging sources).
func containsPath(rows []DiffFileStat, path string) bool {
	for _, r := range rows {
		if r.Path == path {
			return true
		}
	}
	return false
}

// sortDiffRows sorts rows by path in place. It stays a tiny wrapper
// so the call site reads clearly; every row has a non-empty path so
// lexicographic order is unambiguous.
func sortDiffRows(rows []DiffFileStat) {
	sortByPath := func(i, j int) bool { return rows[i].Path < rows[j].Path }
	// Local sort without a package import; tiny slices, insertion
	// sort is fine here and keeps the package dependency-free.
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && sortByPath(j, j-1); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// readWorkingFile returns the contents of an untracked file as a
// synthetic unified diff (every line prefixed with `+`). This gives
// the Diff View a consistent rendering pipeline for all rows.
//
// Binary files (PNG, ZIP, the like) and very large files are
// short-circuited to a one-line synthetic diff so the user
// gets a useful "this is binary, X bytes" hint instead of a
// screenful of garbled bytes.
func readWorkingFile(repo, path string) (string, error) {
	full := filepath.Join(repo, path)
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return syntheticDiff(path,
			fmt.Sprintf("(not a regular file: %s)", info.Mode())), nil
	}

	body, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if isBinary(body) {
		return syntheticDiff(path,
			fmt.Sprintf("(binary file, %d bytes — preview suppressed)",
				info.Size())), nil
	}
	const maxSize = 256 * 1024 // 256 KiB
	if len(body) > maxSize {
		return syntheticDiff(path,
			fmt.Sprintf("(file too large to preview: %d bytes; press y to yank or open it directly)",
				info.Size())), nil
	}

	lines := strings.Split(string(body), "\n")
	var b strings.Builder
	b.WriteString("--- /dev/null\n")
	b.WriteString("+++ b/")
	b.WriteString(path)
	b.WriteString("\n")
	b.WriteString("@@ -0,0 +1,")
	b.WriteString(strconv.Itoa(len(lines)))
	b.WriteString(" @@\n")
	for _, line := range lines {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// syntheticDiff builds a 1-hunk patch whose only line is a
// human-readable note. Used for binaries / oversized files /
// non-regular paths so the Diff View body is never garbled.
func syntheticDiff(path, note string) string {
	var b strings.Builder
	b.WriteString("--- /dev/null\n")
	b.WriteString("+++ b/")
	b.WriteString(path)
	b.WriteString("\n@@ -0,0 +1,1 @@\n+")
	b.WriteString(note)
	b.WriteString("\n")
	return b.String()
}

// isBinary uses the same heuristic git uses: a file is binary
// if its first 8 KiB contain a NUL byte. The check is fast and
// catches every common binary format (images, archives, native
// executables) without needing libmagic.
func isBinary(body []byte) bool {
	const sample = 8 * 1024
	limit := len(body)
	if limit > sample {
		limit = sample
	}
	for i := 0; i < limit; i++ {
		if body[i] == 0 {
			return true
		}
	}
	return false
}

// run executes a git subcommand inside repo and returns its stdout.
func (c *Client) run(ctx context.Context, repo string, args ...string) (string, error) {
	full := append([]string{"-C", repo}, args...)
	cmd := exec.CommandContext(ctx, c.binary(), full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (c *Client) binary() string {
	if c.Binary != "" {
		return c.Binary
	}
	return "git"
}

// exitCode pulls the exit code out of an exec error, or -1 if it isn't
// an *exec.ExitError.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
