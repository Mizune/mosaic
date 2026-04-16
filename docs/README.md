# mosaic documentation

| Guide | What it covers |
|---|---|
| [quick-start.md](quick-start.md) | First 15 minutes with mosaic |
| [configuration.md](configuration.md) | Every knob in `config.toml` |
| [keybindings.md](keybindings.md) | Default keymap + context-specific bindings |
| [themes.md](themes.md) | Built-in themes, first-run wizard preview, custom theme instructions |
| [diff-view.md](diff-view.md) | Diff View guide — browse, filter, search, hunk stage, commit, amend, yank |
| [conductor.md](conductor.md) | Writing and running plan.toml DAGs |
| [hooks.md](hooks.md) | Claude Code hook integration |
| [worktrees.md](worktrees.md) | Declarative worktree setup recipes |
| [architecture.md](architecture.md) | Packages, data flow, performance budget |
| [windows-wsl2-setup.md](windows-wsl2-setup.md) | Running mosaic on Windows via WSL2 |

Demo GIFs and their `.tape` source scripts live in
[`assets/`](assets/README.md). Regenerate them with
[`vhs`](https://github.com/charmbracelet/vhs):

```bash
make build
vhs docs/assets/tour.tape
```
