package store_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/store"
)

var _ = Describe("ConductorsRepo", func() {
	var (
		ctx context.Context
		st  *store.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir := GinkgoT().TempDir()
		var err error
		st, err = store.Open(filepath.Join(dir, "state.db"))
		Expect(err).NotTo(HaveOccurred())
		Expect(st.Migrate(ctx)).To(Succeed())
		DeferCleanup(st.Close)
	})

	Describe("Insert / GetByID / SetStatus", func() {
		It("round-trips a conductor row", func() {
			c, err := st.Conductors().Insert(ctx, store.NewConductor{
				Name:        "refactor-auth",
				ProjectPath: "/tmp/proj",
				PlanPath:    "/tmp/proj/plan.toml",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(c.ID).NotTo(BeEmpty())
			Expect(c.Status).To(Equal(string(store.ConductorDraft)))

			Expect(st.Conductors().SetStatus(ctx, c.ID, store.ConductorRunning)).To(Succeed())

			got, err := st.Conductors().GetByID(ctx, c.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(string(store.ConductorRunning)))
		})
	})

	Describe("InsertTask / SetTaskStatus / Dependencies", func() {
		It("encodes and decodes the depends_on list", func() {
			c, _ := st.Conductors().Insert(ctx, store.NewConductor{
				Name: "x", ProjectPath: "/tmp",
			})
			t, err := st.Conductors().InsertTask(ctx, store.NewConductorTask{
				ConductorID: c.ID,
				Name:        "child",
				DependsOn:   []string{"a", "b"},
				Prompt:      "do thing",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(t.Dependencies()).To(Equal([]string{"a", "b"}))

			Expect(st.Conductors().SetTaskStatus(ctx, t.ID, store.TaskRunning)).To(Succeed())
			tasks, err := st.Conductors().ListTasks(ctx, c.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks).To(HaveLen(1))
			Expect(tasks[0].Status).To(Equal(string(store.TaskRunning)))
			Expect(tasks[0].StartedAt.Valid).To(BeTrue())
		})
	})

	Describe("Messages: PostMessage / Inbox / MarkDelivered", func() {
		It("delivers messages to a single recipient session", func() {
			parent, _ := st.Sessions().Insert(ctx, store.NewSession{
				Name: "p", WorktreePath: "/tmp/p",
			})
			recipient, _ := st.Sessions().Insert(ctx, store.NewSession{
				Name: "r", WorktreePath: "/tmp/r",
			})

			_, err := st.Conductors().PostMessage(ctx, store.NewMessage{
				FromSessionID: parent.ID,
				ToSessionID:   recipient.ID,
				Kind:          "direct",
				Body:          "hello",
			})
			Expect(err).NotTo(HaveOccurred())

			inbox, err := st.Conductors().Inbox(ctx, recipient.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(inbox).To(HaveLen(1))
			Expect(inbox[0].Body).To(Equal("hello"))

			Expect(st.Conductors().MarkDelivered(ctx, recipient.ID)).To(Succeed())
			inbox, _ = st.Conductors().Inbox(ctx, recipient.ID)
			Expect(inbox).To(BeEmpty())
		})
	})
})
