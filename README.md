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
host needs the normal Linux container utilities (`ip`, `nsenter`, `dnsmasq`, `rsync`,
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
| `nat` | managed `bridge` named `bocker-nat` | Dual-stack DHCP/RA with IPv4 and IPv6 masquerading |

`BOCKER_NETWORK` selects the default mode and defaults to `bridge`. Set
`BOCKER_BRIDGE_PARENT` when the physical parent cannot be inferred from the
default route. `BOCKER_NAT_CIDR` changes the IPv4 NAT subnet.
`BOCKER_NAT_IPV6_CIDR` accepts an IPv6 CIDR, `auto` (the default, matching
Incus managed bridge behavior), or `none` to intentionally disable IPv6. The old
`BOCKER_MACVLAN_PARENT` variable is accepted only as a migration alias.

Bocker rejects Incus implementation names such as `macvlan`, `bridged`, and
`ovn` in its public command and `Incusfile` interfaces.

## Examples

The `examples/` directory contains three independently buildable services:

| Example | Language | Port | Dependency mirror |
| --- | --- | --- | --- |
| `go-api` | Go + YAML | 8080 | `goproxy.cn`, Aliyun APK |
| `python-api` | Flask + Gunicorn + TOML | 8000 | Tsinghua PyPI, Aliyun APK |
| `node-api` | Node.js + Express + JSON | 3000 | npmmirror, Aliyun APK |

Each directory has an `Incusfile`, a multi-stage build, an OpenRC service, a
health endpoint, and a dual-stack `EXPOSE` mapping. Build and run one with:

```bash
cd examples/go-api
bocker build
bocker create
```

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
