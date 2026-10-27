package store_test

import (
	"context"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/store"
)

var _ = Describe("ArtifactsRepo", func() {
	var (
		ctx    context.Context
		st     *store.Store
		parent store.Session
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir := GinkgoT().TempDir()
		var err error
		st, err = store.Open(filepath.Join(dir, "state.db"))
		Expect(err).NotTo(HaveOccurred())
		Expect(st.Migrate(ctx)).To(Succeed())
		DeferCleanup(st.Close)

		parent, err = st.Sessions().Insert(ctx, store.NewSession{
			Name: "parent", WorktreePath: "/tmp/p",
		})
		Expect(err).NotTo(HaveOccurred())
	})

	mermaid := func(sessionID, hash string, at time.Time) store.NewArtifact {
		return store.NewArtifact{
			SessionID: sessionID, Kind: "mermaid", Source: "transcript",
			Title: "graph TD", Content: "graph TD\nA-->B", ContentHash: hash, CreatedAt: at,
		}
	}

	Describe("Upsert", func() {
		It("inserts a new artifact and reports created", func() {
			at := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
			row, created, err := st.Artifacts().Upsert(ctx, mermaid(parent.ID, "h1", at))
			Expect(err).NotTo(HaveOccurred())
			Expect(created).To(BeTrue())
			Expect(row.ID).NotTo(BeEmpty())
			Expect(row.Content.Valid).To(BeTrue())
			Expect(row.CreatedAt).To(BeTemporally("==", at))
		})

		It("returns the existing row on duplicate (session, hash)", func() {
			a, created, err := st.Artifacts().Upsert(ctx, mermaid(parent.ID, "h1", time.Now()))
			Expect(err).NotTo(HaveOccurred())
			Expect(created).To(BeTrue())

			dup := mermaid(parent.ID, "h1", time.Now())
			dup.Title = "ignored"
			b, created, err := st.Artifacts().Upsert(ctx, dup)
			Expect(err).NotTo(HaveOccurred())
			Expect(created).To(BeFalse())
			Expect(b.ID).To(Equal(a.ID))
			Expect(b.Title).To(Equal("graph TD"))

			rows, err := st.Artifacts().ListBySession(ctx, parent.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
		})

		It("defaults created_at to now when zero", func() {
			row, _, err := st.Artifacts().Upsert(ctx, mermaid(parent.ID, "h2", time.Time{}))
			Expect(err).NotTo(HaveOccurred())
			Expect(row.CreatedAt).To(BeTemporally("~", time.Now(), 5*time.Second))
		})

		It("rejects missing required fields", func() {
			_, _, err := st.Artifacts().Upsert(ctx, store.NewArtifact{SessionID: parent.ID, Kind: "image"})
			Expect(err).To(HaveOccurred())
		})

		It("stores file artifacts without content", func() {
			row, created, err := st.Artifacts().Upsert(ctx, store.NewArtifact{
				SessionID: parent.ID, Kind: "image", Source: "transcript", AgentID: "agent-1",
				Title: "shot.png", Path: "/tmp/p/shot.png", ContentHash: "p1",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(created).To(BeTrue())
			Expect(row.Content.Valid).To(BeFalse())
			Expect(row.Path).To(Equal("/tmp/p/shot.png"))
			Expect(row.AgentID).To(Equal("agent-1"))
		})
	})

	Describe("ListBySession", func() {
		It("returns newest first and is scoped to the session", func() {
			other, err := st.Sessions().Insert(ctx, store.NewSession{Name: "other", WorktreePath: "/tmp/o"})
			Expect(err).NotTo(HaveOccurred())
			t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
			_, _, err = st.Artifacts().Upsert(ctx, mermaid(parent.ID, "old", t0))
			Expect(err).NotTo(HaveOccurred())
			_, _, err = st.Artifacts().Upsert(ctx, mermaid(parent.ID, "new", t0.Add(time.Minute)))
			Expect(err).NotTo(HaveOccurred())
			_, _, err = st.Artifacts().Upsert(ctx, mermaid(other.ID, "elsewhere", t0.Add(time.Hour)))
			Expect(err).NotTo(HaveOccurred())

			rows, err := st.Artifacts().ListBySession(ctx, parent.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(2))
			Expect(rows[0].ContentHash).To(Equal("new"))
			Expect(rows[1].ContentHash).To(Equal("old"))
		})
	})

	Describe("ListAll", func() {
		It("groups rows by session, newest first", func() {
			other, err := st.Sessions().Insert(ctx, store.NewSession{Name: "other", WorktreePath: "/tmp/o"})
			Expect(err).NotTo(HaveOccurred())
			t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
			for _, in := range []store.NewArtifact{
				mermaid(parent.ID, "a", t0), mermaid(parent.ID, "b", t0.Add(time.Second)), mermaid(other.ID, "c", t0),
			} {
				_, _, err := st.Artifacts().Upsert(ctx, in)
				Expect(err).NotTo(HaveOccurred())
			}
			all, err := st.Artifacts().ListAll(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(all).To(HaveLen(2))
			Expect(all[parent.ID]).To(HaveLen(2))
			Expect(all[parent.ID][0].ContentHash).To(Equal("b"))
			Expect(all[other.ID]).To(HaveLen(1))
		})

		It("returns an empty map when there are no artifacts", func() {
			all, err := st.Artifacts().ListAll(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(all).To(BeEmpty())
		})
	})

	Describe("Delete and cascade", func() {
		It("deletes a single row", func() {
			row, _, err := st.Artifacts().Upsert(ctx, mermaid(parent.ID, "h1", time.Now()))
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Artifacts().Delete(ctx, row.ID)).To(Succeed())
			_, err = st.Artifacts().GetByHash(ctx, parent.ID, "h1")
			Expect(err).To(MatchError(store.ErrArtifactNotFound))
			Expect(st.Artifacts().Delete(ctx, row.ID)).To(Succeed())
		})

		It("removes rows when the session is deleted", func() {
			_, _, err := st.Artifacts().Upsert(ctx, mermaid(parent.ID, "h1", time.Now()))
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Sessions().Delete(ctx, parent.ID)).To(Succeed())
			rows, err := st.Artifacts().ListBySession(ctx, parent.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeEmpty())
		})
	})
})
