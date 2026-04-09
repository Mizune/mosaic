package mosaicpath_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/mosaicpath"
)

var _ = Describe("Resolve", func() {
	var home string

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		// Wipe any influence from the host environment so each spec
		// observes a clean slate.
		for _, v := range []string{
			"MOSAIC_DATA_DIR", "MOSAIC_CONFIG_DIR",
			"XDG_DATA_HOME", "XDG_CONFIG_HOME",
		} {
			GinkgoT().Setenv(v, "")
			Expect(os.Unsetenv(v)).To(Succeed())
		}
	})

	Context("with no environment overrides", func() {
		It("places the data directory under the home directory", func() {
			p, err := mosaicpath.Resolve(home)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.DataDir).To(Equal(filepath.Join(home, ".mosaic")))
		})

		It("places the config directory under ~/.config/mosaic", func() {
			p, err := mosaicpath.Resolve(home)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.ConfigDir).To(Equal(filepath.Join(home, ".config", "mosaic")))
			Expect(p.GlobalConfigFile).To(HaveSuffix("config.toml"))
		})

		It("derives logs, run, sockets and profile paths from the data dir", func() {
			p, err := mosaicpath.Resolve(home)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.LogsDir).To(Equal(filepath.Join(p.DataDir, "logs")))
			Expect(p.RunDir).To(Equal(filepath.Join(p.DataDir, "run")))
			Expect(p.SocketsDir).To(Equal(filepath.Join(p.DataDir, "run", "sockets")))
			Expect(p.ProfilesDir).To(Equal(filepath.Join(p.DataDir, "profiles")))
			Expect(p.LogFile).To(Equal(filepath.Join(p.LogsDir, "mosaic.log")))
		})

		It("uses 'mosaic' as the tmux socket name", func() {
			p, err := mosaicpath.Resolve(home)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.TmuxSocketName).To(Equal("mosaic"))
		})
	})

	Context("when MOSAIC_DATA_DIR is set", func() {
		It("uses it verbatim", func() {
			GinkgoT().Setenv("MOSAIC_DATA_DIR", "/tmp/m-data")
			p, err := mosaicpath.Resolve(home)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.DataDir).To(Equal("/tmp/m-data"))
		})

		It("expands a leading tilde to the home directory", func() {
			GinkgoT().Setenv("MOSAIC_DATA_DIR", "~/custom-data")
			p, err := mosaicpath.Resolve(home)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.DataDir).To(Equal(filepath.Join(home, "custom-data")))
		})
	})

	Context("when XDG_DATA_HOME is set and MOSAIC_DATA_DIR is not", func() {
		It("nests the data directory under XDG_DATA_HOME/mosaic", func() {
			GinkgoT().Setenv("XDG_DATA_HOME", "/xdg/share")
			p, err := mosaicpath.Resolve(home)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.DataDir).To(Equal("/xdg/share/mosaic"))
		})
	})

	Context("when XDG_CONFIG_HOME is set", func() {
		It("nests the config directory under XDG_CONFIG_HOME/mosaic", func() {
			GinkgoT().Setenv("XDG_CONFIG_HOME", "/xdg/config")
			p, err := mosaicpath.Resolve(home)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.ConfigDir).To(Equal("/xdg/config/mosaic"))
		})
	})

	Context("MOSAIC_DATA_DIR has precedence over XDG_DATA_HOME", func() {
		It("prefers MOSAIC_DATA_DIR", func() {
			GinkgoT().Setenv("MOSAIC_DATA_DIR", "/explicit/data")
			GinkgoT().Setenv("XDG_DATA_HOME", "/xdg/share")
			p, err := mosaicpath.Resolve(home)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.DataDir).To(Equal("/explicit/data"))
		})
	})
})

var _ = Describe("Paths helpers", func() {
	var (
		home string
		p    mosaicpath.Paths
	)

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		var err error
		p, err = mosaicpath.Resolve(home)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("ProfileDir", func() {
		It("returns the named profile directory", func() {
			Expect(p.ProfileDir("work")).To(Equal(filepath.Join(p.ProfilesDir, "work")))
		})

		It("falls back to 'default' for an empty name", func() {
			Expect(p.ProfileDir("")).To(Equal(filepath.Join(p.ProfilesDir, "default")))
		})
	})

	Describe("StateDB", func() {
		It("places the SQLite database under the profile directory", func() {
			Expect(p.StateDB("work")).To(Equal(filepath.Join(p.ProfileDir("work"), "state.db")))
		})
	})

	Describe("EnsureAll", func() {
		It("creates every directory it tracks, idempotently", func() {
			Expect(p.EnsureAll()).To(Succeed())
			for _, dir := range []string{
				p.DataDir, p.ConfigDir, p.ProfilesDir, p.LogsDir, p.RunDir, p.SocketsDir,
			} {
				info, err := os.Stat(dir)
				Expect(err).NotTo(HaveOccurred(), "directory should exist: %s", dir)
				Expect(info.IsDir()).To(BeTrue())
			}
			// Calling it twice must not error.
			Expect(p.EnsureAll()).To(Succeed())
		})

		It("creates the runtime directory with restrictive permissions", func() {
			Expect(p.EnsureAll()).To(Succeed())
			info, err := os.Stat(p.RunDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm() & 0o077).To(Equal(os.FileMode(0)))
		})
	})

	Describe("EnsureProfile", func() {
		It("creates the profile directory", func() {
			Expect(p.EnsureProfile("work")).To(Succeed())
			info, err := os.Stat(p.ProfileDir("work"))
			Expect(err).NotTo(HaveOccurred())
			Expect(info.IsDir()).To(BeTrue())
		})
	})
})

var _ = Describe("ProjectConfigFile", func() {
	It("returns <project>/.mosaic/config.toml", func() {
		Expect(mosaicpath.ProjectConfigFile("/tmp/proj")).
			To(Equal("/tmp/proj/.mosaic/config.toml"))
	})
})
