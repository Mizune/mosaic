# Mosaic sandbox image

This directory holds a reference Docker image that mosaic can use as the
per-session sandbox when a user opts in via `--sandbox=docker` (or the TUI
wizard's Sandbox step).

It is deliberately small and opinionated:

- `node:22-slim` base
- `git`, `gh`, `curl`, `iptables`, `openssh-client`, `sudo`
- Claude Code / Codex / Gemini CLIs pre-installed via npm
- Non-root `mosaic` user (UID/GID 1000) so the `/home/mosaic/...` bind
  targets emitted by mosaic's Go wrapper line up with the user inside
  the container.
- An `ENTRYPOINT` that runs `init-firewall.sh` to apply a default-deny
  egress allowlist, then `exec`s the argv mosaic passed (typically
  `claude`, `codex`, `gemini`, or `bash`).

## Building

```sh
docker build -t mosaic-sandbox:dev docker/sandbox
```

Point mosaic at it by adding this to `~/.config/mosaic/config.toml`:

```toml
[docker]
image = "mosaic-sandbox:dev"
```

## Firewall allowlist

The built-in list in `init-firewall.sh` covers the common agent APIs
(Anthropic, OpenAI, Google), package registries (npm, PyPI) and GitHub.
Extend it without editing the script via `config.toml`:

```toml
[docker]
firewall_allowlist = ["my.private-registry.example.com"]
```

mosaic forwards the list as `MOSAIC_FW_ALLOWLIST` (comma-separated), and
the init script merges it with the built-ins.

## Auth mount layout

mosaic's wrapper assumes the following paths exist inside the image
(the entries are the *container* side of the default mounts):

- `/home/mosaic/.claude`
- `/home/mosaic/.codex`
- `/home/mosaic/.ssh`
- `/home/mosaic/.config/gh`

If you build a custom image with a different user, either keep UID 1000
for `mosaic` **or** override the mount targets by setting your own
`[docker].default_mounts`.

## Network isolation failure mode

The entrypoint logs a warning and continues when the firewall script
cannot apply (e.g. the container was launched without `NET_ADMIN`).
The agent still starts — losing the allowlist is preferable to aborting
a session the user explicitly asked for.
