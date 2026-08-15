# Embedded container runtime

`incus-runtime.zip` is a build input embedded into the final `bocker` binary.
It is not installed as an Incus package and does not place `incus` or `incusd`
in the host `PATH`.

Source:

- Incus source: `https://github.com/lxc/incus`, `v7.3.0`.
- The `incusd` binary is built from `./cmd/incusd` with Go using
  `-trimpath -ldflags='-s -w'`.
- LXC, LXCFS, hooks, and private shared libraries are retained from the
  existing container runtime package because they are required at runtime.
- Embedded archive SHA-256: `cbae8b50290565bcd0e5615eb85d60359e9770e14d4d4c62e933249797e7fc20`.

Bocker retains only the daemon, lxcfs (including its private libfuse3
dependency), private shared libraries, and LXC hooks needed by the requested
container workflows. The Incus CLI, documentation, web UI, VM firmware, QEMU
helpers, migration tools, clustering tools, and packaging files are excluded.
VM TPM libraries, NVIDIA container libraries, and the NVIDIA hook are also
excluded because Bocker exposes containers only.

At runtime these private files are extracted below `/var/lib/bocker/runtime`.
They are implementation data owned by Bocker; the only distributed and
user-facing executable is `bocker`.
