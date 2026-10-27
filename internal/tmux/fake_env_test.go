package tmux_test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/tmux"
)

var _ = Describe("Fake global environment and messages", func() {
	var (
		ctx context.Context
		fk  *tmux.Fake
	)

	BeforeEach(func() {
		ctx = context.Background()
		fk = tmux.NewFake()
		Expect(fk.NewSession(ctx, tmux.NewSessionOptions{Name: "mos_1"})).To(Succeed())
	})

	It("records and unsets global environment variables", func() {
		Expect(fk.SetGlobalEnv(ctx, "MOSAIC_OUTER_TMUX", "/tmp/sock,1,0")).To(Succeed())
		Expect(fk.GlobalEnv()).To(HaveKeyWithValue("MOSAIC_OUTER_TMUX", "/tmp/sock,1,0"))
		Expect(fk.SetGlobalEnv(ctx, "MOSAIC_OUTER_TMUX", "")).To(Succeed())
		Expect(fk.GlobalEnv()).NotTo(HaveKey("MOSAIC_OUTER_TMUX"))
	})

	It("records messages for known sessions and drops the rest", func() {
		Expect(fk.DisplayMessage(ctx, "mos_1", "hello", 5*time.Second)).To(Succeed())
		Expect(fk.DisplayMessage(ctx, "mos_missing", "ignored", time.Second)).To(Succeed())
		msgs := fk.Messages()
		Expect(msgs).To(HaveLen(1))
		Expect(msgs[0]).To(Equal(tmux.FakeMessage{Target: "mos_1", Text: "hello", Delay: 5 * time.Second}))
	})

	It("honours FailOnce for both operations", func() {
		fk.FailOnce("setenv", errors.New("boom"))
		Expect(fk.SetGlobalEnv(ctx, "X", "1")).To(MatchError("boom"))
		Expect(fk.SetGlobalEnv(ctx, "X", "1")).To(Succeed())
		fk.FailOnce("message", errors.New("boom"))
		Expect(fk.DisplayMessage(ctx, "mos_1", "x", 0)).To(MatchError("boom"))
		Expect(fk.DisplayMessage(ctx, "mos_1", "x", 0)).To(Succeed())
	})
})
