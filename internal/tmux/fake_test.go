package tmux_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/tmux"
)

var _ = Describe("Fake", func() {
	var (
		ctx context.Context
		fk  *tmux.Fake
	)

	BeforeEach(func() {
		ctx = context.Background()
		fk = tmux.NewFake()
	})

	It("creates and lists sessions", func() {
		Expect(fk.NewSession(ctx, tmux.NewSessionOptions{
			Name: "mos_a", WorkingDir: "/tmp", Command: "bash",
		})).To(Succeed())
		Expect(fk.NewSession(ctx, tmux.NewSessionOptions{
			Name: "mos_b",
		})).To(Succeed())

		list, err := fk.List(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(ConsistOf("mos_a", "mos_b"))
	})

	It("rejects duplicate session names", func() {
		Expect(fk.NewSession(ctx, tmux.NewSessionOptions{Name: "mos_a"})).To(Succeed())
		Expect(fk.NewSession(ctx, tmux.NewSessionOptions{Name: "mos_a"})).To(HaveOccurred())
	})

	It("reports HasSession correctly", func() {
		Expect(fk.NewSession(ctx, tmux.NewSessionOptions{Name: "mos_x"})).To(Succeed())

		ok, err := fk.HasSession(ctx, "mos_x")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		ok, err = fk.HasSession(ctx, "mos_missing")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
	})

	It("removes sessions on Kill, idempotently", func() {
		Expect(fk.NewSession(ctx, tmux.NewSessionOptions{Name: "mos_a"})).To(Succeed())
		Expect(fk.Kill(ctx, "mos_a")).To(Succeed())
		Expect(fk.Kill(ctx, "mos_a")).To(Succeed()) // already gone

		ok, _ := fk.HasSession(ctx, "mos_a")
		Expect(ok).To(BeFalse())
	})

	It("returns the seeded capture body", func() {
		Expect(fk.NewSession(ctx, tmux.NewSessionOptions{Name: "mos_a"})).To(Succeed())
		fk.SetCapture("mos_a", "hello\nworld\n")

		out, err := fk.Capture(ctx, "mos_a", tmux.CaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("hello\nworld\n"))
	})

	It("returns errTmuxNoSession when capturing a missing session", func() {
		_, err := fk.Capture(ctx, "mos_missing", tmux.CaptureOptions{})
		Expect(tmux.IsNotFound(err)).To(BeTrue())
	})

	It("supports FailOnce for scheduled failures", func() {
		fk.FailOnce("new", errors.New("boom"))
		Expect(fk.NewSession(ctx, tmux.NewSessionOptions{Name: "x"})).To(MatchError("boom"))
		// Subsequent calls succeed.
		Expect(fk.NewSession(ctx, tmux.NewSessionOptions{Name: "x"})).To(Succeed())
	})
})

var _ = Describe("SessionName", func() {
	It("prepends the mos_ prefix", func() {
		Expect(tmux.SessionName("01HVZ")).To(Equal("mos_01HVZ"))
	})
})
