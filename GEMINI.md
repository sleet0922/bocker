# GEMINI.md - Bocker AI Assistant Guide

This document provides project context, architectural rules, environment assumptions, and development guidelines for Gemini and AI coding assistants working on the **Bocker** repository.

---

## 1. Project Overview & Core Intent

**Bocker** is a container management tool for Linux `amd64`, providing a Go CLI binary (`bocker`) and a Flutter desktop GUI (`bocker-gui`). It embeds a container-only Incus/LXC runtime directly into Bocker, removing any external dependency on system-installed Incus CLI or system daemon services.

### Product Goals & Permission Model
* **Out-of-the-box Unprivileged Execution**: Normal local users running public `bocker` CLI commands do **NOT** need `sudo`, do **NOT** need to join an `lxd` group, and do **NOT** need manual initialization.
* **Simplified Local Permission Model**: Bocker's API socket (`/var/lib/bocker/incus/unix.socket`) and control broker socket (`/var/lib/bocker/incus/bocker-control.socket`) are open to all local users (`0666` permissions). Any local user can execute full container lifecycle operations.
* **DO NOT** attempt to add `sudo` prompts, PolicyKit integration, `lxd` group checks, or additional privilege checks to the public CLI commands.

---

## 2. Directory Layout & Key Modules

```text
.
├── cmd/bocker/                # Minimal Go entrypoint main.go
├── internal/bocker/           # Core logic (CLI subcommands, Incus API client, embedded daemon, network, permission)
│   ├── runtime/               # Embedded runtime zip archive (incus-runtime.zip)
│   └── embedded_daemon_linux.go # Root supervisor, Incus daemon lifecycle, systemd unit generation
├── gui/                       # Flutter Linux GUI app (uses bundled bocker CLI)
├── build/                     # nfpm deb configurations (nfpm-cli.yaml, nfpm-gui.yaml) & deb lifecycle scripts
├── completions/               # Bash completion script (bocker) & completion test suite
├── Makefile                   # Build targets (build-cli, build-gui, check, release, etc.)
├── README.md                  # User-facing installation, usage, and architecture documentation
└── PROJECT_PROMPT.md          # Project iteration prompt and core product constraints
```

---

## 3. System Architecture & Runtime Paths

### Systemd Service & Broker
* **Service Name**: `bocker.service`
* **Unit File Location**: `/etc/systemd/system/bocker.service`
* **Service Executable**: `/var/lib/bocker/bin/bocker-daemon __daemon`
* **State Directory (`BOCKER_STATE_DIR`)**: Default `/var/lib/bocker` (Directory permissions `0711`).

### Core Runtime Paths

| Path | Purpose | Permissions |
| :--- | :--- | :--- |
| `/var/lib/bocker/bin/bocker-daemon` | Root daemon executable | `0755` |
| `/var/lib/bocker/runtime/` | Extracted private Incus runtime (`bin/incusd`, `bin/lxcfs`, `lib/`) | `0711` |
| `/var/lib/bocker/incus/` | Incus storage pool, sqlite database, certificates, and sockets | `0711` |
| `/var/lib/bocker/incus/unix.socket` | Incus native API Unix Socket | `0666` |
| `/var/lib/bocker/incus/bocker-control.socket` | Bocker Control Broker Unix Socket | `0666` |
| `/var/lib/bocker/logs/` | Daemon log files (`incusd.log`, `supervisor.log`, `lxcfs.log`) | `0700` |
| `/var/lib/incus-lxcfs` | LXCFS mount point for `/proc` resource isolation | `0700` |
| `/opt/incus/lib/lxc/rootfs` | Compatibility mount staging directory required by `liblxc` | `0755` |

---

## 4. Networking & Permission Modes

### Network Modes (`--network`)
* **`nat`** (Default fallback for wireless / private networks):
  - Uses an Incus-managed Linux bridge `bocker-nat` (default subnet `10.0.100.1/24`).
  - Automatically runs `dnsmasq` for DHCP/DNS and sets up IPv4/IPv6 iptables NAT masquerading.
* **`bridge`** (Default mode):
  - Primary implementation uses Linux `macvlan` attached to the default route host interface (`parent`).
  - **Wireless/Restricted Auto-fallback**: When `parent` is a wireless interface (e.g., Wi-Fi `wlp...` / `wlan...`), `macvlan` cannot transmit frames due to 802.11 3-address frame limitations. Bocker automatically provisions and falls back to a managed bridge `bocker-br0` (`10.0.200.1/24` with DHCP/NAT).
  - Automatically configures host-container routing via `AutoConfigureHostBridge`.

### Container Permission Modes (`--permission`)
* **`normal`** (Default):
  - Uses Incus unprivileged containers (`security.privileged=false`).
  - Uses AppArmor profiles and capability isolation. Automatically configures root `/etc/subuid` and `/etc/subgid` mappings (`root:1000000:1000000000`).
* **`super`**:
  - Enables privileged container mode (`security.privileged=true`), nested LXC, and removes container AppArmor and capability restrictions for trusted workloads.

---

## 5. Development & Testing Commands

Before committing or completing any modifications, always execute verification commands:

```bash
# 1. Run unit tests, bash completion tests, and go vet
make check

# 2. Build standalone CLI binary
make build-cli

# 3. Build Flutter GUI bundle
make build-gui

# 4. Build deb packages using nfpm
make build-cli-deb
make build-gui-deb

# 5. Build full release binaries and deb packages
make release
```

---

## 6. Version & Release Rules

When releasing a new version, version strings must be updated in sync across three authoritative files:
1. `Makefile` -> `VERSION ?= X.Y.Z`
2. `internal/bocker/main.go` -> `const Version = "X.Y.Z"`
3. `gui/pubspec.yaml` -> `version: X.Y.Z+N`

Release process:
```bash
git add -A
git commit -m "..."
git push origin main
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
gh release create vX.Y.Z bocker.deb bocker-gui.deb --title "Release vX.Y.Z" --notes "..."
```

---

## 7. Guidelines for AI Assistants

1. **Do Not Swallowing Errors**: Never fix errors by masking symptoms, swallowing exceptions, or returning dummy fallbacks.
2. **Preserve Socket Model**: Always maintain `0666` socket permissions and execute-only `0711` directory traversal permissions.
3. **Streaming & PTY Forwarding**: The control broker (`bocker-control.socket`) must stream stdout/stderr in real time for long-running operations (`image build`, `template install`) and allocate PTY for interactive terminals.
4. **Idempotency & Clean Cleanup**: Changes to `/etc/hosts`, `/etc/subuid`, `/etc/subgid`, and systemd service files must be idempotent and use atomic replacements (e.g. temporary file + rename + flock).
