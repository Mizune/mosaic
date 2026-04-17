# Mosaic reference sandbox image.
#
# This image is a known-good starting point for the Docker sandbox
# feature. It follows the same shape as Anthropic's claude-code
# devcontainer:
#
#   * A minimal Node base with git + gh + curl + iptables so the
#     agent's CLIs and the init-firewall script both work.
#   * Agent CLIs pre-installed from npm so a fresh session does not
#     burn minutes downloading them every launch.
#   * A non-root `mosaic` user with passwordless sudo for the
#     firewall init, matching the "safe default + escape hatch"
#     pattern.
#   * An ENTRYPOINT that applies the network allowlist once, then
#     execs whatever argv mosaic passes (`claude`, `codex`, `shell`,
#     etc.).
#
# Users who want a different toolchain are expected to build their
# own image and set `[docker].image` in mosaic's config.toml.

FROM node:22-slim

# ---- System packages -------------------------------------------------------
#
# iptables/ipset are required by init-firewall.sh. git + openssh-client let
# the agent interact with remotes; gh covers GitHub-specific flows; curl is
# used by both the firewall DNS check and most agent tooling.
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates curl git gnupg iptables ipset sudo openssh-client less \
        procps jq && \
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | gpg --dearmor -o /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list && \
    apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends gh && \
    rm -rf /var/lib/apt/lists/*

# ---- Agent CLIs ------------------------------------------------------------
#
# Install via npm so the image is portable across architectures. Users
# who want to pin a specific release should either extend this image or
# swap it out entirely.
RUN npm install --global \
        @anthropic-ai/claude-code \
        @openai/codex \
        @google/gemini-cli \
    || true

# ---- Non-root user ---------------------------------------------------------
#
# UID/GID 1000 matches the default `node` user already on the base image;
# we just rename it so the in-container paths (/home/mosaic/...) line up
# with the bind mounts the Go wrapper emits.
RUN deluser --remove-home node 2>/dev/null || true && \
    groupadd --gid 1000 mosaic && \
    useradd --uid 1000 --gid 1000 --shell /bin/bash --create-home mosaic && \
    # Sudoers entry scoped to the firewall script so the ENTRYPOINT can
    # configure iptables without giving general root access.
    echo 'mosaic ALL=(root) NOPASSWD: /usr/local/bin/init-firewall.sh' \
        > /etc/sudoers.d/mosaic-firewall && \
    chmod 440 /etc/sudoers.d/mosaic-firewall

# ---- Firewall init script --------------------------------------------------
COPY init-firewall.sh /usr/local/bin/init-firewall.sh
RUN chmod +x /usr/local/bin/init-firewall.sh

# ---- Entrypoint ------------------------------------------------------------
#
# The entrypoint invokes the firewall script first so every outbound
# packet is gated by the allowlist before the agent starts. If the
# script fails (e.g. missing NET_ADMIN) we fall back to logging a
# warning and continue — losing network isolation is better than
# refusing to start when the user explicitly opted into docker mode.
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

USER mosaic
WORKDIR /workspace
ENV PATH="/usr/local/lib/node_modules/.bin:${PATH}"
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["bash"]
