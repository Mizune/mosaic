package config_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/config"
)

var _ = Describe("Merge", func() {
	Context("scalar fields", func() {
		It("keeps the base value when the override is the zero value", func() {
			base := config.Default()
			out := config.Merge(base, config.Config{})
			Expect(out.Mosaic.DefaultTool).To(Equal("claude"))
			Expect(out.UI.Theme).To(Equal("catppuccin-mocha"))
		})

		It("lets a non-zero override replace a base scalar", func() {
			base := config.Default()
			ov := config.Config{
				Mosaic: config.MosaicSection{DefaultTool: "shell"},
				UI:     config.UISection{Theme: "tokyo-night"},
			}
			out := config.Merge(base, ov)
			Expect(out.Mosaic.DefaultTool).To(Equal("shell"))
			Expect(out.UI.Theme).To(Equal("tokyo-night"))
		})

		It("never mutates the inputs", func() {
			base := config.Default()
			ov := config.Config{Mosaic: config.MosaicSection{DefaultTool: "shell"}}
			_ = config.Merge(base, ov)
			Expect(base.Mosaic.DefaultTool).To(Equal("claude"))
			Expect(ov.Mosaic.DefaultTool).To(Equal("shell"))
		})
	})

	Context("worktree setups", func() {
		It("replaces a setup's Copy list when the override sets it", func() {
			base := config.Config{
				Worktree: config.WorktreeSection{
					Setup: map[string]config.WorktreeSetup{
						"default": {Copy: []string{".env", ".env.local"}},
					},
				},
			}
			ov := config.Config{
				Worktree: config.WorktreeSection{
					Setup: map[string]config.WorktreeSetup{
						"default": {Copy: []string{".env"}},
					},
				},
			}
			out := config.Merge(base, ov)
			Expect(out.Worktree.Setup["default"].Copy).To(Equal([]string{".env"}))
		})

		It("appends CopyExtra to the resolved Copy list", func() {
			base := config.Config{
				Worktree: config.WorktreeSection{
					Setup: map[string]config.WorktreeSetup{
						"default": {Copy: []string{".env"}},
					},
				},
			}
			ov := config.Config{
				Worktree: config.WorktreeSection{
					Setup: map[string]config.WorktreeSetup{
						"default": {CopyExtra: []string{".vscode/", "turbo.json"}},
					},
				},
			}
			out := config.Merge(base, ov)
			Expect(out.Worktree.Setup["default"].Copy).
				To(Equal([]string{".env", ".vscode/", "turbo.json"}))
			Expect(out.Worktree.Setup["default"].CopyExtra).To(BeNil(),
				"CopyExtra should be folded into Copy and cleared")
		})

		It("replaces and then appends commands", func() {
			base := config.Config{
				Worktree: config.WorktreeSection{
					Setup: map[string]config.WorktreeSetup{
						"default": {Commands: []string{"pnpm install"}},
					},
				},
			}
			ov := config.Config{
				Worktree: config.WorktreeSection{
					Setup: map[string]config.WorktreeSetup{
						"default": {
							Commands:      []string{"pnpm install --frozen-lockfile"},
							CommandsExtra: []string{"pnpm db:generate"},
						},
					},
				},
			}
			out := config.Merge(base, ov)
			Expect(out.Worktree.Setup["default"].Commands).To(Equal([]string{
				"pnpm install --frozen-lockfile",
				"pnpm db:generate",
			}))
		})

		It("merges env maps key-by-key with override winning", func() {
			base := config.Config{
				Worktree: config.WorktreeSection{
					Setup: map[string]config.WorktreeSetup{
						"default": {Env: map[string]string{
							"NODE_ENV": "development",
							"CI":       "0",
						}},
					},
				},
			}
			ov := config.Config{
				Worktree: config.WorktreeSection{
					Setup: map[string]config.WorktreeSetup{
						"default": {Env: map[string]string{
							"CI":         "1",
							"DEBUG_MODE": "true",
						}},
					},
				},
			}
			out := config.Merge(base, ov).Worktree.Setup["default"].Env
			Expect(out).To(HaveKeyWithValue("NODE_ENV", "development"))
			Expect(out).To(HaveKeyWithValue("CI", "1"))
			Expect(out).To(HaveKeyWithValue("DEBUG_MODE", "true"))
		})

		It("keeps base-only setups unchanged when the override has none", func() {
			base := config.Config{
				Worktree: config.WorktreeSection{
					Setup: map[string]config.WorktreeSetup{
						"default": {Copy: []string{".env"}},
					},
				},
			}
			out := config.Merge(base, config.Config{})
			Expect(out.Worktree.Setup).To(HaveKey("default"))
			Expect(out.Worktree.Setup["default"].Copy).To(Equal([]string{".env"}))
		})

		It("adds override-only setups verbatim with extras folded in", func() {
			base := config.Config{}
			ov := config.Config{
				Worktree: config.WorktreeSection{
					Setup: map[string]config.WorktreeSetup{
						"minimal": {
							Copy:      []string{".env"},
							CopyExtra: []string{".env.local"},
						},
					},
				},
			}
			out := config.Merge(base, ov)
			Expect(out.Worktree.Setup["minimal"].Copy).To(Equal([]string{".env", ".env.local"}))
			Expect(out.Worktree.Setup["minimal"].CopyExtra).To(BeNil())
		})
	})

	Context("hooks section", func() {
		It("keeps the base events when the override has none", func() {
			base := config.Default()
			out := config.Merge(base, config.Config{})
			Expect(out.Hooks.Events).To(ContainElement("Notification"))
		})

		It("replaces events and trusts Suggested when the override sets events", func() {
			base := config.Default()
			ov := config.Config{
				Hooks: config.HooksSection{
					Suggested: false,
					Events:    []string{"Stop"},
				},
			}
			out := config.Merge(base, ov)
			Expect(out.Hooks.Events).To(Equal([]string{"Stop"}))
			Expect(out.Hooks.Suggested).To(BeFalse())
		})
	})
})
