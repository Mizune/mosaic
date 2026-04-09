package store_test

import (
	"context"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/store"
)

var _ = Describe("SubagentsRepo", func() {
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

	Describe("Upsert", func() {
		It("inserts a new sub-agent", func() {
			sub, err := st.Subagents().Upsert(ctx, store.NewSubagent{
				ParentSessionID: parent.ID,
				ClaudeSessionID: "abc",
				AgentID:         "deadbeef",
				SubagentType:    "general-purpose",
				Description:     "refactor router",
				TranscriptPath:  "/tmp/abc/subagents/agent-deadbeef.jsonl",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(sub.ID).NotTo(BeEmpty())
			Expect(sub.Status).To(Equal(string(store.SubagentSpawning)))
		})

		It("returns the existing row on duplicate (parent, agent)", func() {
			a, err := st.Subagents().Upsert(ctx, store.NewSubagent{
				ParentSessionID: parent.ID, AgentID: "x",
				SubagentType: "general-purpose", TranscriptPath: "/p",
			})
			Expect(err).NotTo(HaveOccurred())

			b, err := st.Subagents().Upsert(ctx, store.NewSubagent{
				ParentSessionID: parent.ID, AgentID: "x",
				SubagentType: "ignored", TranscriptPath: "/p",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(b.ID).To(Equal(a.ID))
		})
	})

	Describe("UpdateRunningTool", func() {
		It("records the current tool and timestamp", func() {
			sub, _ := st.Subagents().Upsert(ctx, store.NewSubagent{
				ParentSessionID: parent.ID, AgentID: "x",
				SubagentType: "general-purpose", TranscriptPath: "/p",
			})

			now := time.Now()
			Expect(st.Subagents().UpdateRunningTool(ctx, sub.ID, "Read", now)).To(Succeed())

			got, err := st.Subagents().GetByAgentID(ctx, parent.ID, "x")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.CurrentTool.String).To(Equal("Read"))
			Expect(got.Status).To(Equal(string(store.SubagentRunning)))
		})

		It("clears the tool fields when name is empty", func() {
			sub, _ := st.Subagents().Upsert(ctx, store.NewSubagent{
				ParentSessionID: parent.ID, AgentID: "x",
				SubagentType: "general-purpose", TranscriptPath: "/p",
			})
			Expect(st.Subagents().UpdateRunningTool(ctx, sub.ID, "Read", time.Now())).To(Succeed())
			Expect(st.Subagents().UpdateRunningTool(ctx, sub.ID, "", time.Time{})).To(Succeed())

			got, err := st.Subagents().GetByAgentID(ctx, parent.ID, "x")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.CurrentTool.Valid).To(BeFalse())
		})
	})

	Describe("MarkCompleted", func() {
		It("flips the status to completed", func() {
			sub, _ := st.Subagents().Upsert(ctx, store.NewSubagent{
				ParentSessionID: parent.ID, AgentID: "x",
				SubagentType: "general-purpose", TranscriptPath: "/p",
			})
			Expect(st.Subagents().MarkCompleted(ctx, sub.ID)).To(Succeed())

			got, _ := st.Subagents().GetByAgentID(ctx, parent.ID, "x")
			Expect(got.Status).To(Equal(string(store.SubagentCompleted)))
			Expect(got.CompletedAt.Valid).To(BeTrue())
		})
	})

	Describe("ListByParent", func() {
		It("returns every sub-agent in started_at order", func() {
			for _, id := range []string{"a", "b", "c"} {
				_, _ = st.Subagents().Upsert(ctx, store.NewSubagent{
					ParentSessionID: parent.ID, AgentID: id,
					SubagentType: "general-purpose", TranscriptPath: "/p",
				})
			}
			out, err := st.Subagents().ListByParent(ctx, parent.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(HaveLen(3))
		})
	})
})

var _ = Describe("NotificationsRepo", func() {
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

	It("inserts a notification and lists it as unseen", func() {
		_, err := st.Notifications().Insert(ctx, store.NewNotification{
			SessionID: parent.ID,
			Source:    store.NotificationSourceHook,
			Agent:     "claude",
			Event:     "Notification",
			Title:     "Permission required",
			Body:      "Bash",
		})
		Expect(err).NotTo(HaveOccurred())

		list, err := st.Notifications().ListUnseen(ctx, parent.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(1))
		Expect(list[0].Title.String).To(Equal("Permission required"))
	})

	It("Dismiss clears unseen notifications", func() {
		for i := 0; i < 3; i++ {
			_, _ = st.Notifications().Insert(ctx, store.NewNotification{
				SessionID: parent.ID, Source: store.NotificationSourceHook,
			})
		}
		Expect(st.Notifications().Dismiss(ctx, parent.ID)).To(Succeed())
		list, err := st.Notifications().ListUnseen(ctx, parent.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(BeEmpty())
	})
})

var _ = Describe("Sessions.SetAttention", func() {
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

	It("flips the session into waiting_input and back", func() {
		Expect(st.Sessions().SetAttention(ctx, parent.ID,
			store.AttentionWaitingInput, "Permission required", "Bash", "hook")).To(Succeed())

		got, err := st.Sessions().GetByID(ctx, parent.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.AttentionState).To(Equal(string(store.AttentionWaitingInput)))
		Expect(got.AttentionTitle.String).To(Equal("Permission required"))

		Expect(st.Sessions().SetAttention(ctx, parent.ID, store.AttentionNone, "", "", "")).To(Succeed())
		got, _ = st.Sessions().GetByID(ctx, parent.ID)
		Expect(got.AttentionState).To(Equal(string(store.AttentionNone)))
	})
})
