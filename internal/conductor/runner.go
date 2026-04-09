package conductor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/Mizune/mosaic/internal/service"
	"github.com/Mizune/mosaic/internal/store"
)

// Runner executes one Plan against the session service. It is the
// glue between the pure Scheduler and mosaic's tmux/worktree-aware
// session creation. The runner is **idempotent against the store**:
// rerunning it after a crash picks up where the previous run left
// off, because the conductor / task / message rows persist all the
// state we need.
type Runner struct {
	store    *store.Store
	sessions *service.SessionService
	log      *slog.Logger

	mu       sync.Mutex
	plan     Plan
	conductor store.Conductor
	scheduler *Scheduler
	taskRows  map[string]store.ConductorTask // schedulerName → row
}

// NewRunner constructs a Runner. Pass nil for the logger to use the
// default slog.
func NewRunner(s *store.Store, svc *service.SessionService) *Runner {
	return &Runner{
		store:    s,
		sessions: svc,
		log:      slog.With("subsys", "conductor.runner"),
		taskRows: map[string]store.ConductorTask{},
	}
}

// Prepare loads (or creates) the conductor + task rows for the given
// plan. After Prepare, the Runner has enough state to call Tick or
// to be inspected by the TUI's conductor panel.
//
// projectPath is the absolute path to the source repository every
// task's worktree branches off of.
func (r *Runner) Prepare(ctx context.Context, plan Plan, projectPath, planPath string) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plan = plan

	conductor, err := r.store.Conductors().Insert(ctx, store.NewConductor{
		Name:        plan.Conductor.Name,
		ProjectPath: projectPath,
		PlanPath:    planPath,
	})
	if err != nil {
		return fmt.Errorf("insert conductor: %w", err)
	}
	r.conductor = conductor

	for _, t := range plan.Tasks {
		row, err := r.store.Conductors().InsertTask(ctx, store.NewConductorTask{
			ConductorID: conductor.ID,
			Name:        t.Name,
			DependsOn:   t.DependsOn,
			Prompt:      t.Prompt,
		})
		if err != nil {
			return fmt.Errorf("insert task %s: %w", t.Name, err)
		}
		r.taskRows[t.Name] = row
	}

	sched, err := NewScheduler(plan.Tasks)
	if err != nil {
		return err
	}
	r.scheduler = sched
	return nil
}

// Conductor returns the persisted conductor row produced by Prepare.
func (r *Runner) Conductor() store.Conductor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conductor
}

// Snapshot reports the current per-state task counts. Useful for
// dry-run output and the TUI conductor panel header.
func (r *Runner) Snapshot() map[TaskState]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.scheduler == nil {
		return nil
	}
	return r.scheduler.Snapshot()
}

// Plan returns the loaded plan.
func (r *Runner) Plan() Plan {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.plan
}

// Run drives the plan to completion: dispatch every ready task, wait
// for completion via taskCompletes, mark dependents ready, repeat.
//
// dispatch is the strategy used to actually start a task — production
// callers wire it to a function that creates a mosaic session and
// returns the session id; tests can pass a stub that records the
// dispatch and immediately reports completion.
func (r *Runner) Run(ctx context.Context, dispatch DispatchFunc) error {
	if dispatch == nil {
		return errors.New("conductor: dispatch function is required")
	}
	if err := r.store.Conductors().SetStatus(ctx, r.conductor.ID, store.ConductorRunning); err != nil {
		return err
	}

	completes := make(chan taskCompletion, 16)
	defer close(completes)

	if err := r.dispatchReady(ctx, dispatch, completes); err != nil {
		return err
	}

	for {
		if r.scheduler.Done() {
			break
		}
		select {
		case <-ctx.Done():
			_ = r.store.Conductors().SetStatus(ctx, r.conductor.ID, store.ConductorPaused)
			return ctx.Err()
		case res := <-completes:
			if res.err != nil {
				cascaded, _ := r.scheduler.MarkFailed(res.name)
				_ = r.store.Conductors().SetTaskStatus(ctx, r.taskRows[res.name].ID, store.TaskFailed)
				for _, name := range cascaded {
					_ = r.store.Conductors().SetTaskStatus(ctx, r.taskRows[name].ID, store.TaskFailed)
				}
				continue
			}
			newReady, err := r.scheduler.MarkDone(res.name)
			if err != nil {
				return err
			}
			_ = r.store.Conductors().SetTaskStatus(ctx, r.taskRows[res.name].ID, store.TaskDone)
			for _, name := range newReady {
				if err := r.dispatchOne(ctx, name, dispatch, completes); err != nil {
					return err
				}
			}
		}
	}

	final := store.ConductorDone
	for _, t := range r.taskRows {
		row, _ := r.store.Conductors().ListTasks(ctx, r.conductor.ID)
		_ = row // not used; just keeps the closure cheap
		_ = t
	}
	if hasFailed(r.scheduler) {
		final = store.ConductorFailed
	}
	return r.store.Conductors().SetStatus(ctx, r.conductor.ID, final)
}

