package claudeio_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/claudeio"
	"github.com/Mizune/mosaic/pkg/transcript"
)

// helper builders so the specs read like behaviour, not JSON.
func toolUse(id, name string, at time.Time) transcript.Entry {
	return transcript.Entry{
		Type:      transcript.EntryAssistant,
		Timestamp: at,
		Message: &transcript.Message{
			Role: "assistant",
			Content: transcript.ContentList{
				{Type: "tool_use", ID: id, Name: name},
			},
		},
	}
}

func toolResult(id string, at time.Time) transcript.Entry {
	return transcript.Entry{
		Type:      transcript.EntryUser,
		Timestamp: at,
		Message: &transcript.Message{
			Role: "user",
			Content: transcript.ContentList{
				{Type: "tool_result", ToolUseID: id, Content: []byte(`"ok"`)},
			},
		},
	}
}

var _ = Describe("StateMachine", func() {
	var (
		sm *claudeio.StateMachine
		t0 time.Time
	)

	BeforeEach(func() {
		sm = claudeio.NewStateMachine()
		t0 = time.Now()
	})

	Context("when the agent is idle", func() {
		It("reports an empty current tool", func() {
			Expect(sm.Snapshot().CurrentTool).To(BeEmpty())
			Expect(sm.Snapshot().PendingCount).To(Equal(0))
		})
	})

	Context("when a single tool_use is observed", func() {
		BeforeEach(func() {
			sm.Observe(toolUse("id1", "Read", t0))
		})

		It("marks Read as the current tool", func() {
			snap := sm.Snapshot()
			Expect(snap.CurrentTool).To(Equal("Read"))
			Expect(snap.PendingCount).To(Equal(1))
		})

		It("clears AwaitingInput because the agent is busy again", func() {
			Expect(sm.Snapshot().AwaitingInput).To(BeFalse())
		})
	})

	Context("when the matching tool_result arrives", func() {
		BeforeEach(func() {
			sm.Observe(toolUse("id1", "Read", t0))
			sm.Observe(toolResult("id1", t0.Add(50*time.Millisecond)))
		})

		It("clears the current tool", func() {
			snap := sm.Snapshot()
			Expect(snap.CurrentTool).To(BeEmpty())
			Expect(snap.PendingCount).To(Equal(0))
		})
	})

	Context("with overlapping tool calls", func() {
		BeforeEach(func() {
			sm.Observe(toolUse("id1", "Read", t0))
			sm.Observe(toolUse("id2", "Grep", t0.Add(10*time.Millisecond)))
		})

		It("reports the latest tool while previous is pending", func() {
			Expect(sm.Snapshot().CurrentTool).To(Equal("Grep"))
			Expect(sm.Snapshot().PendingCount).To(Equal(2))
		})

		It("falls back to the older tool when the newer result arrives", func() {
			sm.Observe(toolResult("id2", t0.Add(20*time.Millisecond)))
			Expect(sm.Snapshot().CurrentTool).To(Equal("Read"))
			Expect(sm.Snapshot().PendingCount).To(Equal(1))
		})
	})

	Context("Notifications", func() {
		It("flips AwaitingInput to true", func() {
			sm.ObserveNotification("Permission required", "Bash command needs approval")
			snap := sm.Snapshot()
			Expect(snap.AwaitingInput).To(BeTrue())
			Expect(snap.LastNotificationTitle).To(Equal("Permission required"))
			Expect(snap.LastNotificationBody).To(ContainSubstring("Bash"))
		})

		It("clears AwaitingInput when the next tool_use is observed", func() {
			sm.ObserveNotification("Permission required", "...")
			Expect(sm.Snapshot().AwaitingInput).To(BeTrue())

			sm.Observe(toolUse("id1", "Bash", t0))
			Expect(sm.Snapshot().AwaitingInput).To(BeFalse())
		})

		It("can be dismissed without clearing the recorded title/body", func() {
			sm.ObserveNotification("Permission required", "Bash")
			sm.DismissNotification()
			snap := sm.Snapshot()
			Expect(snap.AwaitingInput).To(BeFalse())
			Expect(snap.LastNotificationTitle).To(Equal("Permission required"))
		})
	})
})
