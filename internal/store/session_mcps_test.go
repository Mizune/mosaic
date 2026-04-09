package store_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/store"
)

var _ = Describe("SessionMCPsRepo", func() {
	var (
		ctx  context.Context
		st   *store.Store
		sess store.Session
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir := GinkgoT().TempDir()
		var err error
		st, err = store.Open(filepath.Join(dir, "state.db"))
		Expect(err).NotTo(HaveOccurred())
		Expect(st.Migrate(ctx)).To(Succeed())
		DeferCleanup(st.Close)

		sess, err = st.Sessions().Insert(ctx, store.NewSession{
			Name: "s1", WorktreePath: "/tmp/s1",
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("attaches and detaches MCPs", func() {
		Expect(st.SessionMCPs().Attach(ctx, sess.ID, "github")).To(Succeed())
		Expect(st.SessionMCPs().Attach(ctx, sess.ID, "codex")).To(Succeed())

		names, err := st.SessionMCPs().AttachedNames(ctx, sess.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(names).To(HaveKey("github"))
		Expect(names).To(HaveKey("codex"))

		Expect(st.SessionMCPs().Detach(ctx, sess.ID, "github")).To(Succeed())

		names, err = st.SessionMCPs().AttachedNames(ctx, sess.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(names).NotTo(HaveKey("github"))
		Expect(names).To(HaveKey("codex"))
	})

	It("is idempotent on double attach", func() {
		Expect(st.SessionMCPs().Attach(ctx, sess.ID, "github")).To(Succeed())
		Expect(st.SessionMCPs().Attach(ctx, sess.ID, "github")).To(Succeed())

		names, err := st.SessionMCPs().AttachedNames(ctx, sess.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(names).To(HaveLen(1))
	})

	It("returns false for a missing attachment", func() {
		got, err := st.SessionMCPs().IsAttached(ctx, sess.ID, "nope")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeFalse())
	})

	It("detach of a missing row is a silent no-op", func() {
		Expect(st.SessionMCPs().Detach(ctx, sess.ID, "never-attached")).To(Succeed())
	})

	It("cascades on session delete", func() {
		Expect(st.SessionMCPs().Attach(ctx, sess.ID, "github")).To(Succeed())
		Expect(st.Sessions().Delete(ctx, sess.ID)).To(Succeed())

		names, err := st.SessionMCPs().AttachedNames(ctx, sess.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(names).To(BeEmpty())
	})
})
