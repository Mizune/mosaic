package store_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/store"
)

var _ = Describe("Migrate", func() {
	var (
		ctx context.Context
		s   *store.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir := GinkgoT().TempDir()
		var err error
		s, err = store.Open(filepath.Join(dir, "state.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
	})

	It("applies migration 0001 to a fresh database", func() {
		Expect(s.Migrate(ctx)).To(Succeed())

		applied, err := s.AppliedVersions(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(applied).To(HaveKey(1))
	})

	It("is idempotent on a second run", func() {
		Expect(s.Migrate(ctx)).To(Succeed())
		Expect(s.Migrate(ctx)).To(Succeed())

		var count int
		Expect(s.DB().GetContext(ctx, &count,
			`SELECT count(*) FROM schema_migrations WHERE version = 1`)).To(Succeed())
		Expect(count).To(Equal(1))
	})

	It("creates the sessions table after migrating", func() {
		Expect(s.Migrate(ctx)).To(Succeed())

		var name string
		Expect(s.DB().GetContext(ctx, &name,
			`SELECT name FROM sqlite_master WHERE type='table' AND name='sessions'`)).To(Succeed())
		Expect(name).To(Equal("sessions"))
	})
})
