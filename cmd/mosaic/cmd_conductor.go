package main

import (
	"context"
	"fmt"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/conductor"
	"github.com/Mizune/mosaic/internal/store"
)

// newConductorCmd builds the `mosaic conductor` subtree.
//
//	mosaic conductor run plan.toml [--dry-run]
//	mosaic conductor ls
//	mosaic conductor show <name>
//	mosaic conductor pause <name>
//	mosaic conductor resume <name>
func newConductorCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "conductor",
		Short: "Run a multi-agent plan with a Master/Worker DAG",
	}
	root.AddCommand(newConductorRunCmd())
	root.AddCommand(newConductorLsCmd())
	root.AddCommand(newConductorShowCmd())
	root.AddCommand(newConductorStatusCmd("pause", store.ConductorPaused,
		"Mark a conductor as paused"))
	root.AddCommand(newConductorStatusCmd("resume", store.ConductorRunning,
		"Resume a paused conductor"))
	return root
}

// findConductorByName looks up a conductor whose name matches name.
// Conductor names are not unique across time (re-running a plan
// creates a new row), so we prefer the most recent match.
func findConductorByName(rt *runtime, ctx context.Context, name string) (store.Conductor, error) {
	rows, err := rt.store.Conductors().List(ctx)
	if err != nil {
		return store.Conductor{}, err
	}
	for _, c := range rows {
		if c.Name == name {
			return c, nil
		}
	}
	return store.Conductor{}, fmt.Errorf("conductor %q not found", name)
}

// newConductorLsCmd: `mosaic conductor ls` prints every persisted
// conductor with its status and task counts, newest first.
func newConductorLsCmd() *cobra.Command {
	var opts runtimeOptions
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List every persisted conductor",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			rows, err := rt.store.Conductors().List(cmd.Context())
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no conductors yet)")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tTASKS\tCREATED")
			for _, c := range rows {
				tasks, _ := rt.store.Conductors().ListTasks(cmd.Context(), c.ID)
				counts := map[string]int{}
				for _, t := range tasks {
					counts[t.Status]++
				}
				summary := fmt.Sprintf("%d/%d done",
					counts[string(store.TaskDone)], len(tasks))
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					c.Name, c.Status, summary,
					c.CreatedAt.Local().Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
	addRuntimeFlags(cmd, &opts)
	return cmd
}

// newConductorShowCmd: `mosaic conductor show <name>` prints every
// task row for the most recent conductor run matching the given
// name, including status, dependencies, and assigned session.
func newConductorShowCmd() *cobra.Command {
	var opts runtimeOptions
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Print the task list and status of a conductor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			c, err := findConductorByName(rt, cmd.Context(), args[0])
			if err != nil {
				return err
			}
			tasks, err := rt.store.Conductors().ListTasks(cmd.Context(), c.ID)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "conductor: %s (%s)\n", c.Name, c.Status)
			fmt.Fprintf(out, "project:   %s\n", c.ProjectPath)
			fmt.Fprintf(out, "created:   %s\n\n",
				c.CreatedAt.Local().Format("2006-01-02 15:04:05"))

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TASK\tSTATUS\tDEPENDS\tSESSION")
			for _, t := range tasks {
				deps := "—"
				if raw := t.Dependencies(); len(raw) > 0 {
					deps = fmt.Sprintf("%v", raw)
				}
				session := "—"
				if t.AssignedSessionID.Valid {
					session = t.AssignedSessionID.String
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					t.Name, t.Status, deps, session)
			}
			return w.Flush()
		},
	}
	addRuntimeFlags(cmd, &opts)
	return cmd
}

// newConductorStatusCmd is the shared factory behind `pause` and
// `resume`. Both flip the conductor's status column to a new value
// without touching its tasks — pausing a running conductor just
// stops the CLI runner from dispatching fresh work; in-flight
// dispatches complete naturally.
func newConductorStatusCmd(name string, status store.ConductorStatus, short string) *cobra.Command {
	var opts runtimeOptions
	cmd := &cobra.Command{
		Use:   name + " <name>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			c, err := findConductorByName(rt, cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := rt.store.Conductors().SetStatus(cmd.Context(), c.ID, status); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"conductor %s → %s\n", c.Name, status)
			return nil
		},
	}
	addRuntimeFlags(cmd, &opts)
	return cmd
}

func newConductorRunCmd() *cobra.Command {
	var (
		opts   runtimeOptions
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "run <plan.toml>",
		Short: "Execute a conductor plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			planPath, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			plan, err := conductor.LoadPlan(planPath)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "conductor: %s\n", plan.Conductor.Name)
			fmt.Fprintf(out, "tasks:     %d\n", len(plan.Tasks))
			for _, t := range plan.Tasks {
				deps := "—"
				if len(t.DependsOn) > 0 {
					deps = fmt.Sprintf("%v", t.DependsOn)
				}
				fmt.Fprintf(out, "  · %-20s deps=%s\n", t.Name, deps)
			}

			if dryRun {
				fmt.Fprintln(out, "\n(dry run; no sessions created)")
				return nil
			}

			project := plan.Conductor.Project
			if project == "" {
				project = "."
			}
			projectAbs, err := filepath.Abs(project)
			if err != nil {
				return err
			}

			runner := conductor.NewRunner(rt.store, rt.sessions)
			if err := runner.Prepare(cmd.Context(), plan, projectAbs, planPath); err != nil {
				return err
			}
			fmt.Fprintf(out, "\nrunning conductor %s ...\n", runner.Conductor().ID)

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			dispatch := conductor.SessionDispatch(rt.sessions)
			if err := runner.Run(ctx, dispatch); err != nil {
				return err
			}

			snap := runner.Snapshot()
			fmt.Fprintf(out, "done: %d done, %d failed, %d pending\n",
				snap[conductor.TaskDone], snap[conductor.TaskFailed], snap[conductor.TaskPending])
			return nil
		},
	}
	addRuntimeFlags(cmd, &opts)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the plan and exit without dispatching")
	return cmd
}
