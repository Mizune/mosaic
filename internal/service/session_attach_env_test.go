package service_test

import (
	"context"
	"errors"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tmux"
)

var _ = Describe("AttachCommand outer tmux hint", func() {
	var (
		ctx context.Context
		fk  *tmux.Fake
		svc *service.SessionService
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir := GinkgoT().TempDir()
		st, err := store.Open(filepath.Join(dir, "state.db"))
		Expect(err).NotTo(HaveOccurred())
		Expect(st.Migrate(ctx)).To(Succeed())
		DeferCleanup(st.Close)
		fk = tmux.NewFake()
		b := bus.New()
		DeferCleanup(b.Close)
		svc = service.NewSessionService(st, fk, b, nil)
		_, err = svc.Create(ctx, service.CreateInput{Name: "attached", WorktreePath: GinkgoT().TempDir()})
		Expect(err).NotTo(HaveOccurred())
	})

	It("publishes the TUI's tmux to the mosaic server on attach", func() {
		svc.Cfg.OuterTmux = "/tmp/tmux-501/default,100,0"
		_, err := svc.AttachCommand(ctx, "attached")
		Expect(err).NotTo(HaveOccurred())
		Expect(fk.GlobalEnv()).To(HaveKeyWithValue("MOSAIC_OUTER_TMUX", "/tmp/tmux-501/default,100,0"))
	})

	It("unsets the hint when the TUI runs outside tmux", func() {
		Expect(fk.SetGlobalEnv(ctx, "MOSAIC_OUTER_TMUX", "stale")).To(Succeed())
		svc.Cfg.OuterTmux = ""
		_, err := svc.AttachCommand(ctx, "attached")
		Expect(err).NotTo(HaveOccurred())
		Expect(fk.GlobalEnv()).NotTo(HaveKey("MOSAIC_OUTER_TMUX"))
	})

	It("still attaches when the environment write fails", func() {
		fk.FailOnce("setenv", errors.New("boom"))
		cmd, err := svc.AttachCommand(ctx, "attached")
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd).NotTo(BeNil())
	})
})
