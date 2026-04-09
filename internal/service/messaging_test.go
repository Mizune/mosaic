package service_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tmux"
)

var _ = Describe("MessagingService", func() {
	var (
		ctx context.Context
		st  *store.Store
		fk  *tmux.Fake
		b   *bus.Bus
		svc *service.SessionService
		msg *service.MessagingService
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir := GinkgoT().TempDir()
		var err error
		st, err = store.Open(filepath.Join(dir, "state.db"))
		Expect(err).NotTo(HaveOccurred())
		Expect(st.Migrate(ctx)).To(Succeed())
		DeferCleanup(st.Close)

		fk = tmux.NewFake()
		b = bus.New()
		DeferCleanup(b.Close)

		svc = service.NewSessionService(st, fk, b, nil)
		msg = service.NewMessagingService(st, fk, b, 0)
	})

	It("posts a message that resolves recipient by name", func() {
		recv, err := svc.Create(ctx, service.CreateInput{
			Name:         "recv",
			WorktreePath: GinkgoT().TempDir(),
		})
		Expect(err).NotTo(HaveOccurred())

		posted, err := msg.Post(ctx, service.PostInput{
			ToSession: "recv",
			Body:      "hello",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(posted.ID).NotTo(BeZero())
		Expect(posted.ToSessionID.Valid).To(BeTrue())
		Expect(posted.ToSessionID.String).To(Equal(recv.ID))
	})

	It("rejects a missing recipient", func() {
		_, err := msg.Post(ctx, service.PostInput{
			ToSession: "nope",
			Body:      "hello",
		})
		Expect(err).To(HaveOccurred())
	})

	It("delivers queued messages via tmux send-keys", func() {
		recv, err := svc.Create(ctx, service.CreateInput{
			Name:         "recv",
			WorktreePath: GinkgoT().TempDir(),
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = msg.Post(ctx, service.PostInput{
			ToSession: "recv",
			Kind:      "task",
			Body:      "please review PR 1234",
		})
		Expect(err).NotTo(HaveOccurred())

		// Step the delivery loop manually — no ticker racing.
		Expect(service.DeliverOnceForTest(ctx, msg)).To(Succeed())

		sent := fk.Sessions()[recv.TmuxSession].SentKeys
		Expect(sent).To(HaveLen(1))
		Expect(sent[0]).To(ContainSubstring("please review PR 1234"))
		Expect(sent[0]).To(ContainSubstring("[mosaic:task]"))

		inbox, err := st.Conductors().Inbox(ctx, recv.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(inbox).To(BeEmpty(), "delivered messages should leave the inbox empty")
	})

	It("tolerates tmux send-keys failures", func() {
		recv, err := svc.Create(ctx, service.CreateInput{
			Name:         "recv",
			WorktreePath: GinkgoT().TempDir(),
		})
		Expect(err).NotTo(HaveOccurred())
		_, _ = msg.Post(ctx, service.PostInput{ToSession: "recv", Body: "one"})

		// Simulate a failed delivery attempt.
		fk.FailOnce("send", context.Canceled)
		// Should not return an error to the caller of deliverOnce.
		Expect(service.DeliverOnceForTest(ctx, msg)).To(Succeed())
		_ = recv
	})
})
