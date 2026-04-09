// Package conductor implements mosaic's multi-agent orchestration:
// load a TOML plan, schedule its tasks via a DAG, dispatch each
// ready task to its own mosaic session, and let the running tasks
// exchange messages through SQLite.
//
// The plan format is intentionally minimal so the user can hand-edit
// it. See features/conductor.feature for executable examples; the
// short version:
//
//	[conductor]
//	name        = "refactor-auth"
//	project     = "."
//	worker_setup = "default"
//
//	[[task]]
//	name   = "scaffold"
//	prompt = "Move auth code into pkg/auth/."
//
//	[[task]]
//	name       = "tests"
//	depends_on = ["scaffold"]
//	prompt     = "Add unit tests for pkg/auth/."
package conductor

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// Plan is the parsed representation of a conductor TOML file.
type Plan struct {
	Conductor ConductorSection `toml:"conductor"`
	Tasks     []TaskSpec       `toml:"task"`
}

// ConductorSection holds the [conductor] table.
type ConductorSection struct {
	Name        string `toml:"name"`
	Project     string `toml:"project"`
	MasterSetup string `toml:"master_setup"`
	WorkerSetup string `toml:"worker_setup"`
	Description string `toml:"description"`
}

// TaskSpec is one entry in [[task]].
type TaskSpec struct {
	Name      string   `toml:"name"`
	DependsOn []string `toml:"depends_on"`
	Prompt    string   `toml:"prompt"`
	Tool      string   `toml:"tool"`
	Branch    string   `toml:"branch"`
	Setup     string   `toml:"setup"`
}

// LoadPlan reads a TOML file and validates the resulting Plan.
func LoadPlan(path string) (Plan, error) {
	var p Plan
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return Plan{}, fmt.Errorf("load plan: %w", err)
	}
	if err := p.Validate(); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// Validate checks that the plan is internally consistent: every task
// has a unique name, every dependency points at a real task, and the
// resulting graph is acyclic.
func (p Plan) Validate() error {
	if p.Conductor.Name == "" {
		return errors.New("plan: [conductor] name is required")
	}
	if len(p.Tasks) == 0 {
		return errors.New("plan: at least one [[task]] is required")
	}
	names := map[string]struct{}{}
	for _, t := range p.Tasks {
		if t.Name == "" {
			return errors.New("plan: every task needs a name")
		}
		if _, dup := names[t.Name]; dup {
			return fmt.Errorf("plan: duplicate task name %q", t.Name)
		}
		names[t.Name] = struct{}{}
	}
	for _, t := range p.Tasks {
		for _, d := range t.DependsOn {
			if _, ok := names[d]; !ok {
				return fmt.Errorf("plan: task %q depends on unknown task %q", t.Name, d)
			}
		}
	}
	if _, err := NewScheduler(p.Tasks); err != nil {
		return err
	}
	return nil
}
