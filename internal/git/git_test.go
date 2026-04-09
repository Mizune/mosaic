package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/git"
)

// gitInit creates a fresh git repository at dir with one initial
// commit so that the repo has a HEAD to point worktrees at.
func gitInit(dir string) {
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		full := append([]string{"-C", dir}, args...)
		cmd := exec.Command("git", full...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		Expect(cmd.Run()).To(Succeed(), "git "+args[0]+" failed")
	}
}

var _ = Describe("Client", func() {
	var (
		ctx context.Context
		c   *git.Client
		dir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		c = git.New()
		dir = GinkgoT().TempDir()
		gitInit(dir)
	})

	Describe("RepoRoot", func() {
		It("returns the top-level directory", func() {
			root, err := c.RepoRoot(ctx, dir)
			Expect(err).NotTo(HaveOccurred())
			abs, _ := filepath.EvalSymlinks(dir)
			gotAbs, _ := filepath.EvalSymlinks(root)
			Expect(gotAbs).To(Equal(abs))
		})
	})

	Describe("CurrentBranch", func() {
		It("returns main on a fresh repo", func() {
			branch, err := c.CurrentBranch(ctx, dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(branch).To(Equal("main"))
		})
	})

	Describe("BranchExists", func() {
		It("returns true for main and false for missing", func() {
			ok, err := c.BranchExists(ctx, dir, "main")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			ok, err = c.BranchExists(ctx, dir, "definitely-missing")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
		})
	})

	Describe("WorktreeAdd", func() {
		It("creates a new worktree on a new branch from main", func() {
			wt := filepath.Join(GinkgoT().TempDir(), "wt-x")

			Expect(c.WorktreeAdd(ctx, git.WorktreeAddOptions{
				Repo:         dir,
				Path:         wt,
				Branch:       "feat-x",
				CreateBranch: true,
				BaseRef:      "main",
			})).To(Succeed())

			Expect(wt).To(BeADirectory())
			info, err := os.Stat(filepath.Join(wt, ".git"))
			Expect(err).NotTo(HaveOccurred())
			Expect(info).NotTo(BeNil())
		})
	})

	Describe("ListIgnored / ListUntracked", func() {
		It("differentiates ignored from untracked files", func() {
			Expect(os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("y"), 0o644)).To(Succeed())

			ignored, err := c.ListIgnored(ctx, dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(ignored).To(ContainElement("ignored.txt"))
			Expect(ignored).NotTo(ContainElement("untracked.txt"))

			untracked, err := c.ListUntracked(ctx, dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(untracked).To(ContainElement("untracked.txt"))
		})
	})

	Describe("DiffAgainst / DiffFile", func() {
		// gitRun is a helper that shells out to git -C dir <args>.
		gitRun := func(args ...string) {
			full := append([]string{"-C", dir}, args...)
			cmd := exec.Command("git", full...)
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
				"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
			Expect(cmd.Run()).To(Succeed(), "git "+args[0]+" failed")
		}

		It("reports committed, unstaged, and untracked changes", func() {
			// Start from a committed baseline file on main.
			Expect(os.WriteFile(filepath.Join(dir, "a.txt"),
				[]byte("one\n"), 0o644)).To(Succeed())
			gitRun("add", "a.txt")
			gitRun("commit", "-m", "add a")

			// Branch away from main and make three kinds of
			// changes the Diff View should all surface.
			gitRun("checkout", "-b", "feat")
			// 1. committed change to a.txt
			Expect(os.WriteFile(filepath.Join(dir, "a.txt"),
				[]byte("one\ntwo\n"), 0o644)).To(Succeed())
			gitRun("add", "a.txt")
			gitRun("commit", "-m", "tweak a")
			// 2. unstaged change on top of the committed one
			Expect(os.WriteFile(filepath.Join(dir, "a.txt"),
				[]byte("one\ntwo\nthree\n"), 0o644)).To(Succeed())
			// 3. brand-new untracked file
			Expect(os.WriteFile(filepath.Join(dir, "b.txt"),
				[]byte("hello\n"), 0o644)).To(Succeed())

			rows, err := c.DiffAgainst(ctx, dir, "main")
			Expect(err).NotTo(HaveOccurred())

			pathsByStatus := map[string]git.DiffFileStat{}
			for _, r := range rows {
				pathsByStatus[r.Path] = r
			}
			Expect(pathsByStatus).To(HaveKey("a.txt"))
			Expect(pathsByStatus["a.txt"].Inserted).To(BeNumerically(">=", 2))
			Expect(pathsByStatus).To(HaveKey("b.txt"))
			Expect(pathsByStatus["b.txt"].Untracked).To(BeTrue())
			Expect(pathsByStatus["b.txt"].Status).To(Equal("?"))
		})

		It("returns the unified diff for a single file", func() {
			Expect(os.WriteFile(filepath.Join(dir, "a.txt"),
				[]byte("line 1\n"), 0o644)).To(Succeed())
			gitRun("add", "a.txt")
			gitRun("commit", "-m", "add a")
			Expect(os.WriteFile(filepath.Join(dir, "a.txt"),
				[]byte("line 1\nline 2\n"), 0o644)).To(Succeed())

			diff, err := c.DiffFile(ctx, dir, "HEAD", "a.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(diff).To(ContainSubstring("+line 2"))
			Expect(diff).To(ContainSubstring("--- a/a.txt"))
		})
	})

	Describe("StageFile / UnstageFile / IsStaged / Commit", func() {
		gitRun := func(args ...string) {
			full := append([]string{"-C", dir}, args...)
			cmd := exec.Command("git", full...)
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
				"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
			Expect(cmd.Run()).To(Succeed(), "git "+args[0]+" failed")
		}

		It("round-trips stage → unstage → is-staged → commit", func() {
			// Seed with a tracked file on main so IsStaged has a
			// HEAD to compare against.
			Expect(os.WriteFile(filepath.Join(dir, "file.txt"),
				[]byte("baseline\n"), 0o644)).To(Succeed())
			gitRun("add", "file.txt")
			gitRun("commit", "-m", "baseline")

			// Modify the file but don't stage yet — IsStaged
			// should report false.
			Expect(os.WriteFile(filepath.Join(dir, "file.txt"),
				[]byte("baseline\nnew line\n"), 0o644)).To(Succeed())

			staged, err := c.IsStaged(ctx, dir, "file.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(staged).To(BeFalse())

			// Stage the change.
			Expect(c.StageFile(ctx, dir, "file.txt")).To(Succeed())
			staged, err = c.IsStaged(ctx, dir, "file.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(staged).To(BeTrue())

			// Unstage and verify.
			Expect(c.UnstageFile(ctx, dir, "file.txt")).To(Succeed())
			staged, err = c.IsStaged(ctx, dir, "file.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(staged).To(BeFalse())

			// Re-stage and commit — Commit should succeed and the
			// working tree should be clean afterwards.
			Expect(c.StageFile(ctx, dir, "file.txt")).To(Succeed())
			Expect(c.Commit(ctx, dir, "follow-up")).To(Succeed())

			// After commit, IsStaged should be false again and
			// DiffAgainst should report no files vs HEAD.
			staged, _ = c.IsStaged(ctx, dir, "file.txt")
			Expect(staged).To(BeFalse())

			rows, err := c.DiffAgainst(ctx, dir, "HEAD")
			Expect(err).NotTo(HaveOccurred())
			for _, r := range rows {
				Expect(r.Path).NotTo(Equal("file.txt"))
			}
		})

		It("rejects an empty commit message", func() {
			Expect(c.Commit(ctx, dir, "   ")).To(HaveOccurred())
		})
	})

	Describe("ParseDiffHunks", func() {
		It("separates the preamble from hunks", func() {
			diff := `diff --git a/file.txt b/file.txt
index abc..def 100644
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 one
-two
+two-modified
+three
@@ -10,2 +11,3 @@ context tail
 ten
+eleven
`
			preamble, hunks := git.ParseDiffHunks(diff)
			Expect(preamble).To(ContainSubstring("--- a/file.txt"))
			Expect(preamble).To(ContainSubstring("+++ b/file.txt"))
			Expect(hunks).To(HaveLen(2))
			Expect(hunks[0].Header).To(HavePrefix("@@ -1,3"))
			Expect(hunks[1].Header).To(HavePrefix("@@ -10,2"))
		})

		It("handles an empty diff", func() {
			pre, hunks := git.ParseDiffHunks("")
			Expect(pre).To(BeEmpty())
			Expect(hunks).To(BeEmpty())
		})
	})
})
