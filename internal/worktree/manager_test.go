package worktree_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/git"
	"github.com/Mizune/mosaic/internal/worktree"
)

func gitInit(dir string) {
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		full := append([]string{"-C", dir}, args...)
		Expect(exec.Command("git", full...).Run()).To(Succeed())
	}
	// One real commit so HEAD exists.
	Expect(os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo\n"), 0o644)).To(Succeed())
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "-m", "init"},
	} {
		full := append([]string{"-C", dir}, args...)
		cmd := exec.Command("git", full...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		Expect(cmd.Run()).To(Succeed())
	}
}

var _ = Describe("Manager.Create", func() {
	var (
		ctx  context.Context
		mgr  *worktree.Manager
		repo string
	)

	BeforeEach(func() {
		ctx = context.Background()
		repo = GinkgoT().TempDir()
		gitInit(repo)
		mgr = worktree.NewManager(git.New())
	})

	It("creates a worktree on a new branch from main", func() {
		dest := filepath.Join(GinkgoT().TempDir(), "wt-feat")

		res, err := mgr.Create(ctx, worktree.CreateInput{
			Repo:         repo,
			DestPath:     dest,
			Branch:       "feat-x",
			CreateBranch: true,
			BaseRef:      "main",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Path).To(Equal(dest))
		Expect(dest).To(BeADirectory())
		Expect(filepath.Join(dest, "README.md")).To(BeAnExistingFile())
		Expect(res.Stages).NotTo(BeEmpty())
	})

	It("copies declared files into the new worktree", func() {
		Expect(os.WriteFile(filepath.Join(repo, ".env"), []byte("FOO=bar\n"), 0o644)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(repo, ".vscode"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(repo, ".vscode", "settings.json"), []byte("{}"), 0o644)).To(Succeed())

		dest := filepath.Join(GinkgoT().TempDir(), "wt-with-copy")

		_, err := mgr.Create(ctx, worktree.CreateInput{
			Repo:         repo,
			DestPath:     dest,
			Branch:       "with-copy",
			CreateBranch: true,
			BaseRef:      "main",
			Setup: config.WorktreeSetup{
				Copy: []string{".env", ".vscode/"},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(dest, ".env")).To(BeAnExistingFile())
		Expect(filepath.Join(dest, ".vscode", "settings.json")).To(BeAnExistingFile())
	})

	It("copies ignored files when CopyIgnored is true", func() {
		Expect(os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("local.env\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(repo, "local.env"), []byte("SECRET=1\n"), 0o644)).To(Succeed())

		dest := filepath.Join(GinkgoT().TempDir(), "wt-ign")

		_, err := mgr.Create(ctx, worktree.CreateInput{
			Repo:         repo,
			DestPath:     dest,
			Branch:       "ign",
			CreateBranch: true,
			BaseRef:      "main",
			Setup:        config.WorktreeSetup{CopyIgnored: true},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(dest, "local.env")).To(BeAnExistingFile())
	})

	It("runs setup commands inside the new worktree", func() {
		dest := filepath.Join(GinkgoT().TempDir(), "wt-cmd")

		_, err := mgr.Create(ctx, worktree.CreateInput{
			Repo:         repo,
			DestPath:     dest,
			Branch:       "cmd-branch",
			CreateBranch: true,
			BaseRef:      "main",
			Setup: config.WorktreeSetup{
				Commands: []string{"touch generated.txt"},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(dest, "generated.txt")).To(BeAnExistingFile())
	})

	It("runs the post-create hook", func() {
		hookDir := filepath.Join(repo, "scripts")
		Expect(os.MkdirAll(hookDir, 0o755)).To(Succeed())
		hook := filepath.Join(hookDir, "post.sh")
		Expect(os.WriteFile(hook, []byte("#!/bin/sh\ntouch hook-ran.txt\n"), 0o755)).To(Succeed())

		dest := filepath.Join(GinkgoT().TempDir(), "wt-hook")

		_, err := mgr.Create(ctx, worktree.CreateInput{
			Repo:         repo,
			DestPath:     dest,
			Branch:       "hook-branch",
			CreateBranch: true,
			BaseRef:      "main",
			Setup: config.WorktreeSetup{
				PostCreateHook: "scripts/post.sh",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(dest, "hook-ran.txt")).To(BeAnExistingFile())
	})

	It("rejects unsafe paths in the copy list", func() {
		dest := filepath.Join(GinkgoT().TempDir(), "wt-bad")

		_, err := mgr.Create(ctx, worktree.CreateInput{
			Repo:         repo,
			DestPath:     dest,
			Branch:       "bad",
			CreateBranch: true,
			BaseRef:      "main",
			Setup:        config.WorktreeSetup{Copy: []string{"../escape"}},
		})
		Expect(err).To(HaveOccurred())
	})

	It("errors when destination already exists", func() {
		dest := filepath.Join(GinkgoT().TempDir(), "wt-exists")
		Expect(os.MkdirAll(dest, 0o755)).To(Succeed())

		_, err := mgr.Create(ctx, worktree.CreateInput{
			Repo:         repo,
			DestPath:     dest,
			Branch:       "x",
			CreateBranch: true,
			BaseRef:      "main",
		})
		Expect(err).To(HaveOccurred())
	})
})
