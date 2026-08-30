# Bocker Incusfile v2

Bocker 是面向 Incus 的 Linux amd64 构建与运行工具。构建文件固定命名为 `Incusfile`，内容是严格 YAML；当前唯一接受的格式是 `version: 2`。旧格式和 `steps` 字段会直接报错，不做兼容解析。

## 快速开始

```sh
bocker template install debian/13 --name debian-13
bocker image build --name hello ./Incusfile
bocker image run hello --name hello-dev
bocker container exec hello-dev /bin/sh
```

镜像、容器和构建操作都通过 Bocker 后台服务执行。普通用户使用 CLI 时无需直接访问 Incus socket。

## 文件结构

```yaml
version: 2
args: {GO_VERSION: "1.26.6"}
mirror: china
name: my-app
network: nat
stages:
  - name: builder
    from: debian/13
    workdir: /src
    env: {CGO_ENABLED: "0"}
    packages: [ca-certificates, build-essential]
    tools: {go: "${GO_VERSION}"}
    files: {/src/: [go.mod, go.sum, cmd, internal]}
    fetch:
      - url: https://example.test/tool.tar.gz
        extract: /out
        format: tar.gz
        sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
    commands:
      - [mkdir, -p, /out]
      - [go, build, -trimpath, -o, /out/app, ./cmd/app]
  - from: debian/13
    packages: [ca-certificates]
    artifacts: {builder: {/out/app: /usr/local/bin/app}}
    runtime:
      env: {APP_ENV: production}
      entrypoint: [/usr/local/bin/app]
      expose: [8080, 8081/udp]
      mounts:
        - {source: ./data, target: /var/lib/app, mode: rw}
        - {source: ./app.conf, target: /etc/app.conf, readonly: true}
      autostart: true
```

Top-level fields:

- `version` is required and must be `2`.
- `args` declares build arguments. Override declared values with `--build-arg KEY=VALUE`.
- `mirror: china` switches Debian/Ubuntu and Alpine package repositories to Tsinghua mirrors. Omit it to keep the base image's repositories.
- `name` is the default image alias. `network` is `nat` or `bridge`.
- `stages` contains one or more ordered stages. Every stage needs `from`, unless top-level `base` is set.

Bocker enables Incus managed DNS on its private networks. Containers resolve
each other as `<container-name>.bocker`; records follow container lifecycle
and IP changes. Use the container name as the service name instead of an IP.
`runtime.domain` remains the host-side `/etc/hosts` mapping for a chosen
external name, and is intentionally separate from internal service discovery.

## Stage intent

`workdir` creates and selects the working directory for subsequent commands. `env` sets build environment variables and persists them into the image only when declared in the final stage. `packages` installs apt/apk packages through the base distribution's package manager.

`tools` installs an exact Go, Node, Python or Rust version with Bocker's mise integration. It is allowed only in non-final builder stages; tool caches and mise itself do not leak into the runtime stage.

`files` maps each destination to one or more paths in the Incusfile directory. A destination ending in `/` is a directory. `artifacts` maps a prior stage name or index to source/destination paths and is the only way to copy from another stage.

`fetch` downloads and extracts one archive. `format` is `tar.gz`, `tgz` or `zip`; `timeout` defaults to 30 seconds and `tries` to 1. `sha256` is optional, so trusted upstream binaries can intentionally be downloaded without a checksum. Use `move` when an archive needs a path rename.

`commands` accepts either a compact argv list or an explicit mapping:

```yaml
commands:
  - [openssl, rand, -hex, "24"]
  - run: [openssl, rand, -hex, "24"]
    capture: PASSWORD
  - shell: |
      set -eu
      printf '%s\n' "$PASSWORD" > /etc/app.env
```

argv commands never invoke a shell. Use `shell` only for pipelines, redirection, conditionals or several tightly-related setup operations. `${NAME}` references declared `args`, stage `env`, and captured command output; `$${NAME}` emits a literal `${NAME}`.

The final stage may contain `runtime` with `env`, `entrypoint`, `cmd`, `expose` (`PORT` or `PORT/udp`), `mounts`, `domain`, and `autostart`. Runtime directives are rejected in builder stages. `tools` is also rejected in the final stage.

`runtime.mounts` declares host paths to attach when `bocker image run` creates the
container. Each item requires an absolute, non-root container `target`; `source` may be an
absolute host path or a path relative to the Incusfile directory. `mode` is
`rw` (the default) or `ro`. As an alternative, use `readonly: true|false`.
Sources must exist when the image is run and must be a regular file or directory;
Incus detects the source type and creates the target with the matching
file/directory type. Mounts are stored in image metadata, so running an image
later preserves the declaration.

## Validation and tests

```sh
go test ./...
make check
make test-e2e
```

`make test-e2e` builds the projects under `testdata/yaml-projects`. Use unique image and container names when adding fixtures and always clean them in a trap. The parser rejects duplicate YAML keys, anchors, multiple documents, unknown fields, invalid package names, unsafe paths, duplicate ports, and forward stage references.

## Packaging

```sh
make build-cli-deb
make build-gui-deb
```

The GUI package embeds the matching CLI version. Release publication is intentionally explicit:

```sh
make release
git commit -am 'Release Bocker Incusfile v2'
git push origin HEAD
make publish-release PUBLISH=1 VERSION=3.3.5
```
