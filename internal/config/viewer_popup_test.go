package config_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/config"
)

var _ = Describe("ViewerSection attached-session keys", func() {
	It("defaults to a popup viewer with tmux notifications on", func() {
		v := config.Default().Viewer
		Expect(v.TmuxViewer).To(Equal("popup"))
		Expect(config.BoolOr(v.NotifyTmux, false)).To(BeTrue())
	})

	It("lets an override switch to split and turn notifications off", func() {
		ov := config.Config{Viewer: config.ViewerSection{TmuxViewer: "split", NotifyTmux: config.BoolPtr(false)}}
		out := config.Merge(config.Default(), ov)
		Expect(out.Viewer.TmuxViewer).To(Equal("split"))
		Expect(config.BoolOr(out.Viewer.NotifyTmux, true)).To(BeFalse())
	})

	It("inherits both when the override is silent", func() {
		out := config.Merge(config.Default(), config.Config{Viewer: config.ViewerSection{Focus: true}})
		Expect(out.Viewer.TmuxViewer).To(Equal("popup"))
		Expect(config.BoolOr(out.Viewer.NotifyTmux, false)).To(BeTrue())
	})
})
