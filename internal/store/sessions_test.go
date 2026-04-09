package store_test

import (
	"context"
	"errors"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/store"
)

var _ = Describe("SessionsRepo", func() {
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
		Expect(s.Migrate(ctx)).To(Succeed())
		DeferCleanup(s.Close)
	})

	Describe("Insert", func() {
		It("rejects an empty name", func() {
			_, err := s.Sessions().Insert(ctx, store.NewSession{
				WorktreePath: "/tmp/x",
			})
			Expect(err).To(HaveOccurred())
		})

		It("rejects an empty worktree path", func() {
			_, err := s.Sessions().Insert(ctx, store.NewSession{Name: "x"})
			Expect(err).To(HaveOccurred())
		})

		It("returns the inserted session populated with id and timestamps", func() {
			out, err := s.Sessions().Insert(ctx, store.NewSession{
				Name:         "feat-auth",
				ProjectPath:  "/tmp/orchard",
				WorktreePath: "/tmp/orchard/.worktrees/feat-auth",
				Branch:       "feat-auth",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out.ID).NotTo(BeEmpty())
			Expect(out.TmuxSession).To(Equal("mos_" + out.ID))
			Expect(out.Status).To(Equal(string(store.StatusIdle)))
			Expect(out.Tool).To(Equal(string(store.ToolClaude)))
			Expect(out.CreatedAt.IsZero()).To(BeFalse())
		})

		It("rejects duplicate names", func() {
			_, err := s.Sessions().Insert(ctx, store.NewSession{
				Name: "dup", WorktreePath: "/tmp/x",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Sessions().Insert(ctx, store.NewSession{
				Name: "dup", WorktreePath: "/tmp/y",
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Get / List", func() {
		BeforeEach(func() {
			for _, name := range []string{"alpha", "beta", "gamma"} {
				_, err := s.Sessions().Insert(ctx, store.NewSession{
					Name: name, WorktreePath: "/tmp/" + name,
				})
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("retrieves a session by name", func() {
			got, err := s.Sessions().GetByName(ctx, "beta")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Name).To(Equal("beta"))
			Expect(got.WorktreePath).To(Equal("/tmp/beta"))
		})

		It("returns ErrSessionNotFound for a missing name", func() {
			_, err := s.Sessions().GetByName(ctx, "nonexistent")
			Expect(errors.Is(err, store.ErrSessionNotFound)).To(BeTrue())
		})

		It("lists every inserted session", func() {
			list, err := s.Sessions().List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(3))
			names := make([]string, 0, len(list))
			for _, sess := range list {
				names = append(names, sess.Name)
			}
			Expect(names).To(ContainElements("alpha", "beta", "gamma"))
		})
	})

	Describe("UpdateStatus", func() {
		It("updates the status and refreshes last_active_at", func() {
			created, err := s.Sessions().Insert(ctx, store.NewSession{
				Name: "tracked", WorktreePath: "/tmp/tracked",
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Sessions().UpdateStatus(ctx, created.ID, store.StatusRunning)).To(Succeed())

			got, err := s.Sessions().GetByID(ctx, created.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(string(store.StatusRunning)))
			Expect(got.LastActiveAt).NotTo(BeZero())
		})

		It("returns ErrSessionNotFound for an unknown id", func() {
			err := s.Sessions().UpdateStatus(ctx, "nonexistent-id", store.StatusError)
			Expect(errors.Is(err, store.ErrSessionNotFound)).To(BeTrue())
		})
	})

	Describe("Delete", func() {
		It("removes the row", func() {
			created, err := s.Sessions().Insert(ctx, store.NewSession{
				Name: "doomed", WorktreePath: "/tmp/doomed",
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Sessions().Delete(ctx, created.ID)).To(Succeed())

			_, err = s.Sessions().GetByID(ctx, created.ID)
			Expect(errors.Is(err, store.ErrSessionNotFound)).To(BeTrue())
		})
	})
})
