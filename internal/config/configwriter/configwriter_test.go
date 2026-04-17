package configwriter_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/config"
	"github.com/Mizune/mosaic/internal/config/configwriter"
)

var _ = Describe("UpsertWorktreeSetup", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	decode := func(path string) config.Config {
		var cfg config.Config
		_, err := toml.DecodeFile(path, &cfg)
		Expect(err).NotTo(HaveOccurred())
		return cfg
	}

	readRaw := func(path string) string {
		b, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		return string(b)
	}

	It("creates the .mosaic directory and a new config file", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		recipe := config.WorktreeSetup{
			Copy:     []string{".env", ".vscode/"},
			Commands: []string{"pnpm install"},
		}

		Expect(configwriter.UpsertWorktreeSetup(path, "alpha", recipe)).To(Succeed())

		info, err := os.Stat(filepath.Dir(path))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.IsDir()).To(BeTrue())
		info, err = os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Size()).To(BeNumerically(">", 0))

		cfg := decode(path)
		Expect(cfg.Worktree.Setup).To(HaveKey("alpha"))
		Expect(cfg.Worktree.Setup["alpha"].Copy).To(Equal([]string{".env", ".vscode/"}))
		Expect(cfg.Worktree.Setup["alpha"].Commands).To(Equal([]string{"pnpm install"}))
	})

	It("appends a new recipe while preserving existing recipes", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(configwriter.UpsertWorktreeSetup(path, "alpha", config.WorktreeSetup{
			Copy: []string{".env"},
		})).To(Succeed())
		Expect(configwriter.UpsertWorktreeSetup(path, "beta", config.WorktreeSetup{
			Copy: []string{".env.local"},
		})).To(Succeed())

		cfg := decode(path)
		Expect(cfg.Worktree.Setup).To(HaveKey("alpha"))
		Expect(cfg.Worktree.Setup).To(HaveKey("beta"))
		Expect(cfg.Worktree.Setup["alpha"].Copy).To(Equal([]string{".env"}))
		Expect(cfg.Worktree.Setup["beta"].Copy).To(Equal([]string{".env.local"}))
	})

	It("overwrites a recipe with the same name", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(configwriter.UpsertWorktreeSetup(path, "alpha", config.WorktreeSetup{
			Copy: []string{".env"},
		})).To(Succeed())
		Expect(configwriter.UpsertWorktreeSetup(path, "alpha", config.WorktreeSetup{
			Copy:     []string{".env.local", ".vscode/"},
			Commands: []string{"go mod download"},
		})).To(Succeed())

		cfg := decode(path)
		Expect(cfg.Worktree.Setup).To(HaveLen(1))
		Expect(cfg.Worktree.Setup["alpha"].Copy).To(Equal([]string{".env.local", ".vscode/"}))
		Expect(cfg.Worktree.Setup["alpha"].Commands).To(Equal([]string{"go mod download"}))
	})

	It("preserves unrelated sections already present in the file", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		existing := `[mosaic]
default_tool = "shell"

[ui]
theme = "tokyo-night"

[worktree.setup.default]
copy = [".env"]
`
		Expect(os.WriteFile(path, []byte(existing), 0o644)).To(Succeed())

		Expect(configwriter.UpsertWorktreeSetup(path, "bar", config.WorktreeSetup{
			Copy: []string{".env.local"},
		})).To(Succeed())

		cfg := decode(path)
		Expect(cfg.Mosaic.DefaultTool).To(Equal("shell"))
		Expect(cfg.UI.Theme).To(Equal("tokyo-night"))
		Expect(cfg.Worktree.Setup).To(HaveKey("default"))
		Expect(cfg.Worktree.Setup).To(HaveKey("bar"))
		Expect(cfg.Worktree.Setup["default"].Copy).To(Equal([]string{".env"}))
		Expect(cfg.Worktree.Setup["bar"].Copy).To(Equal([]string{".env.local"}))
	})

	It("preserves comments in unrelated sections across an upsert", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		existing := `# ==============================================
# Project-local mosaic config
# Edit with care — the wizard preserves comments.
# ==============================================

[mosaic]
# Prefer shell sessions for this repo
default_tool = "shell"

[ui]
theme = "tokyo-night"   # matches my terminal

[worktree.setup.default]
# Keep this recipe minimal
copy = [".env"]
`
		Expect(os.WriteFile(path, []byte(existing), 0o644)).To(Succeed())

		Expect(configwriter.UpsertWorktreeSetup(path, "bar", config.WorktreeSetup{
			Copy: []string{".env.local"},
		})).To(Succeed())

		raw := readRaw(path)
		Expect(raw).To(ContainSubstring("# ==============================================\n# Project-local mosaic config"))
		Expect(raw).To(ContainSubstring("# Prefer shell sessions for this repo"))
		Expect(raw).To(ContainSubstring("# matches my terminal"))
		Expect(raw).To(ContainSubstring("# Keep this recipe minimal"))
		Expect(raw).To(ContainSubstring("[worktree.setup.bar]"))
	})

	It("replaces an existing recipe block without touching surrounding comments", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		existing := `# header comment

[mosaic]
default_tool = "shell"

# Before the recipe
[worktree.setup.alpha]
copy = [".env"]

# After the recipe
[ui]
theme = "tokyo-night"
`
		Expect(os.WriteFile(path, []byte(existing), 0o644)).To(Succeed())

		Expect(configwriter.UpsertWorktreeSetup(path, "alpha", config.WorktreeSetup{
			Copy: []string{".env", ".vscode/"},
		})).To(Succeed())

		raw := readRaw(path)
		Expect(raw).To(ContainSubstring("# header comment"))
		Expect(raw).To(ContainSubstring("# Before the recipe"))
		Expect(raw).To(ContainSubstring("# After the recipe"))
		// Replaced recipe should reflect the new Copy list, in order.
		alphaIdx := strings.Index(raw, "[worktree.setup.alpha]")
		Expect(alphaIdx).To(BeNumerically(">=", 0))
		Expect(raw[alphaIdx:]).To(ContainSubstring("copy = [\".env\", \".vscode/\"]"))
	})

	It("replaces a recipe and its sub-tables together", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		existing := `[worktree.setup.alpha]
copy = [".env"]

[worktree.setup.alpha.env]
FOO = "old"

[worktree.setup.beta]
copy = [".env.local"]
`
		Expect(os.WriteFile(path, []byte(existing), 0o644)).To(Succeed())

		Expect(configwriter.UpsertWorktreeSetup(path, "alpha", config.WorktreeSetup{
			Copy: []string{".env.new"},
			Env:  map[string]string{"FOO": "new"},
		})).To(Succeed())

		cfg := decode(path)
		Expect(cfg.Worktree.Setup["alpha"].Copy).To(Equal([]string{".env.new"}))
		Expect(cfg.Worktree.Setup["alpha"].Env["FOO"]).To(Equal("new"))
		Expect(cfg.Worktree.Setup["beta"].Copy).To(Equal([]string{".env.local"}))

		raw := readRaw(path)
		// The old FOO="old" should have been replaced.
		Expect(strings.Count(raw, `FOO`)).To(Equal(1))
		Expect(raw).To(ContainSubstring(`FOO = "new"`))
	})

	It("writes atomically (no .tmp file left behind on success)", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(configwriter.UpsertWorktreeSetup(path, "alpha",
			config.WorktreeSetup{Copy: []string{".env"}})).To(Succeed())

		_, err := os.Stat(path + ".tmp")
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("preserves permissions of an existing file", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte("# empty\n"), 0o640)).To(Succeed())
		Expect(configwriter.UpsertWorktreeSetup(path, "alpha",
			config.WorktreeSetup{Copy: []string{".env"}})).To(Succeed())
		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o640)))
	})

	It("defaults new files to 0600", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(configwriter.UpsertWorktreeSetup(path, "alpha",
			config.WorktreeSetup{Copy: []string{".env"}})).To(Succeed())
		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	})

	It("rejects empty recipe names", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		err := configwriter.UpsertWorktreeSetup(path, "", config.WorktreeSetup{})
		Expect(err).To(HaveOccurred())
	})

	It("rejects empty paths", func() {
		err := configwriter.UpsertWorktreeSetup("", "alpha", config.WorktreeSetup{})
		Expect(err).To(HaveOccurred())
	})

	// Regression for b-006: splicedSection previously treated
	// `[[project]]` headers as "not a header", so any array-of-tables
	// entries sitting between the target section and the next regular
	// header were swallowed into the replace range and silently lost.
	It("preserves [[project]] entries that sit between the target section and the next header", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		existing := `[worktree.setup.alpha]
copy = [".env"]

[[project]]
name = "foo"
path = "~/foo"

[[project]]
name = "bar"
path = "~/bar"

[mosaic]
default_tool = "claude"
`
		Expect(os.WriteFile(path, []byte(existing), 0o644)).To(Succeed())
		Expect(configwriter.UpsertWorktreeSetup(path, "alpha", config.WorktreeSetup{
			Copy: []string{".env", ".vscode/"},
		})).To(Succeed())

		raw := readRaw(path)
		Expect(raw).To(ContainSubstring(`[[project]]`))
		Expect(raw).To(ContainSubstring(`name = "foo"`))
		Expect(raw).To(ContainSubstring(`name = "bar"`))
		Expect(raw).To(ContainSubstring(`[mosaic]`))

		// The replace must still have landed on the target recipe.
		cfg := decode(path)
		Expect(cfg.Worktree.Setup["alpha"].Copy).To(Equal([]string{".env", ".vscode/"}))
		Expect(cfg.Projects).To(HaveLen(2))
	})

	// Companion to the above: verify a [[project]] block sitting
	// BEFORE the target section is also preserved. This exercises
	// the start-scan side — array-of-tables headers must not match
	// the target path even if the inner name happens to collide.
	It("preserves [[project]] entries that sit before the target section", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		existing := `[[project]]
name = "foo"
path = "~/foo"

[worktree.setup.alpha]
copy = [".env"]

[mosaic]
default_tool = "claude"
`
		Expect(os.WriteFile(path, []byte(existing), 0o644)).To(Succeed())
		Expect(configwriter.UpsertWorktreeSetup(path, "alpha", config.WorktreeSetup{
			Copy: []string{".env.local"},
		})).To(Succeed())

		raw := readRaw(path)
		Expect(raw).To(ContainSubstring(`[[project]]`))
		Expect(raw).To(ContainSubstring(`name = "foo"`))
		Expect(raw).To(ContainSubstring(`[mosaic]`))

		cfg := decode(path)
		Expect(cfg.Projects).To(HaveLen(1))
		Expect(cfg.Worktree.Setup["alpha"].Copy).To(Equal([]string{".env.local"}))
	})

	// Regression for b-006: RenderFragment used an unanchored
	// bytes.ReplaceAll to rewrite wrapper-injected leaf headers
	// (`[alpha.` → `[worktree.setup.alpha.`). Any string VALUE that
	// happened to contain the same substring — e.g. an Env value —
	// was silently corrupted. The anchored line-by-line rewrite
	// should leave string values untouched.
	It("preserves string values that contain the recipe name in brackets", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		recipe := config.WorktreeSetup{
			Env: map[string]string{
				"MY_VAR":  "prefix[alpha.nested]suffix",
				"OTHER":   "has [alpha] token",
				"UNRELAT": "plain value",
			},
		}
		Expect(configwriter.UpsertWorktreeSetup(path, "alpha", recipe)).To(Succeed())

		cfg := decode(path)
		Expect(cfg.Worktree.Setup).To(HaveKey("alpha"))
		Expect(cfg.Worktree.Setup["alpha"].Env["MY_VAR"]).To(Equal("prefix[alpha.nested]suffix"))
		Expect(cfg.Worktree.Setup["alpha"].Env["OTHER"]).To(Equal("has [alpha] token"))
		Expect(cfg.Worktree.Setup["alpha"].Env["UNRELAT"]).To(Equal("plain value"))

		// The rewritten header must still be the fully-qualified one.
		raw := readRaw(path)
		Expect(raw).To(ContainSubstring("[worktree.setup.alpha.env]"))
	})
})

