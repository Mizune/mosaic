package config_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/config"
)

var _ = Describe("Projects registry", func() {
	Describe("Merge", func() {
		It("appends override-only projects", func() {
			base := config.Config{
				Projects: []config.ProjectEntry{
					{Name: "a", Path: "/a"},
				},
			}
			ov := config.Config{
				Projects: []config.ProjectEntry{
					{Name: "b", Path: "/b"},
				},
			}
			got := config.Merge(base, ov).Projects
			Expect(got).To(HaveLen(2))
			Expect(got[0].Name).To(Equal("a"))
			Expect(got[1].Name).To(Equal("b"))
		})

		It("merges existing projects field-by-field", func() {
			base := config.Config{
				Projects: []config.ProjectEntry{
					{Name: "orchard", Path: "/base", DefaultTool: "claude"},
				},
			}
			ov := config.Config{
				Projects: []config.ProjectEntry{
					{Name: "orchard", DefaultTool: "codex"},
				},
			}
			got := config.Merge(base, ov).Projects
			Expect(got).To(HaveLen(1))
			Expect(got[0].Path).To(Equal("/base"))        // unchanged
			Expect(got[0].DefaultTool).To(Equal("codex")) // overridden
		})
	})

	Describe("FindProjectByName", func() {
		It("returns (entry, true) when the name matches", func() {
			cfg := config.Config{Projects: []config.ProjectEntry{
				{Name: "orchard", Path: "/tmp/orchard"},
			}}
			got, ok := cfg.FindProjectByName("orchard")
			Expect(ok).To(BeTrue())
			Expect(got.Path).To(Equal("/tmp/orchard"))
		})

		It("returns (zero, false) for a miss", func() {
			cfg := config.Config{Projects: []config.ProjectEntry{
				{Name: "orchard", Path: "/tmp/orchard"},
			}}
			_, ok := cfg.FindProjectByName("missing")
			Expect(ok).To(BeFalse())
		})
	})

	Describe("ResolvedPath", func() {
		It("expands a leading tilde", func() {
			p := config.ProjectEntry{Name: "x", Path: "~/Projects/demo"}
			got, err := p.ResolvedPath()
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(ContainSubstring("Projects/demo"))
		})

		It("passes absolute paths through", func() {
			p := config.ProjectEntry{Name: "x", Path: "/tmp/demo"}
			got, err := p.ResolvedPath()
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal("/tmp/demo"))
		})
	})
})
