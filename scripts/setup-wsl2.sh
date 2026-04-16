#!/usr/bin/env bash
# setup-wsl2.sh — Provision a WSL2 (Ubuntu) environment for building and running mosaic.
#
# This script is idempotent: safe to re-run. It installs the apt packages
# mosaic depends on (tmux, git, build-essential, curl), installs a recent
# Go toolchain from the official tarball if one is not already available,
# builds the mosaic binary, and runs `mosaic doctor` to verify the setup.
#
# Usage:
#   bash scripts/setup-wsl2.sh
#
# Environment overrides:
#   GO_VERSION   — Go toolchain version to install (default: 1.25.1)
#   GO_INSTALL_DIR — Where to extract Go (default: /usr/local)
#   SKIP_BUILD   — Set to 1 to skip `make build` and `mosaic doctor`

set -euo pipefail

GO_VERSION="${GO_VERSION:-1.25.1}"
GO_INSTALL_DIR="${GO_INSTALL_DIR:-/usr/local}"
REQUIRED_TMUX_MAJOR=3
REQUIRED_TMUX_MINOR=2

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 1. Preflight checks
# ---------------------------------------------------------------------------

check_wsl2() {
  if ! grep -qi microsoft /proc/version 2>/dev/null; then
    warn "This script is intended for WSL2. /proc/version does not mention Microsoft."
    warn "Continuing anyway, but the script assumes an Ubuntu-like apt-based environment."
  else
    log "Detected WSL2 environment."
  fi
}

check_apt() {
  command -v apt-get >/dev/null 2>&1 || die "apt-get not found. This script supports Debian/Ubuntu (the WSL2 default)."
}

# ---------------------------------------------------------------------------
# 2. apt packages
# ---------------------------------------------------------------------------

install_apt_packages() {
  log "Installing apt packages: tmux git curl build-essential ca-certificates"
  sudo apt-get update -y
  sudo apt-get install -y --no-install-recommends \
    tmux \
    git \
    curl \
    build-essential \
    ca-certificates
}

# ---------------------------------------------------------------------------
# 3. tmux version check (mosaic requires >= 3.2)
# ---------------------------------------------------------------------------

check_tmux_version() {
  local raw major minor
  raw=$(tmux -V 2>/dev/null | awk '{print $2}')
  # Strip non-numeric suffixes (e.g. "3.3a" -> "3.3").
  major=${raw%%.*}
  minor=${raw#*.}
  minor=${minor%%[!0-9]*}
  if [[ -z "$major" || -z "$minor" ]]; then
    warn "Could not parse tmux version: '$raw'. Skipping version check."
    return
  fi
  if (( major > REQUIRED_TMUX_MAJOR )) ||
     (( major == REQUIRED_TMUX_MAJOR && minor >= REQUIRED_TMUX_MINOR )); then
    log "tmux ${raw} OK (>= ${REQUIRED_TMUX_MAJOR}.${REQUIRED_TMUX_MINOR})"
  else
    die "tmux ${raw} is too old. mosaic requires >= ${REQUIRED_TMUX_MAJOR}.${REQUIRED_TMUX_MINOR}. Upgrade Ubuntu (22.04+) or build tmux from source."
  fi
}

# ---------------------------------------------------------------------------
# 4. Go toolchain
# ---------------------------------------------------------------------------

go_version_ge() {
  # Returns 0 if $1 >= $2 in semver-ish comparison (major.minor.patch).
  local a="$1" b="$2"
  local IFS=.
  # shellcheck disable=SC2206
  local aa=($a) bb=($b)
  for i in 0 1 2; do
    local x=${aa[i]:-0} y=${bb[i]:-0}
    if (( x > y )); then return 0; fi
    if (( x < y )); then return 1; fi
  done
  return 0
}

install_go() {
  if command -v go >/dev/null 2>&1; then
    local current
    current=$(go version | awk '{print $3}' | sed 's/^go//')
    if go_version_ge "$current" "$GO_VERSION"; then
      log "Go ${current} already installed (>= ${GO_VERSION}). Skipping."
      return
    fi
    log "Go ${current} found but < ${GO_VERSION}. Upgrading."
  else
    log "Go not found. Installing ${GO_VERSION}."
  fi

  local arch
  case "$(uname -m)" in
    x86_64)  arch=amd64 ;;
    aarch64) arch=arm64 ;;
    *) die "Unsupported architecture: $(uname -m)" ;;
  esac

  local tarball="go${GO_VERSION}.linux-${arch}.tar.gz"
  local url="https://go.dev/dl/${tarball}"
  local tmpdir
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT

  log "Downloading ${url}"
  curl -fsSL "$url" -o "${tmpdir}/${tarball}"

  log "Extracting to ${GO_INSTALL_DIR}/go (requires sudo)"
  sudo rm -rf "${GO_INSTALL_DIR}/go"
  sudo tar -C "$GO_INSTALL_DIR" -xzf "${tmpdir}/${tarball}"

  export PATH="${GO_INSTALL_DIR}/go/bin:$PATH"

  # Persist PATH for future shells if not already present.
  local profile="${HOME}/.profile"
  # shellcheck disable=SC2016  # $PATH should be expanded at .profile load time, not now.
  local path_line='export PATH="/usr/local/go/bin:$PATH"'
  if [[ "$GO_INSTALL_DIR" == "/usr/local" ]] && ! grep -Fqs "$path_line" "$profile" 2>/dev/null; then
    echo "$path_line" >> "$profile"
    log "Appended Go to PATH in ${profile}. Re-source it or open a new shell."
  fi
}

# ---------------------------------------------------------------------------
# 5. Build mosaic and verify
# ---------------------------------------------------------------------------

build_mosaic() {
  if [[ "${SKIP_BUILD:-}" == "1" ]]; then
    log "SKIP_BUILD=1 set — skipping build and doctor."
    return
  fi

  local repo_root
  repo_root=$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel 2>/dev/null || true)
  if [[ -z "$repo_root" || ! -f "${repo_root}/Makefile" ]]; then
    warn "Not inside the mosaic git repo — skipping build. Clone the repo and re-run from there."
    return
  fi

  log "Building mosaic (make build) in ${repo_root}"
  ( cd "$repo_root" && make build )

  log "Running mosaic doctor"
  ( cd "$repo_root" && ./bin/mosaic doctor )
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
  check_wsl2
  check_apt
  install_apt_packages
  check_tmux_version
  install_go
  build_mosaic

  log "Done. Launch mosaic with: ./bin/mosaic"
  log "If 'go' is not yet on PATH in this shell, run: source ~/.profile"
}

main "$@"
