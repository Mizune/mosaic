package conductor_test

import (
	"context"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/conductor"
	"github.com/Mizune/mosaic/internal/store"
)

var _ = Describe("Runner.Run", func() {
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

	It("dispatches tasks in topological order", func() {
		plan := conductor.Plan{
			Conductor: conductor.ConductorSection{Name: "demo"},
			Tasks: []conductor.TaskSpec{
				{Name: "a"},
				{Name: "b", DependsOn: []string{"a"}},
				{Name: "c", DependsOn: []string{"b"}},
			},
		}
		runner := conductor.NewRunner(st, nil)
		Expect(runner.Prepare(ctx, plan, "/tmp/proj", "")).To(Succeed())

		var (
			mu       sync.Mutex
			executed []string
		)
		dispatch := func(_ context.Context, in conductor.DispatchInput) error {
			mu.Lock()
			executed = append(executed, in.Spec.Name)
			mu.Unlock()
			return nil
		}
		Expect(runner.Run(ctx, dispatch)).To(Succeed())

		Expect(executed).To(Equal([]string{"a", "b", "c"}))

		c := runner.Conductor()
		got, err := st.Conductors().GetByID(ctx, c.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(string(store.ConductorDone)))
	})

	It("runs independent branches concurrently", func() {
		plan := conductor.Plan{
			Conductor: conductor.ConductorSection{Name: "fan"},
			Tasks: []conductor.TaskSpec{
				{Name: "root"},
				{Name: "left", DependsOn: []string{"root"}},
				{Name: "right", DependsOn: []string{"root"}},
				{Name: "join", DependsOn: []string{"left", "right"}},
			},
		}
		runner := conductor.NewRunner(st, nil)
		Expect(runner.Prepare(ctx, plan, "/tmp/proj", "")).To(Succeed())

		dispatch := func(_ context.Context, in conductor.DispatchInput) error { return nil }
		Expect(runner.Run(ctx, dispatch)).To(Succeed())

		// Every task ended up in the done state.
		tasks, err := st.Conductors().ListTasks(ctx, runner.Conductor().ID)
		Expect(err).NotTo(HaveOccurred())
		for _, t := range tasks {
			Expect(t.Status).To(Equal(string(store.TaskDone)))
		}
	})

	It("marks the conductor failed when a task fails", func() {
		plan := conductor.Plan{
			Conductor: conductor.ConductorSection{Name: "boom"},
			Tasks: []conductor.TaskSpec{
				{Name: "a"},
				{Name: "b", DependsOn: []string{"a"}},
			},
		}
		runner := conductor.NewRunner(st, nil)
		Expect(runner.Prepare(ctx, plan, "/tmp/proj", "")).To(Succeed())

		dispatch := func(_ context.Context, in conductor.DispatchInput) error {
			if in.Spec.Name == "a" {
				return errAlwaysFails
			}
			return nil
		}
		Expect(runner.Run(ctx, dispatch)).To(Succeed())

		got, err := st.Conductors().GetByID(ctx, runner.Conductor().ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(string(store.ConductorFailed)))
	})
})

// errAlwaysFails is a sentinel returned by the failure scenario's
// dispatch stub.
var errAlwaysFails = errString("dispatch failed")

type errString string

func (e errString) Error() string { return string(e) }
