package conductor

import (
	"errors"
	"fmt"
)

// TaskState reflects where a task is in the DAG lifecycle.
type TaskState string

const (
	TaskPending TaskState = "pending" // dependencies not yet satisfied
	TaskReady   TaskState = "ready"   // can be dispatched right now
	TaskRunning TaskState = "running"
	TaskDone    TaskState = "done"
	TaskFailed  TaskState = "failed"
)

// Scheduler is a Kahn-style topological executor that knows nothing
// about how tasks are actually run — that's the runner's job. It just
// answers "what is ready right now?" and accepts state transitions.
type Scheduler struct {
	tasks      map[string]*scheduledTask
	order      []string         // insertion order so iteration is stable
	indegree   map[string]int   // remaining unsatisfied dependencies
	dependents map[string][]string
}

type scheduledTask struct {
	spec  TaskSpec
	state TaskState
}

// NewScheduler builds a Scheduler from the parsed task specs and
// validates that the dependency graph is acyclic.
func NewScheduler(specs []TaskSpec) (*Scheduler, error) {
	s := &Scheduler{
		tasks:      make(map[string]*scheduledTask, len(specs)),
		indegree:   make(map[string]int, len(specs)),
		dependents: make(map[string][]string, len(specs)),
	}
	for _, t := range specs {
		s.order = append(s.order, t.Name)
		s.tasks[t.Name] = &scheduledTask{spec: t, state: TaskPending}
		s.indegree[t.Name] = len(t.DependsOn)
	}
	for _, t := range specs {
		for _, dep := range t.DependsOn {
			if _, ok := s.tasks[dep]; !ok {
				return nil, fmt.Errorf("scheduler: %q depends on unknown task %q", t.Name, dep)
			}
			s.dependents[dep] = append(s.dependents[dep], t.Name)
		}
	}
	if err := s.detectCycle(); err != nil {
		return nil, err
	}
	// Initial sweep: any task with indegree=0 is immediately ready.
	for _, name := range s.order {
		if s.indegree[name] == 0 {
			s.tasks[name].state = TaskReady
		}
	}
	return s, nil
}

// detectCycle uses Kahn's algorithm in a copy of the indegree map.
// If we cannot empty the queue, the remaining nodes form a cycle.
func (s *Scheduler) detectCycle() error {
	indeg := make(map[string]int, len(s.indegree))
	for k, v := range s.indegree {
		indeg[k] = v
	}
	var queue []string
	for _, name := range s.order {
		if indeg[name] == 0 {
			queue = append(queue, name)
		}
	}
	processed := 0
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		processed++
		for _, child := range s.dependents[name] {
			indeg[child]--
			if indeg[child] == 0 {
				queue = append(queue, child)
			}
		}
	}
	if processed != len(s.tasks) {
		return errors.New("scheduler: dependency cycle detected")
	}
	return nil
}

// Ready returns the names of every task currently in TaskReady, in
// stable insertion order.
func (s *Scheduler) Ready() []string {
	var out []string
	for _, name := range s.order {
		if s.tasks[name].state == TaskReady {
			out = append(out, name)
		}
	}
	return out
}

// State returns the current state of one task.
func (s *Scheduler) State(name string) TaskState {
	t, ok := s.tasks[name]
	if !ok {
		return ""
	}
	return t.state
}

// Spec returns the underlying TaskSpec for a name.
func (s *Scheduler) Spec(name string) (TaskSpec, bool) {
	t, ok := s.tasks[name]
	if !ok {
		return TaskSpec{}, false
	}
	return t.spec, true
}

// Order returns task names in insertion order.
func (s *Scheduler) Order() []string {
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// MarkRunning transitions a task from Ready → Running.
func (s *Scheduler) MarkRunning(name string) error {
	t, ok := s.tasks[name]
	if !ok {
		return fmt.Errorf("scheduler: unknown task %q", name)
	}
	if t.state != TaskReady {
		return fmt.Errorf("scheduler: cannot mark %q running from %s", name, t.state)
	}
	t.state = TaskRunning
	return nil
}

// MarkDone records a task's success and transitions any newly
// unblocked dependents into Ready. The names of newly ready tasks
// are returned so the runner can dispatch them immediately.
func (s *Scheduler) MarkDone(name string) ([]string, error) {
	t, ok := s.tasks[name]
	if !ok {
		return nil, fmt.Errorf("scheduler: unknown task %q", name)
	}
	if t.state != TaskRunning && t.state != TaskReady {
		return nil, fmt.Errorf("scheduler: cannot mark %q done from %s", name, t.state)
	}
	t.state = TaskDone

	var newlyReady []string
	for _, child := range s.dependents[name] {
		s.indegree[child]--
		if s.indegree[child] == 0 && s.tasks[child].state == TaskPending {
			s.tasks[child].state = TaskReady
			newlyReady = append(newlyReady, child)
		}
	}
	return newlyReady, nil
}

// MarkFailed records a task's failure and cascades the failure to
// every transitive dependent so the conductor cannot deadlock waiting
// for tasks that can never run. The names of newly failed tasks are
// returned so the caller can persist the cascade in one pass.
func (s *Scheduler) MarkFailed(name string) ([]string, error) {
	t, ok := s.tasks[name]
	if !ok {
		return nil, fmt.Errorf("scheduler: unknown task %q", name)
	}
	t.state = TaskFailed

	var cascaded []string
	queue := []string{name}
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		for _, child := range s.dependents[head] {
			c := s.tasks[child]
			if c.state == TaskDone || c.state == TaskFailed {
				continue
			}
			c.state = TaskFailed
			cascaded = append(cascaded, child)
			queue = append(queue, child)
		}
	}
	return cascaded, nil
}

// Done reports whether every task is in TaskDone or TaskFailed.
func (s *Scheduler) Done() bool {
	for _, t := range s.tasks {
		if t.state != TaskDone && t.state != TaskFailed {
			return false
		}
	}
	return true
}

// Snapshot returns a (state, count) summary of the whole graph,
// useful for the conductor panel header.
func (s *Scheduler) Snapshot() map[TaskState]int {
	out := map[TaskState]int{}
	for _, t := range s.tasks {
		out[t.state]++
	}
	return out
}
