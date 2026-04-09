// Package worktree creates git worktrees and applies the declarative
// setup recipes mosaic exposes via [config.WorktreeSetup]:
//
//   - copy a list of paths from the source repo into the new worktree
//   - optionally include all .gitignore'd files (.env, .vscode/, etc.)
//   - optionally include untracked files
//   - run a sequence of shell commands inside the worktree
//   - run a final post-create hook script
//
// Manager is the high-level entry point. The lower-level building
// blocks (copy walker, command runner, hook runner) live in setup.go
// and hook.go and are kept private so callers cannot misuse them.
package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/git"
)

// Manager wires the git wrapper, the copy walker and the command
// runner into a single Create operation.
type Manager struct {
	git *git.Client
}

// NewManager constructs a Manager. Pass nil to use the default git
// client (which shells out to the system git binary).
func NewManager(g *git.Client) *Manager {
	if g == nil {
		g = git.New()
	}
	return &Manager{git: g}
}

// CreateInput describes a worktree creation request.
type CreateInput struct {
	// Repo is the source repository.
	Repo string

	// DestPath is where the worktree should live.
	DestPath string

	// Branch is the branch to check out in the new worktree.
	Branch string

	// BaseRef is the ref to branch from when CreateBranch is true.
	// Defaults to the result of [git.Client.CurrentBranch] on Repo.
	BaseRef string

	// CreateBranch tells git to create Branch from BaseRef. Set to
	// false when Branch already exists and you just want to attach a
	// worktree to it.
	CreateBranch bool

	// Setup is the recipe to apply after the worktree exists. Pass
	// the zero value to skip setup entirely.
	Setup config.WorktreeSetup
}

// CreateResult bundles the worktree path and a list of human-readable
// stage labels for the TUI to render in a progress overlay.
type CreateResult struct {
	Path   string
	Branch string
	Stages []StageReport
}

// StageReport is one row in the progress overlay (`copy: .env`,
// `cmd: pnpm install`, etc.).
type StageReport struct {
	Stage string
	Label string
	Err   error
}

// Create runs the full pipeline: git worktree add → copy files →
// run commands → run post-create hook.
//
// On any failure the partial worktree is intentionally left in place
// so the user can inspect it. The TUI can call [Manager.Remove]
// explicitly to clean it up.
func (m *Manager) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	if in.Repo == "" || in.DestPath == "" || in.Branch == "" {
		return CreateResult{}, errors.New("worktree: repo, dest_path and branch are required")
	}
	if _, err := os.Stat(in.DestPath); err == nil {
		return CreateResult{}, fmt.Errorf("worktree: destination already exists: %s", in.DestPath)
	}
	if in.BaseRef == "" && in.CreateBranch {
		ref, err := m.git.CurrentBranch(ctx, in.Repo)
		if err != nil {
			return CreateResult{}, fmt.Errorf("resolve base ref: %w", err)
		}
		in.BaseRef = ref
	}

	res := CreateResult{Path: in.DestPath, Branch: in.Branch}

	// 1. git worktree add
	if err := m.git.WorktreeAdd(ctx, git.WorktreeAddOptions{
		Repo:         in.Repo,
		Path:         in.DestPath,
		Branch:       in.Branch,
		CreateBranch: in.CreateBranch,
		BaseRef:      in.BaseRef,
	}); err != nil {
		return res, fmt.Errorf("git worktree add: %w", err)
	}
	res.Stages = append(res.Stages, StageReport{
		Stage: "git",
		Label: "worktree add " + filepath.Base(in.DestPath),
	})

	// 2. copy files
	copied, err := copyDeclaredFiles(ctx, m.git, in.Repo, in.DestPath, in.Setup)
	for _, c := range copied {
		res.Stages = append(res.Stages, StageReport{
			Stage: "copy",
			Label: c,
		})
	}
	if err != nil {
		res.Stages = append(res.Stages, StageReport{Stage: "copy", Label: "failed", Err: err})
		return res, err
	}

	// 3. setup commands
	for _, cmd := range in.Setup.Commands {
		if err := runCommand(ctx, in.DestPath, cmd, in.Setup.Env); err != nil {
			res.Stages = append(res.Stages, StageReport{Stage: "cmd", Label: cmd, Err: err})
			return res, fmt.Errorf("setup command %q failed: %w", cmd, err)
		}
		res.Stages = append(res.Stages, StageReport{Stage: "cmd", Label: cmd})
	}

	// 4. post-create hook
	if in.Setup.PostCreateHook != "" {
		hook := in.Setup.PostCreateHook
		if !filepath.IsAbs(hook) {
			hook = filepath.Join(in.Repo, hook)
		}
		if err := runHook(ctx, in.DestPath, hook, in.Setup.Env); err != nil {
			res.Stages = append(res.Stages, StageReport{Stage: "hook", Label: hook, Err: err})
			return res, fmt.Errorf("post-create hook %q failed: %w", hook, err)
		}
		res.Stages = append(res.Stages, StageReport{Stage: "hook", Label: hook})
	}

	return res, nil
}

// Remove tears down a worktree (git metadata + filesystem). Force is
// passed through to `git worktree remove --force`.
func (m *Manager) Remove(ctx context.Context, repo, dest string, force bool) error {
	return m.git.WorktreeRemove(ctx, repo, dest, force)
}
