# Running mosaic on Windows (via WSL2)

mosaic depends on tmux, Unix domain sockets, and POSIX signals, so it does
not run natively on Windows. The supported path on Windows is **WSL2 with
Ubuntu**, where mosaic runs unmodified.

This guide takes you from a fresh Windows install to a working `mosaic`
binary in under 15 minutes.

## Prerequisites

- Windows 10 build 19041+ (May 2020 Update) or Windows 11
- Administrator rights to install WSL2
- ~2 GB free disk space for Ubuntu + Go toolchain

## 1. Install WSL2 + Ubuntu

Open **PowerShell as Administrator** and run:

```powershell
wsl --install -d Ubuntu
```

Reboot when prompted. On first launch of Ubuntu you will be asked to
create a UNIX username and password — these are independent of your
Windows account.

Verify the distro is running on WSL **version 2** (not WSL1):

```powershell
wsl -l -v
```

If Ubuntu shows `VERSION 1`, upgrade it:

```powershell
wsl --set-version Ubuntu 2
```

## 2. Quick start — scripted setup

From inside the Ubuntu shell, clone mosaic and run the setup script:

```bash
git clone https://github.com/Mizune/mosaic.git
cd mosaic
bash scripts/setup-wsl2.sh
```

The script is idempotent and will:

1. Install `tmux`, `git`, `curl`, `build-essential` via `apt`
2. Verify tmux is 3.2 or newer (mosaic's minimum)
3. Install Go 1.25+ to `/usr/local/go` (skipped if already present)
4. Build the `./bin/mosaic` binary
5. Run `mosaic doctor` to confirm the environment

When it finishes, launch the TUI:

```bash
./bin/mosaic
```

If `go` is not on your PATH in the current shell, open a new Ubuntu
terminal or run `source ~/.profile`.

## 3. Manual setup (if you prefer not to run the script)

```bash
# Apt packages
sudo apt update
sudo apt install -y tmux git curl build-essential ca-certificates

# Go (replace 1.25.1 with the current release if newer)
curl -fsSL https://go.dev/dl/go1.25.1.linux-amd64.tar.gz -o /tmp/go.tgz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.tgz
echo 'export PATH="/usr/local/go/bin:$PATH"' >> ~/.profile
source ~/.profile

# Build mosaic
git clone https://github.com/Mizune/mosaic.git
cd mosaic
make build
./bin/mosaic doctor
```

For ARM-based Windows machines, replace `linux-amd64` with `linux-arm64`
in the Go download URL.

## 4. Windows Terminal recommendations

mosaic renders best in Windows Terminal (installed by default on Windows
11, available from the Microsoft Store on Windows 10):

- Set the profile's color scheme to one with **24-bit true color** —
  mosaic's themes assume it.
- Install a **Nerd Font** (e.g. JetBrainsMono Nerd Font) for the glyphs
  used in the session list and status bar.
- In the profile settings, set the starting directory to a path inside
  the WSL2 filesystem (e.g. `\\wsl$\Ubuntu\home\<you>`), not a
  `C:\` path — see the performance note below.

## 5. Claude Code integration (optional)

If you want mosaic's sub-second attention detection for Claude Code
sessions, install Claude Code **inside WSL2** (not on Windows) so it
shares the same filesystem as mosaic:

```bash
# Inside Ubuntu
npm install -g @anthropic-ai/claude-code   # or the installer of your choice
mosaic hook install
mosaic hook status   # expects "✓ mosaic hooks installed"
```

See [hooks.md](hooks.md) for how the hook plumbing works.

## 6. Known limitations on WSL2

- **Audio notifications** — `paplay` is not available by default in
  WSL2. Visual notifications via `notify-send` work if you install
  `libnotify-bin` and have WSLg enabled (Windows 11 default).
- **Filesystem performance** — keep your projects inside the Linux
  filesystem (`~/Projects/`), not under `/mnt/c/`. Cross-filesystem
  access is 10–50x slower and breaks file watchers.
- **tmux across WSL and Windows Terminal** — detach/attach works fine,
  but Ctrl+Q (mosaic's default detach binding) may conflict with
  Windows Terminal's default key bindings. Rebind either side if
  needed.
- **PID / process tooling** — `mosaic stop` uses `pgrep` and `SIGTERM`.
  These exist in WSL2 Ubuntu and behave normally; no action required.

## 7. Troubleshooting

**`tmux: command not found` after install**
Close and reopen the Ubuntu terminal. WSL2 occasionally caches PATH
from before `apt install`.

**`go: command not found` after running the script**
The installer writes `export PATH=...` to `~/.profile`. Open a new
shell or run `source ~/.profile`.

**`mosaic doctor` reports tmux too old**
Ubuntu 20.04 ships tmux 3.0. Upgrade the distro (`do-release-upgrade`)
or build tmux 3.4 from source. Ubuntu 22.04 and later are fine.

**TUI colors look wrong or glyphs are missing**
Verify Windows Terminal is being used (not the legacy conhost console)
and that a Nerd Font is selected for the Ubuntu profile.

**`mosaic` can't reach projects on `C:\`**
Move the repo inside the Linux home directory. `/mnt/c/...` works in a
pinch but filesystem notifications and Git operations will be sluggish.
