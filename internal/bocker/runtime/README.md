# Embedded container runtime

`incus-runtime.zip` is a build input embedded into the final `bocker` binary.
It is not installed as an Incus package and does not place `incus` or `incusd`
in the host `PATH`.

Source package:

- Repository: `https://pkgs.zabbly.com/incus/stable/`
- Package: `incus-base_7.2-debian12-202607262008_amd64.deb`
- Package SHA-256: `03777911b7bdb96343e03a6113b4fbb55f811d9e33c9b8fb704f194d730ae267`
- Embedded archive SHA-256: `ca82101ce67e0c38894fd4ad8e68c7fd535f4551ae3f24ce3c4fe9bad6be6eac`

The upstream container-only package contains 1326 files. Bocker retains only
the daemon, lxcfs (including its private libfuse3 dependency), private shared
libraries, and LXC hooks needed by the requested container workflows. The Incus
CLI, documentation, web UI, VM
firmware, QEMU helpers, migration tools, clustering tools, and packaging files
are excluded. The retained archive currently has 24 entries and is about
23.5 MB compressed. VM TPM libraries, NVIDIA container libraries, and the
NVIDIA hook are intentionally excluded because Bocker exposes containers only.

At runtime these private files are extracted below `/var/lib/bocker/runtime`.
They are implementation data owned by Bocker; the only distributed and
user-facing executable is `bocker`.
