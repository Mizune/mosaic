package config_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/config"
)

var _ = Describe("DockerSection", func() {
	Describe("defaults", func() {
		It("enables auth mounts and auto_cleanup by default", func() {
			d := config.Default().Docker
			Expect(config.BoolOr(d.MountClaude, false)).To(BeTrue())
			Expect(config.BoolOr(d.MountGh, false)).To(BeTrue())
			Expect(config.BoolOr(d.MountSSH, false)).To(BeTrue())
			Expect(config.BoolOr(d.AutoCleanup, false)).To(BeTrue())
			// No image is shipped: users supply their own.
			Expect(d.Image).To(BeEmpty())
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

		It("parses the full [docker] table", func() {
			global := writeFile("global.toml", `
[docker]
image = "ghcr.io/mosaic/sandbox:1.2.3"
extra_args = ["--cpus=2", "--memory=4g"]
default_mounts = ["~/work:/work:ro"]
mount_ssh = false
firewall_allowlist = ["api.github.com", "api.anthropic.com"]
`)
			res, err := config.Load(config.LoadOptions{GlobalPath: global})
			Expect(err).NotTo(HaveOccurred())

			d := res.Config.Docker
			Expect(d.Image).To(Equal("ghcr.io/mosaic/sandbox:1.2.3"))
			Expect(d.ExtraArgs).To(Equal([]string{"--cpus=2", "--memory=4g"}))
			Expect(d.DefaultMounts).To(Equal([]string{"~/work:/work:ro"}))
			// Explicit mount_ssh = false wins over the default; the
			// other toggles are not in the override so they inherit
			// the defaults (MountClaude/MountGh/AutoCleanup = true).
			Expect(d.MountSSH).NotTo(BeNil())
			Expect(*d.MountSSH).To(BeFalse())
			Expect(config.BoolOr(d.MountClaude, false)).To(BeTrue())
			Expect(config.BoolOr(d.MountGh, false)).To(BeTrue())
			Expect(config.BoolOr(d.AutoCleanup, false)).To(BeTrue())
			Expect(d.FirewallAllowlist).To(ConsistOf("api.github.com", "api.anthropic.com"))
		})

		It("leaves the defaults intact when no [docker] block is set", func() {
			global := writeFile("global.toml", `
[mosaic]
default_tool = "claude"
`)
			res, err := config.Load(config.LoadOptions{GlobalPath: global})
			Expect(err).NotTo(HaveOccurred())

			d := res.Config.Docker
			Expect(config.BoolOr(d.MountClaude, false)).To(BeTrue())
			Expect(config.BoolOr(d.MountGh, false)).To(BeTrue())
			Expect(config.BoolOr(d.MountSSH, false)).To(BeTrue())
			Expect(config.BoolOr(d.AutoCleanup, false)).To(BeTrue())
		})

		It("distinguishes an explicit false from an unset field", func() {
			// The key invariant of the *bool representation: a
			// project override that only disables one mount does
			// not blow the other mounts back to false.
			global := writeFile("global.toml", `
[docker]
mount_ssh = false
`)
			res, err := config.Load(config.LoadOptions{GlobalPath: global})
			Expect(err).NotTo(HaveOccurred())

			d := res.Config.Docker
			Expect(d.MountSSH).NotTo(BeNil())
			Expect(*d.MountSSH).To(BeFalse())
			Expect(config.BoolOr(d.MountClaude, false)).To(BeTrue())
			Expect(config.BoolOr(d.MountGh, false)).To(BeTrue())
			Expect(config.BoolOr(d.AutoCleanup, false)).To(BeTrue())
		})
	})

	Describe("per-project sandbox opt-in", func() {
		var dir string
		BeforeEach(func() { dir = GinkgoT().TempDir() })

		It("loads the sandbox field on a [[project]] entry", func() {
			path := filepath.Join(dir, "global.toml")
			Expect(os.WriteFile(path, []byte(`
[[project]]
name = "foo"
path = "~/Projects/foo"
sandbox = "docker"
`), 0o644)).To(Succeed())

			res, err := config.Load(config.LoadOptions{GlobalPath: path})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Config.Projects).To(HaveLen(1))
			Expect(res.Config.Projects[0].Sandbox).To(Equal("docker"))
		})
	})
})
