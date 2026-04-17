#!/usr/bin/env bash
#
# Firewall init for the mosaic sandbox image.
#
# Design: default-deny egress, allow only the hosts named in the
# built-in list and the optional MOSAIC_FW_ALLOWLIST env var.
# Loopback and DNS stay open so the allowlist hosts can actually
# resolve. Failures during setup are logged but never abort — the
# entrypoint continues with whatever rules did land so an agent
# never fails to start over a firewall mishap.
#
# The list below is intentionally small: package registries the
# agents reach for routinely, plus the agents' own model APIs.
# Users who need more (private registries, self-hosted GitHub,
# etc.) add them to [docker].firewall_allowlist in config.toml,
# which the Go wrapper forwards as MOSAIC_FW_ALLOWLIST.

set -uo pipefail

log() {
    printf 'mosaic-firewall: %s\n' "$*" >&2
}

# Resolve a hostname to one or more IPv4 addresses. Failures are
# swallowed so one bad DNS lookup does not keep the rest of the
# allowlist from being applied.
resolve() {
    local host="$1"
    getent ahostsv4 "$host" 2>/dev/null | awk '{print $1}' | sort -u
}

# Built-in defaults: npm registry, PyPI, the major agent APIs,
# GitHub, Anthropic docs, OpenAI.
DEFAULT_HOSTS=(
    registry.npmjs.org
    pypi.org
    files.pythonhosted.org
    api.github.com
    github.com
    raw.githubusercontent.com
    objects.githubusercontent.com
    api.anthropic.com
    api.openai.com
    generativelanguage.googleapis.com
)

# User-extended hosts from config (comma-separated).
EXTRA_HOSTS=()
if [ -n "${MOSAIC_FW_ALLOWLIST:-}" ]; then
    IFS=',' read -r -a EXTRA_HOSTS <<< "$MOSAIC_FW_ALLOWLIST"
fi

# ---- Policy ---------------------------------------------------------------
# Flush previous rules so re-runs are idempotent, then set the chain
# policies to ACCEPT-except-OUTPUT.
iptables -F
iptables -X
iptables -P INPUT ACCEPT
iptables -P FORWARD ACCEPT
iptables -P OUTPUT DROP

# Loopback + established connections must stay open or nothing
# downloads, not even DNS replies.
iptables -A INPUT -i lo -j ACCEPT
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT

# DNS: allow outbound to whatever resolver /etc/resolv.conf names.
# Agents routinely resolve domains late (e.g. gh auth flows), so a
# wildcard UDP/53 is cheaper than maintaining a resolver list.
iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT

# Allow 443/80 to every host in the combined allowlist.
for host in "${DEFAULT_HOSTS[@]}" "${EXTRA_HOSTS[@]}"; do
    [ -z "$host" ] && continue
    while read -r ip; do
        [ -z "$ip" ] && continue
        iptables -A OUTPUT -p tcp -d "$ip" --dport 443 -j ACCEPT
        iptables -A OUTPUT -p tcp -d "$ip" --dport 80  -j ACCEPT
    done < <(resolve "$host")
done

log "firewall ready (default-deny egress, allowlist hosts: ${#DEFAULT_HOSTS[@]} built-in, ${#EXTRA_HOSTS[@]} user)"