var _ = Describe("UpsertSections", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	readRaw := func(path string) string {
		b, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		return string(b)
	}

	// Regression for b-008: saveSettings previously called
	// UpsertSection three times in a row, so a failure on the
	// second or third call left a partial file. UpsertSections
	// folds every splice into a single read-modify-write so the
	// happy path lands all sections together.
	It("applies every section in a single atomic write", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(configwriter.UpsertSections(path, []configwriter.Section{
			{TablePath: "mosaic", Value: config.MosaicSection{DefaultTool: "shell"}},
			{TablePath: "ui", Value: config.UISection{Theme: "tokyo-night"}},
		})).To(Succeed())

		raw := readRaw(path)
		Expect(raw).To(ContainSubstring("[mosaic]"))
		Expect(raw).To(ContainSubstring(`default_tool = "shell"`))
		Expect(raw).To(ContainSubstring("[ui]"))
		Expect(raw).To(ContainSubstring(`theme = "tokyo-night"`))
	})

	It("is a no-op when called with no sections", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		Expect(configwriter.UpsertSections(path, nil)).To(Succeed())
		_, err := os.Stat(path)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("rejects entries with an empty TablePath", func() {
		path := filepath.Join(dir, ".mosaic", "config.toml")
		err := configwriter.UpsertSections(path, []configwriter.Section{
			{TablePath: "mosaic", Value: config.MosaicSection{DefaultTool: "shell"}},
			{TablePath: "", Value: config.UISection{}},
		})
		Expect(err).To(HaveOccurred())
		// File must be untouched when validation rejects the batch.
		_, statErr := os.Stat(path)
		Expect(os.IsNotExist(statErr)).To(BeTrue())
	})
})
