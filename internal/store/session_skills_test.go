package store_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/store"
)

var _ = Describe("SessionSkillsRepo", func() {
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

	It("attaches, reports, and detaches", func() {
		Expect(st.SessionSkills().Attach(ctx, sess.ID, "commit")).To(Succeed())
		Expect(st.SessionSkills().Attach(ctx, sess.ID, "branch")).To(Succeed())

		names, err := st.SessionSkills().AttachedNames(ctx, sess.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(names).To(HaveKey("commit"))
		Expect(names).To(HaveKey("branch"))

		Expect(st.SessionSkills().Detach(ctx, sess.ID, "branch")).To(Succeed())
		names, _ = st.SessionSkills().AttachedNames(ctx, sess.ID)
		Expect(names).NotTo(HaveKey("branch"))
	})

	It("is idempotent on repeat attach", func() {
		Expect(st.SessionSkills().Attach(ctx, sess.ID, "commit")).To(Succeed())
		Expect(st.SessionSkills().Attach(ctx, sess.ID, "commit")).To(Succeed())
		names, _ := st.SessionSkills().AttachedNames(ctx, sess.ID)
		Expect(names).To(HaveLen(1))
	})

	It("cascades when the owning session is deleted", func() {
		Expect(st.SessionSkills().Attach(ctx, sess.ID, "commit")).To(Succeed())
		Expect(st.Sessions().Delete(ctx, sess.ID)).To(Succeed())
		names, _ := st.SessionSkills().AttachedNames(ctx, sess.ID)
		Expect(names).To(BeEmpty())
	})
})
