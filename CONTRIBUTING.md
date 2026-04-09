# Contributing to mosaic

Thanks for wanting to help! mosaic is a small, opinionated project
built around a single tight loop: "make the TUI feel like a
command center for parallel agents". Every contribution should be
measured against that loop.

This guide covers how to get a working dev environment, the
conventions the codebase follows, and how to propose changes.

## Getting started

### Prerequisites

- Go 1.25 or newer (pure Go; no CGO required)
- `tmux` 3.2+
- `git` 2.20+
- `make` (for convenience — every target has a direct `go` equivalent)

Optional:

- [`vhs`](https://github.com/charmbracelet/vhs) to regenerate the
  README demo GIFs from `docs/assets/*.tape`

### Clone and build

```bash
git clone https://github.com/Mizune/mosaic
cd mosaic
make build            # ./bin/mosaic
./bin/mosaic doctor   # provisions ~/.mosaic and runs migrations
```

### Run the test suite

```bash
make test             # unit + integration, -race
make test-bdd         # Ginkgo specs in internal/
make test-e2e         # Godog Gherkin scenarios in features/
make lint             # go vet
make fmt              # gofmt
```

Aim for green on the whole tree before opening a PR.

## How the code is organised

High-level tour in [docs/architecture.md](docs/architecture.md). The
short version:

- **`cmd/mosaic/`** — cobra CLI subcommands and the TUI bootstrap.
- **`internal/`** — everything else, organised by responsibility
  (store, bus, tmux, git, worktree, claudeio, hook, live,
  conductor, service, mcp, skill, tui).
- **`pkg/transcript/`** — the only package under `pkg/` — it hosts
  the Claude Code JSONL types so external tooling can consume them.
- **`features/`** — Godog `.feature` files + step definitions.
- **`docs/`** — user-facing documentation (what you are editing
  now is also welcome there).

Cross-cutting rules:

- Every subsystem that touches external state (store, tmux, git)
  has a typed wrapper **and** a fake for tests.
- Business logic lives in `internal/service/`; CLI / TUI layers
  should be thin and call into service operations.
- The event bus (`internal/bus`) is the preferred cross-subsystem
  communication channel — subscribing is cheap, polling the
  store is expensive.

## Coding style

### General

- **Readability beats cleverness.** If a one-liner needs a
  comment to explain it, split it.
- **Comments explain `why`, not `what`.** The code already says
  what it does; use comments for the context a future reader
  cannot reconstruct from the diff.
- **Early exits over deep nesting.** `if err != nil { return ...
  }` at the top of a function is idiomatic and welcome.
- **Package doc comments are mandatory.** Every `internal/<pkg>`
  has a file whose top comment explains the package's job.
- **Exported identifiers need a doc comment** that starts with
  the identifier's name.

### Go specifics

- `gofmt` / `goimports` formatting.
- No global mutable state outside `main`. Tests rely on being
  able to instantiate `Store`, `SessionService`, etc. in
  isolation.
- Avoid third-party dependencies unless they pull their weight.
  mosaic currently depends on Bubble Tea, Lipgloss, Cobra,
  sqlx, modernc/sqlite, fsnotify, chroma, atotto/clipboard, and
  Ginkgo / Godog. Adding another library to `go.mod` is a
  conversation, not a PR.
- Error messages start lowercase and do not end with a period:
  `return fmt.Errorf("open %s: %w", path, err)`.
- Use typed sentinel errors (`var ErrFoo = errors.New("foo")`)
  when callers need to branch on error identity.

### TUI specifics

- Every render function is a **pure function** of the Model.
  Side effects live in `Update` via `tea.Cmd`s.
- Keybindings are registered in `internal/tui/keys.go` and
  respected via `keyMap.applyOverrides`. Users can remap every
  binding in `config.toml`; do not hardcode keys in view code.
- New panels should use `renderModalBox` for a consistent
  frame. Do not roll your own border style.
- When adding state to `Model`, keep it close to the feature
  it serves (e.g. `diffPanel diffPanelState`) and prefer a
  sub-struct over naked fields.
- Colour usage: reach for the theme. Plain strings render in
  the terminal's default colour, which defeats theme switching.
  Use `m.theme.Accent`, `m.theme.Text`, `m.theme.Muted`, etc.
  for every visible glyph.

## Testing expectations

- **Unit tests first.** If a pure function can be tested without
  the store / tmux / git, it should be.
- **Ginkgo BDD** for features that need the store + fakes.
  Use `BeforeEach` to spin up `store.Open(":memory:")` and a
  `tmux.NewFake()`.
- **Godog scenarios** for user journeys that touch the real
  binary. Prefer scenarios over unit tests when the behaviour
  is "the user presses X and sees Y".
- **`-race` is mandatory.** The bus and live reconciler are
  concurrent; `make test` runs with `-race`. A failing race
  detector is a blocker.
- **Keep tests fast.** The whole suite should finish in < 15s
  on an M1. If a test takes longer than a second, reconsider
  whether it belongs in the `integration` bucket.

## Commit conventions

We follow [Conventional Commits](https://www.conventionalcommits.org/)
loosely:

```
feat(tui): add next-attention jump key
fix(tmux): treat "no server running" as not found
docs(conductor): explain replay and pause
refactor(store): move SessionMCPs into its own file
test(live): reproduce stale attention suppression bug
chore: bump chroma to v2.23.1
```

- `feat` / `fix` / `refactor` / `test` / `docs` / `chore` are the
  valid types.
- Scope is optional but encouraged; pick the package name.
- Keep the first line under 72 characters.
- The body (optional) explains **why** — link to issues or plan
  docs when relevant.

## Submitting a PR

1. Branch from `main` with a descriptive name (`fix/diff-base-fallback`).
2. Make the change and add tests.
3. `make test && make lint`.
4. Rebase on main if upstream moved.
5. Open the PR with a clear title and a summary of **what + why**.
6. Include a short "how to verify" section if the change has
   user-visible behaviour.

Your first PR will get a close read. Don't take it personally —
small, tight patches with clear reasoning land fastest.

## Reporting bugs

Open a GitHub issue with:

- mosaic version (`mosaic --version`)
- Go version (`go version`)
- macOS / Linux version
- Steps to reproduce
- Expected vs actual behaviour
- Relevant excerpt from `~/.mosaic/logs/mosaic.log` (scrub any
  personal paths / secrets before pasting)

A screenshot / GIF of the TUI is worth a lot for UI bugs. We
ship `.tape` files in `docs/assets/` for recording polished demos
with [`vhs`](https://github.com/charmbracelet/vhs) — feel free to
use them to capture reproductions too.

## Release cadence

mosaic uses [SemVer](https://semver.org/) and tags releases
manually. `goreleaser` takes over from tag push onwards:

1. `git tag v0.x.y`
2. `git push --tags`
3. GitHub Actions runs `.github/workflows/release.yml` →
   `goreleaser release --clean`
4. Archives appear on the GitHub Releases page.

Only maintainers push tags; contributors land changes on `main`
and the release cadence bundles them into versioned drops.

## License

By contributing, you agree that your work will be released under
the project's [MIT License](LICENSE).
