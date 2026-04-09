<p align="center">
  <img src="docs/assets/logo.svg" alt="mosaic logo" width="128" height="128">
</p>

# mosaic

[![CI](https://github.com/Mizune/mosaic/actions/workflows/ci.yml/badge.svg)](https://github.com/Mizune/mosaic/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Mizune/mosaic)](https://goreportcard.com/report/github.com/Mizune/mosaic)
[![Go Reference](https://pkg.go.dev/badge/github.com/Mizune/mosaic.svg)](https://pkg.go.dev/github.com/Mizune/mosaic)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> A TUI command center for running 10+ AI coding agents in parallel.

`mosaic` is a terminal session manager for [Claude Code](https://docs.anthropic.com/claude/docs/claude-code),
Codex, Gemini and similar LLM CLI agents. It treats every agent you
spawn as a first-class session with its own git worktree, sub-agent
visibility, attention detection, and a lazygit-style TUI that lets
you bounce between dozens of simultaneous conversations without
losing context.

It is built around three observations:

1. **Sub-agents need to be visible.** When Claude's `Task` tool
   spawns a `general-purpose` or `debugger` agent, you want to see
   it running in real time, not discover after the fact.
2. **"Which agent is waiting for me?" is the most-asked question.**
   mosaic surfaces it in three independent ways (hooks, fsnotify,
   OSC escape scan) and highlights the row so you can jump to it
   with a single keystroke.
3. **Parallel work needs parallel worktrees.** Declarative setup
   recipes copy `.env`, `.vscode/`, untracked files, and run
   `pnpm install` so spinning up a fresh branch takes one keystroke.

## Demo

![mosaic tour](docs/assets/tour.gif)

_45-second tour: doctor → wizard → session list → preview tab
cycle → Diff View → Conductor panel._

Regenerate the demo GIFs from the `.tape` scripts in
[`docs/assets/`](docs/assets/README.md) using
[`vhs`](https://github.com/charmbracelet/vhs):

```bash
make build
vhs docs/assets/tour.tape         # → docs/assets/tour.gif
vhs docs/assets/attention.tape    # ⚠ attention killer feature
vhs docs/assets/theme-switch.tape # cycle through every theme
vhs docs/assets/diff-view.tape    # Diff View + hunk stage + commit
```

## Highlights

- **Sub-agent visibility** — every `Task`-tool sub-agent shows up
  inline under its parent session with its current tool and live
  elapsed counter.
- **Triple-source attention detection** — Claude Code hooks
  (sub-second latency), JSONL transcript tailing via fsnotify (~50ms),
  and tmux `capture-pane` OSC 777 extraction as a fallback. The TOP
  screen's `⚠ N waiting` badge is the single source of truth.
- **Worktree automation** — `mosaic new --worktree` creates a git
  worktree on a fresh branch, copies declared files (`.env`,
  `.vscode/`, …), optionally mirrors untracked and ignored files,
  and runs `pnpm install` et al. from a TOML recipe.
- **Conductor** — TOML-defined task DAGs dispatched to a pool of
  worker sessions with inter-agent messaging via `mosaic send`
  and a lazygit-style DAG panel (`c`).
- **Diff view** — lazygit-style file list + chroma-highlighted
  unified diff, with hunk-level staging, inline commit + amend,
  clipboard yank, and filename fuzzy search.
- **First-class polish** — 5 built-in themes, full keymap
  override via config, multi-select with visual mode, trace/pprof
  flags, Homebrew tap release pipeline, first-run onboarding
  wizard.
## Requirements

- `tmux` 3.2+
- `git` 2.20+
- `claude` / `codex` / `gemini` CLI — whichever tools you plan to
  orchestrate
- A terminal with true-color support for the richest theme experience

## Install

### Pre-built binaries

Download the latest release from the
[GitHub Releases](https://github.com/Mizune/mosaic/releases) page, or
use `gh`:

```bash
gh release download -R Mizune/mosaic --pattern 'mosaic_*_darwin_arm64.tar.gz'
tar xzf mosaic_*_darwin_arm64.tar.gz
mv mosaic /usr/local/bin/
mosaic doctor
```

Swap the `--pattern` glob for your OS/arch (`darwin_amd64`,
`linux_arm64`, `linux_amd64`).

### From source

```bash
git clone https://github.com/Mizune/mosaic.git
cd mosaic
make build
./bin/mosaic doctor
```

### go install

```bash
go install github.com/Mizune/mosaic/cmd/mosaic@latest
mosaic doctor
```

## Quick start

```bash
# 1. Run doctor to set up ~/.mosaic and verify the environment.
mosaic doctor

# 2. Install Claude Code hooks for sub-second attention updates.
mosaic hook install

# 3. Launch the TUI. First run opens a 2-step onboarding wizard
#    (theme, hooks).
mosaic
```

From inside the TUI:

- `n` to open the new-session wizard
- `Enter` / `a` to attach to the highlighted session
- `Ctrl+Q` while attached to return to mosaic
- `q` to quit

See [docs/keybindings.md](docs/keybindings.md) for the full keymap.

## Documentation

| Guide | What it covers |
|---|---|
| [docs/quick-start.md](docs/quick-start.md) | First 15 minutes with mosaic |
| [docs/configuration.md](docs/configuration.md) | `config.toml` reference |
| [docs/keybindings.md](docs/keybindings.md) | Every keystroke in every screen |
| [docs/themes.md](docs/themes.md) | Built-in themes + writing custom ones |
| [docs/diff-view.md](docs/diff-view.md) | Diff View: hunk stage, commit, yank, search |
| [docs/conductor.md](docs/conductor.md) | Writing and running plan.toml DAGs |
| [docs/hooks.md](docs/hooks.md) | Claude Code hook integration |
| [docs/worktrees.md](docs/worktrees.md) | Declarative worktree setup recipes |
| [docs/architecture.md](docs/architecture.md) | How the pieces fit together |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Dev environment + code style + PR flow |

## Feature matrix

| Feature | Implemented | Notes |
|---|---|---|
| Session CRUD | ✅ | `new / ls / attach / kill` + batch kill + fork |
| Sub-agent visibility | ✅ | JSONL tail + state machine + inline rows |
| Attention detection | ✅ | Hook / capture / OSC, with pulse + dismiss |
| Worktree automation | ✅ | Declarative copy + setup + post-create hook |
| Conductor DAG | ✅ | TOML plans + CLI + TUI panel + replay |
| Messaging | ✅ | store-backed + tmux send-keys delivery |
| Diff view | ✅ | hunk stage + commit amend + chroma + yank |
| MCP / Skill manager | ✅ | discover + attach state + TUI modal |
| Settings editor | ✅ | in-TUI toggle + save to TOML |
| Theme engine | ✅ | 5 built-in themes + config-driven |
| Keymap override | ✅ | every binding is user-overridable |
| Multi-profile | ✅ | `--profile` + TUI picker |
| First-run wizard | ✅ | theme / hooks |
| OS notifications | ✅ | macOS osascript + Linux notify-send |
| goreleaser releases | ✅ | tag → GitHub release via goreleaser |
| Performance budget | ✅ | `mosaic doctor --bench` |
| Hunk-level staging | ✅ | `H` in Diff View |

## Development

```bash
make build      # ./bin/mosaic
make test       # unit + integration tests, -race
make test-bdd   # Ginkgo specs in internal/
make test-e2e   # Godog Gherkin scenarios in features/
make lint       # go vet
make fmt        # gofmt
```

Performance budget benchmarks:

```bash
./bin/mosaic doctor --bench
```

Optional pprof server:

```bash
./bin/mosaic --pprof :6060
# then: go tool pprof http://localhost:6060/debug/pprof/profile
```

## Project layout

```
mosaic/
├── cmd/mosaic/              # cobra CLI + TUI launch
├── internal/
│   ├── bus/                 # in-process pub/sub
│   ├── claudeio/            # Claude Code transcript tail + OSC scanner
│   ├── conductor/           # plan + DAG scheduler + runner
│   ├── config/              # TOML loader + merger
│   ├── git/                 # git CLI wrapper (worktree, diff, stage, …)
│   ├── hook/                # settings.json installer + unix socket server
│   ├── live/                # bus → store reconciler + claude session binder
│   ├── mcp/                 # MCP server discovery
│   ├── mosaicpath/          # XDG paths
│   ├── service/             # session service + messaging + importer
│   ├── skill/               # Claude Code skill discovery
│   ├── store/               # sqlx + migrations + typed repositories
│   ├── tmux/                # tmux CLI backend (+ Fake for tests)
│   ├── tui/                 # Bubble Tea model + views + theme + wizards
│   └── worktree/            # declarative worktree creation
├── pkg/transcript/          # Claude Code JSONL types + parser (public)
├── features/                # Godog (.feature + step defs)
├── docs/                    # user documentation
└── .goreleaser.yaml         # release pipeline
```

## Stack

- **Go 1.25+** (pure Go, no CGO — cross-compiles trivially)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) +
  [Lipgloss](https://github.com/charmbracelet/lipgloss) — TUI framework
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) — pure-Go SQLite
- [fsnotify](https://github.com/fsnotify/fsnotify) — transcript + config watching
- [chroma v2](https://github.com/alecthomas/chroma) — diff syntax highlighting
- [atotto/clipboard](https://github.com/atotto/clipboard) — diff yank
- [Ginkgo v2](https://github.com/onsi/ginkgo) + Gomega — BDD specs
- [Godog](https://github.com/cucumber/godog) — Gherkin scenarios
- **tmux** — terminal session backend

## License

MIT — see [LICENSE](LICENSE).
