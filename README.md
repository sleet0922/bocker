# Bocker

Bocker is a standalone Linux container tool built from the small Incus feature
set used by the original `sb_lxc` project. The final deliverable is one
`bocker` executable: it contains the container-only Incus daemon and liblxc
runtime, starts its own private daemon, and talks to it through a Bocker-owned
Unix socket. Installing the Incus CLI, daemon, service, or package is not
required.

The public command surface is intentionally narrow: container lifecycle,
image install/list/remove, export/import, port and domain settings, autostart,
and Dockerfile-like `Incusfile` builds. Clustering, virtual machines, remote
servers, storage administration, and all other Incus network drivers are not
exposed by Bocker.

## Build

```bash
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '-s -w' -o bocker .
```

The embedded runtime is currently Linux amd64. Bocker must run as root. The
host needs the normal Linux container utilities (`ip`, `dnsmasq`, `rsync`,
`setfattr`, `tar`, `unsquashfs`, and `xz`) but no external Incus installation.
On first use, Bocker creates `/var/lib/bocker`, starts its private daemon, and
initializes the same default `dir` storage pool and root-disk profile produced
by a default `incus admin init`. The embedded runtime is not installed in
`PATH`; it remains private implementation data under `/var/lib/bocker`.

## Commands

```text
bocker install [--network bridge|nat] [image] [name]
bocker list
bocker start|stop|restart [name]
bocker in [name]
bocker exec <name> <command...>
bocker remove container|image [name]
bocker export [name]
bocker import [backup.tar.gz] [name] [--network bridge|nat]
bocker set <name> port|domain|autostart|network ...
bocker build [--name name] [--network bridge|nat] [Incusfile]
bocker create [name] [--network bridge|nat]
bocker images
```

When a container name is omitted, Bocker uses a small interactive selector.
Running `bocker install` without an image first asks for `Bridge` or `NAT`;
`bocker set <name> network` also opens this network selector when the mode is
omitted. The current mode is listed first and is the default choice.

## Network model

Only two public network modes exist:

| Bocker | Incus implementation | Behavior |
| --- | --- | --- |
| `bridge` | `nictype=macvlan` | A LAN address on the detected physical parent |
| `nat` | managed `bridge` named `bocker-nat` | DHCP on `10.0.100.0/24` with IPv4 masquerading |

`BOCKER_NETWORK` selects the default mode and defaults to `bridge`. Set
`BOCKER_BRIDGE_PARENT` when the physical parent cannot be inferred from the
default route. `BOCKER_NAT_CIDR` changes the NAT subnet. The old
`BOCKER_MACVLAN_PARENT` variable is accepted only as a migration alias.

Bocker rejects Incus implementation names such as `macvlan`, `bridged`, and
`ovn` in its public command and `Incusfile` interfaces.

## Incusfile

```text
FROM debian:12
NETWORK nat
NAME web
WORKDIR /srv
ENV APP_ENV=production
COPY . .
RUN apt-get update && apt-get install -y python3
EXPOSE 8080/tcp
DOMAIN web.test
AUTOSTART on
```

Supported instructions are `FROM`, `NAME`, `NETWORK`, `WORKDIR`, `RUN`,
`COPY` (including `--from`), `ENV`, `EXPOSE`, `DOMAIN`, `AUTOSTART`, and
`TEMP ... END` for isolated build stages. Build context paths are checked for
path traversal and symbolic links are rejected.

## State

`BOCKER_STATE_DIR` changes the private state root from `/var/lib/bocker`.
Containers, images, the Unix socket, logs, and the extracted internal runtime
all live below that directory. Bocker never connects to or requires a system
Incus daemon.
