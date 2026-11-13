package tmux_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/tmux"
)

var _ = Describe("DefaultConfigBody", func() {
	It("binds prefix+O to a popup by default", func() {
		body := tmux.DefaultConfigBody(tmux.ConfigOptions{})
		Expect(body).To(ContainSubstring(`bind-key O display-popup -E -w 90% -h 90% -T " artifacts " -e MOSAIC_TMUX_POPUP=1 "mosaic artifacts"`))
		Expect(body).NotTo(ContainSubstring(`bind-key O split-window`))
	})

	It("binds prefix+O to a split when asked", func() {
		body := tmux.DefaultConfigBody(tmux.ConfigOptions{ArtifactViewer: tmux.ArtifactViewerSplit})
		Expect(body).To(ContainSubstring(`bind-key O split-window -h "mosaic artifacts"`))
		Expect(body).NotTo(ContainSubstring("display-popup"))
	})

	It("keeps the existing bindings and enables passthrough everywhere", func() {
		body := tmux.DefaultConfigBody(tmux.ConfigOptions{})
		Expect(body).To(ContainSubstring("bind-key -n C-q detach-client"))
		Expect(body).To(ContainSubstring(`bind-key D split-window -h "mosaic diff"`))
		Expect(body).To(ContainSubstring("set -g allow-passthrough all"))
		Expect(body).NotTo(ContainSubstring("set -g allow-passthrough on"))
		Expect(body).To(ContainSubstring("source-file -q ~/.mosaic/tmux-overlay.conf"))
	})

	It("writes the rendered body with EnsureConfigFile", func() {
		path := filepath.Join(GinkgoT().TempDir(), "nested", "tmux.conf")
		Expect(tmux.EnsureConfigFile(path, tmux.ConfigOptions{ArtifactViewer: "split"})).To(Succeed())
		data, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal(tmux.DefaultConfigBody(tmux.ConfigOptions{ArtifactViewer: "split"})))
	})
})
