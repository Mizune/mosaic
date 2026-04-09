# Conductor — multi-agent DAGs

The conductor orchestrates many agent sessions in dependency
order. You describe a workflow as a set of tasks with `depends_on`
edges in a TOML file; `mosaic conductor run plan.toml` loads it,
persists it, and dispatches each task to its own mosaic session
(with its own git worktree and branch) as soon as its dependencies
are satisfied.

It is the same idea as `Makefile` or GitHub Actions workflow, but
the "command" for each node is "ask an LLM agent to do X" rather
than "run this shell script".

## Plan file format

```toml
[conductor]
name        = "refactor-auth"      # required, unique run label
project     = "/abs/path/to/repo"  # the git repo the run operates on
description = "Split auth out of the monolith"

[[task]]
name   = "scaffold"
tool   = "claude"
prompt = """
Move every file under src/auth/* into pkg/auth/, update the imports
in the callers, and adjust go.mod if necessary.
"""

[[task]]
name       = "tests"
tool       = "claude"
depends_on = ["scaffold"]
prompt     = "Add unit tests for pkg/auth covering Login and Logout."

[[task]]
name       = "docs"
tool       = "claude"
depends_on = ["scaffold"]
prompt     = "Add a docs/auth.md describing the new package layout."

[[task]]
name       = "release"
tool       = "claude"
depends_on = ["tests", "docs"]
prompt     = "Run go test ./... and summarise the diff for the PR description."
```

The graph here is:

```
scaffold
   ├── tests   ┐
   └── docs    ┴─→ release
```

`tests` and `docs` run in **parallel** after `scaffold` finishes,
then `release` waits for both.

### `[conductor]` fields

| Field | Type | Required | Default | Meaning |
|---|---|---|---|---|
| `name` | string | ✓ | — | Unique label for this run (also becomes the session name prefix: `<name>-<task>`). |
| `project` | string | ✓ | — | Absolute path to the git repository all tasks operate on. Relative paths are resolved against the plan file's directory. |
| `description` | string |   | `""` | Free-form summary shown in the Conductor Panel header. |
| `master_setup` | string |   | `""` | Worktree setup recipe name used by the master session (reserved for Phase 8). |
| `worker_setup` | string |   | `"default"` | Worktree setup recipe every task inherits unless overridden. |

### `[[task]]` fields

| Field | Type | Required | Default | Meaning |
|---|---|---|---|---|
| `name` | string | ✓ | — | Unique within the plan; becomes the suffix of the worker session name (`<conductor>-<name>`). |
| `prompt` | string | ✓ | — | The first message mosaic types into the worker pane after the agent boots. For `tool = "shell"` this becomes the initial shell command. |
| `tool` | string |   | `"claude"` | `claude` / `codex` / `gemini` / `shell` |
| `depends_on` | []string |   | `[]` | Other task names that must complete before this one dispatches. |
| `branch` | string |   | `task/<conductor>/<name>` | Git branch name the worktree is checked out on. |
| `setup` | string |   | inherited from `worker_setup` | Worktree setup recipe name. |

### Validation

`mosaic conductor run` validates the plan before doing anything:

- every task name must be unique
- every `depends_on` entry must reference a real task
- the resulting graph must be acyclic (topological sort succeeds)
- at least one task must exist

Failures are reported with the offending task name so you can
fix the plan without guessing.

## Running a plan

### From the CLI

```bash
# Validate + print the plan without dispatching:
mosaic conductor run plan.toml --dry-run

# Actually dispatch:
mosaic conductor run plan.toml
```

`run` blocks in the current terminal, prints each task's status
as it progresses, and exits when the DAG is done (or any ready
task has failed and its dependents are cascaded).

### From the TUI

Open the Conductor Panel with `c`, then:

- `+` — opens the plan-file input. Type an absolute path and press
  Enter to dispatch in the background.
- `Y` — replays the selected conductor using its stored `plan_path`.

The panel shows live conductor / task status as the runner
updates the store.

## Observing a running plan

### In the TUI

- **TOP screen**: each task becomes a regular mosaic session
  named `<conductor>-<task>`. They appear grouped under their
  project path like any other session.
