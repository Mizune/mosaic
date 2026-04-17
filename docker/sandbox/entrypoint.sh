#!/usr/bin/env bash
#
# Entrypoint for the mosaic sandbox image.
#
# Responsibilities:
#   1. Run init-firewall.sh once so every outbound packet from the
#      agent is gated by an allowlist. Failure is logged but not
#      fatal: the agent should still run even in environments that
#      refuse NET_ADMIN (e.g. some CI runners), and the user has
#      already opted in to docker mode knowing what it implies.
#   2. `exec "$@"` the actual agent argv mosaic passed so signals
#      and exit codes propagate to the tmux pane without an
#      intermediate shell.

set -u

if [ -x /usr/local/bin/init-firewall.sh ]; then
    if ! sudo -n /usr/local/bin/init-firewall.sh 2>&1; then
        echo "mosaic-sandbox: firewall init failed (continuing without network isolation)" >&2
    fi
fi

# exec so signals forward cleanly and `ps` inside the container
# shows the real command, not a shell wrapper.
exec "$@"
