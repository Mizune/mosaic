package config_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/config"
)

var _ = Describe("Load", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	writeFile := func(name, body string) string {
		p := filepath.Join(dir, name)
		Expect(os.WriteFile(p, []byte(body), 0o644)).To(Succeed())
		return p
	}

	It("returns the defaults when no files exist", func() {
		res, err := config.Load(config.LoadOptions{
			GlobalPath:  filepath.Join(dir, "missing-global.toml"),
			ProjectPath: filepath.Join(dir, "missing-project.toml"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Config.Mosaic.DefaultTool).To(Equal("claude"))
		Expect(res.Sources).To(BeEmpty())
	})

	It("layers the global config on top of the defaults", func() {
		global := writeFile("global.toml", `
[mosaic]
default_tool = "shell"
log_level = "debug"
`)
		res, err := config.Load(config.LoadOptions{GlobalPath: global})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Config.Mosaic.DefaultTool).To(Equal("shell"))
		Expect(res.Config.Mosaic.LogLevel).To(Equal("debug"))
		Expect(res.Sources).To(ConsistOf(global))
	})

	It("layers the project config on top of the global config", func() {
		global := writeFile("global.toml", `
[mosaic]
default_tool = "shell"
`)
		project := writeFile("project.toml", `
[mosaic]
default_tool = "claude"
`)
		res, err := config.Load(config.LoadOptions{
			GlobalPath:  global,
			ProjectPath: project,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Config.Mosaic.DefaultTool).To(Equal("claude"))
		Expect(res.Sources).To(Equal([]string{global, project}))
	})

	It("returns an error when a file is unparseable", func() {
		bad := writeFile("global.toml", "not = valid toml = at all")
		_, err := config.Load(config.LoadOptions{GlobalPath: bad})
		Expect(err).To(HaveOccurred())
	})
})