- **Conductor Panel (`c`)**: two-pane view. Left = every
  persisted conductor + its task tree. Right = details for the
  highlighted row (header stats for a conductor, prompt / status
  / result for a task).
- **`Enter` / `l`** on a task row jumps you straight to the
  worker session's TOP-screen row.
- **`M`** on a task row opens a message input targeted at the
  worker; the delivery loop types it into the pane on the next
  tick.

### From the CLI

```bash
mosaic conductor ls
# NAME           STATUS    TASKS         CREATED
# refactor-auth  running   2/4 done      2026-04-08 18:42
# demo           done      4/4 done      2026-04-07 12:03

mosaic conductor show refactor-auth
# prints every task row with status, deps, assigned_session

mosaic conductor pause refactor-auth
mosaic conductor resume refactor-auth
```

## Inter-agent messaging

Tasks can talk to each other (or you can talk to any task) via
`mosaic send`:

```bash
mosaic send --to refactor-auth-tests \
            --from refactor-auth-scaffold \
            --kind task \
            "Please also cover the LogoutAll endpoint."
```

The message is stored in SQLite and the messaging delivery loop
injects it into the recipient's tmux pane on its next tick
(usually within a second). The same action is available from the
TUI by pressing `M` in the Conductor Panel.

## Retry & failure handling

- A task that fails (worker session exits non-zero or times out)
  is marked `failed`. Every dependent task cascades to `failed`
  automatically.
- Press `r` in the Conductor Panel to flip a failed task back to
  `pending`. The next `mosaic conductor run <plan>` (or an
  explicit `replay`) will pick it up again.
- Pause does NOT kill in-flight dispatches. It just stops the
  runner from starting new ones.

## How prompts reach Claude

For `tool = "claude"` (and friends), mosaic starts the agent with
its normal launcher (including `--continue` if a prior transcript
exists, plus any flags from `[mosaic.tools.claude]`), waits ~1.5s
for the agent to boot, and then uses `tmux send-keys` to type
the prompt followed by Enter. This works with both fresh sessions
and resumed ones.

For `tool = "shell"` the prompt is typed directly into the shell,
so you can put any command line there (`make build`, `pnpm test`,
etc.).

## Example patterns

### CI-style pipeline

```toml
[conductor]
name    = "release-check"
project = "."

[[task]]
name   = "build"
tool   = "shell"
prompt = "make build"

[[task]]
name       = "test"
tool       = "shell"
depends_on = ["build"]
prompt     = "make test"

[[task]]
name       = "bench"
tool       = "shell"
depends_on = ["build"]
prompt     = "make bench"
```

### Multi-angle refactor

```toml
[conductor]
name    = "refactor-payments"
project = "/Users/me/Projects/web"

[[task]]
name   = "analyse"
tool   = "claude"
prompt = "Read src/payments/, produce a refactor plan in docs/refactor-payments.md"

[[task]]
name       = "split"
tool       = "claude"
depends_on = ["analyse"]
prompt     = "Follow docs/refactor-payments.md — split the module into pkg/payments/."

[[task]]
name       = "migrate-callers"
tool       = "claude"
depends_on = ["split"]
prompt     = "Update every import site to use pkg/payments/."

[[task]]
name       = "tests"
tool       = "claude"
depends_on = ["migrate-callers"]
prompt     = "Add integration tests that exercise the happy path end-to-end."
```

## Limits and gotchas

- **Completion is dispatch-based**, not prompt-based. mosaic
  considers a task "done" when the worker session's Create call
  returns successfully. It does not wait for the agent to finish
  speaking. For that, pair each task with a hook / stop event or
  use `shell` tool where process exit is deterministic.
- **One run = one set of worktrees.** Re-running the same plan
  when the previous run's worktrees still exist will fail (git
  refuses to create duplicate worktrees). Clean with
  `rm -rf <project>/.worktrees/<conductor>-*` or a distinct
  `conductor.name`.
- **Messaging is plain-text**. Multi-line messages work but
  embedded ANSI escape codes are typed literally.
