package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/internal/tmux"
)

var _ = Describe("SessionService", func() {
	var (
		ctx context.Context
		st  *store.Store
		fk  *tmux.Fake
		b   *bus.Bus
		svc *service.SessionService
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
	})

	Describe("Create", func() {
		It("inserts a row and spawns the matching tmux session", func() {
			received := make(chan bus.Event, 1)
			b.Subscribe(func(e bus.Event) { received <- e })

			row, err := svc.Create(ctx, service.CreateInput{
				Name:         "feat-auth",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(row.ID).NotTo(BeEmpty())
			// New sessions start at idle — the reconciler
			// upgrades them to running on real agent activity.
			Expect(row.Status).To(Equal(string(store.StatusIdle)))

			sessions := fk.Sessions()
			Expect(sessions).To(HaveKey(row.TmuxSession))

			Eventually(received, "1s").Should(Receive(BeAssignableToTypeOf(bus.SessionStarted{})))
		})

		It("rolls back the database row when tmux fails", func() {
			fk.FailOnce("new", errors.New("boom"))

			_, err := svc.Create(ctx, service.CreateInput{
				Name:         "doomed",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).To(HaveOccurred())

			_, err = st.Sessions().GetByName(ctx, "doomed")
			Expect(errors.Is(err, store.ErrSessionNotFound)).To(BeTrue(),
				"row should have been rolled back when tmux failed")
		})

		It("rejects an empty name", func() {
			_, err := svc.Create(ctx, service.CreateInput{
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("InitialPrompt", func() {
		It("types the prompt into the session after a delay", func() {
			row, err := svc.Create(ctx, service.CreateInput{
				Name:          "prompt-test",
				WorktreePath:  GinkgoT().TempDir(),
				InitialPrompt: "refactor the auth package",
			})
			Expect(err).NotTo(HaveOccurred())

			// The injection goroutine waits ~1.5s before
			// calling SendKeys. We Eventually-poll the fake
			// session to see the typed keystrokes land.
			Eventually(func() []string {
				return fk.Sessions()[row.TmuxSession].SentKeys
			}, "3s", "50ms").Should(ContainElement("refactor the auth package"))
		})

		It("leaves SentKeys empty when InitialPrompt is unset", func() {
			row, err := svc.Create(ctx, service.CreateInput{
				Name:         "plain",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())
			// Give the injection goroutine ample time to NOT
			// fire (it should not exist when InitialPrompt is
			// empty).
			time.Sleep(100 * time.Millisecond)
			Expect(fk.Sessions()[row.TmuxSession].SentKeys).To(BeEmpty())
		})
	})

	Describe("hook environment", func() {
		It("exports MOSAIC_* variables when HookSocketPath is set", func() {
			svc.Cfg.HookSocketPath = "/tmp/mosaic-test/pid-1234"

			row, err := svc.Create(ctx, service.CreateInput{
				Name:         "with-hooks",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())

			spawned := fk.Sessions()[row.TmuxSession]
			Expect(spawned.Env).To(HaveKeyWithValue("MOSAIC_SOCKET_PATH", "/tmp/mosaic-test/pid-1234"))
			Expect(spawned.Env).To(HaveKeyWithValue("MOSAIC_WORKTREE_ID", row.ID))
			Expect(spawned.Env).To(HaveKeyWithValue("MOSAIC_TAB_ID", row.ID))
			Expect(spawned.Env).To(HaveKeyWithValue("MOSAIC_SURFACE_ID", "tmux"))
		})

		It("leaves MOSAIC_* unset when no hook server is running", func() {
			// Default: svc.Cfg.HookSocketPath is empty.
			row, err := svc.Create(ctx, service.CreateInput{
				Name:         "no-hooks",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())

			spawned := fk.Sessions()[row.TmuxSession]
			Expect(spawned.Env).NotTo(HaveKey("MOSAIC_SOCKET_PATH"))
		})

		It("re-exports MOSAIC_* when respawning a dead tmux session on attach", func() {
			svc.Cfg.HookSocketPath = "/tmp/mosaic-test/pid-1234"

			row, err := svc.Create(ctx, service.CreateInput{
				Name:         "respawn-hooks",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())
			// Wipe the recorded session so the respawn path takes
			// effect, as if Claude had /exited and the tmux pane
			// tore down.
			Expect(fk.Kill(ctx, row.TmuxSession)).To(Succeed())

			_, err = svc.AttachCommand(ctx, "respawn-hooks")
			Expect(err).NotTo(HaveOccurred())

			respawned := fk.Sessions()[row.TmuxSession]
			Expect(respawned.Env).To(HaveKeyWithValue("MOSAIC_SOCKET_PATH", "/tmp/mosaic-test/pid-1234"))
			Expect(respawned.Env).To(HaveKeyWithValue("MOSAIC_WORKTREE_ID", row.ID))
		})
	})

	Describe("Kill", func() {
		It("kills tmux, removes the row, and publishes an event", func() {
			received := make(chan bus.Event, 4)
			b.Subscribe(func(e bus.Event) { received <- e })

			row, err := svc.Create(ctx, service.CreateInput{
				Name:         "to-kill",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())
			// drain the start event
			Eventually(received, "1s").Should(Receive())

			Expect(svc.Kill(ctx, "to-kill")).To(Succeed())

			_, err = st.Sessions().GetByID(ctx, row.ID)
			Expect(errors.Is(err, store.ErrSessionNotFound)).To(BeTrue())

			Expect(fk.Sessions()).NotTo(HaveKey(row.TmuxSession))

			Eventually(received, "1s").Should(Receive(BeAssignableToTypeOf(bus.SessionStopped{})))
		})
	})

	Describe("List", func() {
		It("flips status to stopped when the tmux session disappears", func() {
			row, err := svc.Create(ctx, service.CreateInput{
				Name:         "vanishing",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())

			// Simulate tmux dying outside mosaic.
			Expect(fk.Kill(ctx, row.TmuxSession)).To(Succeed())

			res, err := svc.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Sessions).To(HaveLen(1))
			Expect(res.Sessions[0].Status).To(Equal(string(store.StatusStopped)))
			Expect(res.TmuxMissing).To(ConsistOf("vanishing"))
		})

		It("does not flag sessions whose tmux backend is alive", func() {
			_, err := svc.Create(ctx, service.CreateInput{
				Name:         "healthy",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())

			res, err := svc.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.TmuxMissing).To(BeEmpty())
			// Alive tmux backend: the status stays at whatever
			// the reconciler last set — "idle" right after Create
			// because no tool_use has been observed yet.
			Expect(res.Sessions[0].Status).To(Equal(string(store.StatusIdle)))
		})
	})

	Describe("AttachCommand", func() {
		It("returns an exec.Cmd for an existing session", func() {
			row, err := svc.Create(ctx, service.CreateInput{
				Name:         "attached",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())

			cmd, err := svc.AttachCommand(ctx, "attached")
			Expect(err).NotTo(HaveOccurred())
			Expect(cmd).NotTo(BeNil())
			_ = row
		})

		It("errors when the session is unknown", func() {
			_, err := svc.AttachCommand(ctx, "ghost")
			Expect(errors.Is(err, store.ErrSessionNotFound)).To(BeTrue())
		})

		It("respawns the tmux session when it has disappeared", func() {
			row, err := svc.Create(ctx, service.CreateInput{
				Name:         "halfdead",
				WorktreePath: GinkgoT().TempDir(),
			})
			Expect(err).NotTo(HaveOccurred())
			// Simulate Claude /exit tearing down the tmux pane.
			Expect(fk.Kill(ctx, row.TmuxSession)).To(Succeed())

			cmd, err := svc.AttachCommand(ctx, "halfdead")
			Expect(err).NotTo(HaveOccurred())
			Expect(cmd).NotTo(BeNil())

			// The backend must have a new session by the same name
			// again, so a subsequent attach-style command still
			// hits something real.
			Expect(fk.Sessions()).To(HaveKey(row.TmuxSession))
		})
	})
})
