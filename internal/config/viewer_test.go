package config_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/config"
)

var _ = Describe("ViewerSection", func() {
	Describe("defaults", func() {
		It("enables the viewer in the dock with the jsdelivr mermaid build", func() {
			v := config.Default().Viewer
			Expect(config.BoolOr(v.Enabled, false)).To(BeTrue())
			Expect(v.AutoOpen).To(BeFalse())
			Expect(v.Focus).To(BeFalse())
			Expect(v.Placement).To(Equal("dock"))
			Expect(v.MermaidJSURL).To(Equal(config.DefaultMermaidJSURL))
			Expect(v.MermaidJSURL).To(Equal(
				"https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"))
		})
	})

	Describe("merge", func() {
		It("keeps the defaults when the override has no [viewer] block", func() {
			out := config.Merge(config.Default(), config.Config{})
			Expect(config.BoolOr(out.Viewer.Enabled, false)).To(BeTrue())
			Expect(out.Viewer.Placement).To(Equal("dock"))
			Expect(out.Viewer.MermaidJSURL).To(Equal(config.DefaultMermaidJSURL))
		})

		It("lets a project override replace placement and the script URL", func() {
			ov := config.Config{Viewer: config.ViewerSection{
				Placement:    "right",
				MermaidJSURL: "file:///opt/mermaid/mermaid.min.js",
			}}
			out := config.Merge(config.Default(), ov)
			Expect(out.Viewer.Placement).To(Equal("right"))
			Expect(out.Viewer.MermaidJSURL).To(Equal("file:///opt/mermaid/mermaid.min.js"))
			// Keys the override does not mention inherit the base.
			Expect(config.BoolOr(out.Viewer.Enabled, false)).To(BeTrue())
			Expect(out.Viewer.AutoOpen).To(BeFalse())
		})

		It("honours enabled = false from the override", func() {
			ov := config.Config{Viewer: config.ViewerSection{Enabled: config.BoolPtr(false)}}
			out := config.Merge(config.Default(), ov)
			Expect(out.Viewer.Enabled).NotTo(BeNil())
			Expect(*out.Viewer.Enabled).To(BeFalse())
			// Disabling must not disturb the other keys.
			Expect(out.Viewer.Placement).To(Equal("dock"))
		})

		It("lets true bools win without letting false reset the base", func() {
			base := config.Default()
			base.Viewer.AutoOpen = true
			out := config.Merge(base, config.Config{Viewer: config.ViewerSection{Focus: true}})
			Expect(out.Viewer.AutoOpen).To(BeTrue())
			Expect(out.Viewer.Focus).To(BeTrue())
		})

		It("never mutates the inputs", func() {
			base := config.Default()
			ov := config.Config{Viewer: config.ViewerSection{Placement: "down"}}
			_ = config.Merge(base, ov)
			Expect(base.Viewer.Placement).To(Equal("dock"))
			Expect(ov.Viewer.Placement).To(Equal("down"))
		})
	})

	Describe("loading from TOML", func() {
		var dir string
		BeforeEach(func() { dir = GinkgoT().TempDir() })

		writeFile := func(name, body string) string {
			p := filepath.Join(dir, name)
			Expect(os.WriteFile(p, []byte(body), 0o644)).To(Succeed())
			return p
		}

		It("applies a project placement override on top of the global file", func() {
			global := writeFile("global.toml", `
[viewer]
auto_open = true
`)
			project := writeFile("project.toml", `
[viewer]
placement = "down"
`)
			res, err := config.Load(config.LoadOptions{
				GlobalPath:  global,
				ProjectPath: project,
			})
			Expect(err).NotTo(HaveOccurred())

			v := res.Config.Viewer
			Expect(v.Placement).To(Equal("down"))
			Expect(v.AutoOpen).To(BeTrue())
			Expect(config.BoolOr(v.Enabled, false)).To(BeTrue())
			Expect(v.MermaidJSURL).To(Equal(config.DefaultMermaidJSURL))
		})

		It("distinguishes an explicit enabled = false from an unset key", func() {
			project := writeFile("project.toml", `
[viewer]
enabled = false
`)
			res, err := config.Load(config.LoadOptions{ProjectPath: project})
			Expect(err).NotTo(HaveOccurred())

			v := res.Config.Viewer
			Expect(v.Enabled).NotTo(BeNil())
			Expect(config.BoolOr(v.Enabled, true)).To(BeFalse())
			Expect(v.Placement).To(Equal("dock"))
		})

		It("leaves the defaults intact when no [viewer] block is set", func() {
			global := writeFile("global.toml", `
[mosaic]
default_tool = "claude"
`)
			res, err := config.Load(config.LoadOptions{GlobalPath: global})
			Expect(err).NotTo(HaveOccurred())
			Expect(config.BoolOr(res.Config.Viewer.Enabled, false)).To(BeTrue())
			Expect(res.Config.Viewer.Placement).To(Equal("dock"))
		})
	})
})