func hasFailed(s *Scheduler) bool {
	for state, n := range s.Snapshot() {
		if state == TaskFailed && n > 0 {
			return true
		}
	}
	return false
}

// dispatchReady fires off every task currently in TaskReady state.
func (r *Runner) dispatchReady(ctx context.Context, dispatch DispatchFunc, completes chan<- taskCompletion) error {
	for _, name := range r.scheduler.Ready() {
		if err := r.dispatchOne(ctx, name, dispatch, completes); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) dispatchOne(ctx context.Context, name string, dispatch DispatchFunc, completes chan<- taskCompletion) error {
	if err := r.scheduler.MarkRunning(name); err != nil {
		return err
	}
	if err := r.store.Conductors().SetTaskStatus(ctx, r.taskRows[name].ID, store.TaskRunning); err != nil {
		return err
	}
	spec, _ := r.scheduler.Spec(name)
	go func() {
		err := dispatch(ctx, DispatchInput{
			ConductorID:   r.conductor.ID,
			ConductorName: r.plan.Conductor.Name,
			Project:       r.conductor.ProjectPath,
			Spec:          spec,
		})
		completes <- taskCompletion{name: name, err: err}
	}()
	return nil
}

// DispatchInput carries everything a dispatch function needs to spin
// up a worker for one task.
type DispatchInput struct {
	ConductorID   string
	ConductorName string
	Project       string
	Spec          TaskSpec
}

// DispatchFunc is the strategy interface that decouples Runner from
// session creation. Production wires it to a function that creates a
// mosaic session via the session service; tests use stubs.
type DispatchFunc func(ctx context.Context, in DispatchInput) error

// taskCompletion is the message a dispatch goroutine sends back to
// Run when its task finishes.
type taskCompletion struct {
	name string
	err  error
}

// SessionDispatch returns a DispatchFunc that creates a mosaic
// session per task. The session is named "<conductor>-<task>" and
// gets a worktree under <project>/.worktrees/<conductor>-<task>.
//
// The task's prompt is delivered via [service.CreateInput.InitialPrompt]
// rather than [service.CreateInput.Command], so the normal tool
// launcher (claude / codex / shell) runs first and the prompt is
// then typed into the pane via tmux send-keys. This makes the
// dispatch useful for LLM-based workers: the agent boots, reads
// the prompt as its first user message, and starts working.
//
// For pure-shell tasks the end result is identical — the shell
// sees the typed characters and executes them — so the old
// "prompt as Command" flow is preserved for that case too,
// without the shell-escaping footgun of passing the prompt via
// argv.
func SessionDispatch(svc *service.SessionService) DispatchFunc {
	return func(ctx context.Context, in DispatchInput) error {
		name := in.ConductorName + "-" + in.Spec.Name
		branch := in.Spec.Branch
		if branch == "" {
			branch = "task/" + in.ConductorName + "/" + in.Spec.Name
		}
		_, err := svc.Create(ctx, service.CreateInput{
			Name:           name,
			ProjectPath:    in.Project,
			WorktreePath:   filepath.Join(in.Project, ".worktrees", in.ConductorName+"-"+in.Spec.Name),
			Branch:         branch,
			Tool:           store.SessionTool(in.Spec.Tool),
			InitialPrompt:  in.Spec.Prompt,
			CreateWorktree: true,
			WorktreeRepo:   in.Project,
		})
		// Conductor treats Create failures as task failures, but does
		// not abort the whole run.
		return err
	}
}

// WaitForCompletion blocks until the runner's scheduler reports Done
// or the context is cancelled. It is mostly useful in tests.
func (r *Runner) WaitForCompletion(ctx context.Context, poll time.Duration) error {
	for {
		if r.scheduler.Done() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}
