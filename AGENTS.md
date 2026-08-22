# AGENTS.md - Bocker Agent Operating Standards & Guidelines

This document specifies operational roles, subagent delegation protocols, safety guardrails, and verification workflows for AI Agents (including Antigravity / Gemini agents) operating on the **Bocker** codebase.

---

## 1. Agent Personas & Specialization

When operating on this repository, agents should adopt one or more specialized perspectives based on the sub-task:

1. **System & Container Runtime Architect**: Focuses on `internal/bocker/`, LXC/Incus daemon management, process supervision, PTY/broker forwarding, and systemd service lifecycle.
2. **Networking & Permission Specialist**: Focuses on macvlan, bridge creation (`bocker-nat`, `bocker-br0`), host routing shims, subuid/subgid idmaps, and socket security (`0666` sockets, `0711` traversal).
3. **Packaging & Release Engineer**: Focuses on `Makefile`, `build/` scripts (`nfpm-cli.yaml`, `nfpm-gui.yaml`), version alignment (`Makefile`, `main.go`, `pubspec.yaml`), and `gh` GitHub Releases.
4. **Desktop GUI Developer**: Focuses on Flutter codebase in `gui/`, Linux desktop integration, bundle compilation, and CLI interaction within the GUI.

---

## 2. Subagent Delegation Protocols

### When to Delegate to `research` Subagent
- Broad codebase exploration or multi-file pattern searches.
- Checking external documentation or searching web resources.
- Surveying log structures or historical commit diffs without modifying files.

### When to Delegate to `self` Subagent
- Running isolated, multi-step code refactoring or test runs that require isolated execution contexts.

### Rules for Subagent Invocation
- Always provide clear, actionable prompts specifying exact target files, expected outputs, and constraints.
- **Do NOT poll** or loop checking subagent status; wait reactively for system notifications.

---

## 3. Strict Execution Guardrails

### 1. Empirical Diagnosis First
- **NEVER** diagnose a daemon failure, container launch crash, or build error without inspecting actual log files.
- Inspect `/var/lib/bocker/logs/supervisor.log`, `/var/lib/bocker/logs/incusd.log`, or `journalctl -u bocker.service` before forming diagnostic hypotheses.

### 2. Zero Symptom Patches
- Do not swallow errors, ignore non-zero exit codes, or return dummy fallbacks.
- When an operation fails (e.g. Incus API call, socket connection, route replacement), propagate the detailed error upstream.

### 3. Non-Destructive Testing
- **NEVER** remove or mutate existing user containers, storage pools, or images during testing.
- Always create unique temporary test resources (e.g. `--name test-temp-8912`) and ensure they are removed in cleanup routines.
- Run user-facing CLI tests **without** `sudo` to verify unprivileged user accessibility.

### 4. Idempotency & Atomic File Operations
- All file modifications to critical system paths (`/etc/hosts`, `/etc/subuid`, `/etc/subgid`, `/etc/systemd/system/bocker.service`) must be **idempotent**.
- Use atomic write patterns (`flock` file locking + temporary file creation + atomic `rename`) to prevent corrupting system configurations upon unexpected crashes.

---

## 4. Mandatory Verification Checklist

Before reporting task completion or creating a release, every agent **MUST** complete the following verification steps:

```bash
# Step 1: Run Go tests, bash completion tests, and go vet
make check

# Step 2: Ensure git diff has no trailing whitespace or format issues
git diff --check

# Step 3: Build CLI & GUI binaries and test deb packages
make build-cli-deb
make build-gui-deb

# Step 4: Verify package metadata
dpkg-deb -f bocker.deb Package Version Architecture
dpkg-deb -f bocker-gui.deb Package Version Architecture
```

### Unprivileged User Verification Protocol
1. Verify systemd service status: `systemctl is-active bocker.service` is `active`.
2. Verify socket permissions: `/var/lib/bocker/incus/unix.socket` and `/var/lib/bocker/incus/bocker-control.socket` are `0666`.
3. Test unprivileged CLI commands without `sudo`:
   ```bash
   bocker version
   bocker container list --json
   bocker image list --json
   bocker template list --json
   ```

---

## 5. Release & Publishing Protocol

When instructed to perform a GitHub release:

1. **Verify Clean Workspace**: Confirm `make check` passes and `git status` shows a clean workspace.
2. **Version Alignment**: Confirm identical version numbers in `Makefile` (`VERSION`), `internal/bocker/main.go` (`Version`), and `gui/pubspec.yaml` (`version`).
3. **Build Release Artifacts**: Run `make release`.
4. **Git Tag & Push**:
   ```bash
   git add -A
   git commit -m "release: vX.Y.Z"
   git push origin main
   git tag -a vX.Y.Z -m "Release vX.Y.Z"
   git push origin vX.Y.Z
   ```
5. **Publish via GitHub CLI**:
   ```bash
   gh release create vX.Y.Z bocker.deb bocker-gui.deb --title "Release vX.Y.Z" --notes "..."
   ```
