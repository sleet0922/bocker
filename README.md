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
bocker install [--network bridge|nat] [--permission normal|super] [image] [name]
bocker list
bocker start|stop|restart [name]
bocker in [name]
bocker exec <name> <command...>
bocker remove container|image [name]
bocker export [name]
bocker import [backup.tar.gz] [name] [--network bridge|nat]
bocker set <name> port|domain|autostart|network ...
bocker build [--name name] [--network bridge|nat] [Incusfile]
bocker create [name] [--network bridge|nat] [--permission normal|super]
bocker images
bocker completion bash|zsh|fish
bocker completion install [bash|zsh|fish]
```

When a container name is omitted, Bocker uses a small interactive selector.
Running `bocker install` without an image first asks for `Bridge` or `NAT`;
`bocker set <name> network` also opens this network selector when the mode is
omitted. The current mode is listed first and is the default choice.
Interactive `bocker install` also asks for `normal` or `super` permission;
passing `--permission` skips that prompt.

Container permissions are per-instance. `normal` is the default and keeps the
existing creation behavior. `super` enables privileged/nested LXC operation,
removes the container's AppArmor and dropped-capability restrictions, and relaxes
the inner systemd sandbox. It does not change other containers or the host's
systemd configuration:

```bash
bocker create --permission normal
bocker create --permission super
bocker install --permission super debian:12 redis
```

Use `super` only for trusted software. The mode is stored in the container's
LXC configuration and remains in effect across restarts.

## Shell completion

Print a completion script and load it for the current shell:

```bash
source <(bocker completion bash)       # Bash
eval "$(bocker completion zsh)"        # Zsh
bocker completion fish | source        # Fish
```

For a system-wide installation (run as root), use
`bocker completion install bash`, `zsh`, or `fish`. The completion script
also queries current container and image names through the hidden
`bocker __complete` endpoint.

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

## Incusfile

下面是一份可以直接复制修改的傻瓜式教程。`Incusfile` 放在项目目录根部，
构建时该目录就是 `COPY` 的上下文目录。Bocker 当前支持的指令只有：
`FROM`、`NAME`、`NETWORK`、`WORKDIR`、`RUN`、`COPY`、`ENV`、`EXPOSE`、
`DOMAIN`、`AUTOSTART`，以及用于临时构建阶段的 `TEMP ... END`。
此外，`ENTRYPOINT` 和 `CMD` 可声明镜像的默认应用命令。

### 1. 最小可运行容器

先创建一个空目录和文件：

```bash
mkdir hello && cd hello
```

写入下面的 `Incusfile`：

```text
FROM alpine/3.24
NAME hello
NETWORK nat
RUN echo 'hello from bocker' > /hello.txt
AUTOSTART on
```

逐行解释：

- `FROM alpine/3.24`：选择基础镜像。可用 `bocker build show` 查看远程镜像。
- `NAME hello`：同时作为镜像别名和默认容器名，只能使用字母、数字、点、下划线和短横线。
- `NETWORK nat`：使用 Bocker 的 NAT 网络。另一个可选值是 `bridge`，它会把容器直接接到宿主机所在局域网。
- `RUN ...`：在构建阶段容器内执行 `/bin/sh -c` 命令，命令失败就停止构建。
- `AUTOSTART on`：容器开机自启，不等同于应用进程自启。

构建和启动：

```bash
bocker build Incusfile
bocker create
bocker list
bocker exec hello cat /hello.txt
```

`bocker create` 必须在包含 `Incusfile` 的目录中执行，并且会读取其中的
`NAME`、`NETWORK`、`EXPOSE`、`DOMAIN` 和 `AUTOSTART` 设置。

### 2. 运行一个 HTTP 服务

如果不写 `CMD` 或 `ENTRYPOINT`，应用仍可通过基础镜像自己的 init 系统启动。
写了这两个指令后，Bocker 会在构建阶段自动生成应用包装器和原生服务：
Debian/Ubuntu 使用 systemd，Alpine 使用 OpenRC。基础镜像的 init 仍然是
PID 1，因此 DHCP、IPv4、IPv6 和系统服务不会被覆盖，也不需要用户手写
unit 或 init 文件。

例如，下面的声明会在容器启动时执行 `/usr/bin/python3 /opt/app.py --port 8080`：

```text
ENTRYPOINT ["/usr/bin/python3", "/opt/app.py"]
CMD ["--port", "8080"]
```

JSON 数组是推荐写法，也支持带引号的 shell-like 写法，例如
`CMD /usr/bin/python3 /opt/app.py --port 8080`。`ENTRYPOINT` 是固定命令，
`CMD` 是追加参数；只写其中一个也可以。

下面仍给出一个完全手动管理 OpenRC 服务的模板，适用于不想使用默认命令
机制、或需要自定义多个服务的镜像。

项目目录：

```text
web/
  Incusfile
  server.sh
  web.init
```

`server.sh`：

```sh
#!/bin/sh
while true; do
  printf 'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 12\r\n\r\nhello bocker\n' \\
    | nc -l -p 8080 -q 1
done
```

`web.init`：

```sh
#!/sbin/openrc-run
command="/usr/local/bin/server.sh"
command_background="yes"
pidfile="/run/web.pid"
output_log="/var/log/web.log"
error_log="/var/log/web.err"

depend() {
  need net
}
```

对应的 `Incusfile`：

```text
FROM alpine/3.24
NAME web
NETWORK nat
ENV APP_ENV=production
WORKDIR /opt/web
RUN sed -i 's#https://dl-cdn.alpinelinux.org/alpine#https://mirrors.aliyun.com/alpine#g' /etc/apk/repositories && apk add --no-cache openrc netcat-openbsd
COPY server.sh /usr/local/bin/server.sh
COPY web.init /etc/init.d/web
RUN chmod +x /usr/local/bin/server.sh /etc/init.d/web && rc-update add web default
EXPOSE 8080/tcp
DOMAIN web.test
AUTOSTART on
```

这里的关键点是：`COPY` 把宿主机文件放进镜像，`rc-update add web default`
把服务加入 Alpine 默认运行级别，`EXPOSE 8080/tcp` 会在 `bocker create`
时自动创建 IPv4/IPv6 端口映射，`DOMAIN web.test` 会把容器当前的 IPv4
和 IPv6 都写入宿主机 `/etc/hosts`。

### 3. 两种网络怎么选

```text
NETWORK nat
```

适合需要访问外网、但不希望容器直接出现在局域网中的服务。容器通常获得
`10.0.100.0/24` 和 `fd42:.../64` 地址，IPv4/IPv6 都通过 Bocker NAT 出网。

```text
NETWORK bridge
```

适合必须从局域网直接访问的服务。Bocker 对外叫 `bridge`，底层使用 Incus
的 macvlan 实现；容器会从物理网卡所在局域网获得地址。

只写一次 `NETWORK` 即可。没有写时使用全局默认值，默认是 `bridge`，也可
通过 `BOCKER_NETWORK=nat` 修改。

### 4. Debian 12 与国内 APT 源

Debian 基础镜像可能使用传统的 `/etc/apt/sources.list`，也可能使用新式的
`/etc/apt/sources.list.d/debian.sources`。为了同时兼容两种格式，可直接使用：

```text
FROM debian/12
NAME debian-demo
NETWORK nat
RUN if [ -f /etc/apt/sources.list.d/debian.sources ]; then sed -i 's|http://deb.debian.org|https://mirrors.tuna.tsinghua.edu.cn|g; s|http://security.debian.org/debian-security|https://mirrors.tuna.tsinghua.edu.cn/debian-security|g' /etc/apt/sources.list.d/debian.sources; fi && if [ -f /etc/apt/sources.list ]; then sed -i 's|http://deb.debian.org|https://mirrors.tuna.tsinghua.edu.cn|g; s|http://security.debian.org/debian-security|https://mirrors.tuna.tsinghua.edu.cn/debian-security|g' /etc/apt/sources.list; fi && apt-get update && apt-get install -y --no-install-recommends python3 ca-certificates && rm -rf /var/lib/apt/lists/*
```

这条命令已在 `debian/12` 上实际构建验证。不要只假定 `debian.sources` 存在，
否则传统布局会在 `sed` 阶段直接失败。

Debian 的应用服务通常应使用 systemd。把 unit 文件复制到
`/etc/systemd/system/` 后，执行 `systemctl enable service-name.service`，再通过
`EXPOSE` 暴露应用端口即可。

### 5. 环境变量和工作目录

```text
FROM debian/12
NAME config-demo
NETWORK nat
ENV APP_ENV=production
ENV APP_PORT=8080
WORKDIR /srv/app
COPY . .
RUN mkdir -p /var/log/app && printf '%s\\n' "$APP_ENV:$APP_PORT" > /var/log/app/build.txt
```

每条 `ENV` 只写一个变量。`WORKDIR` 会影响后续的 `RUN` 和相对路径的
`COPY`；相对 `WORKDIR` 会基于当前目录累加，绝对路径会直接切换。

### 6. 多阶段构建

编译型语言应把编译器放在中间阶段，最终镜像只保留运行时和产物：

```text
FROM alpine/3.24 AS builder
NETWORK nat
RUN apk add --no-cache go
WORKDIR /src
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY main.go .
RUN CGO_ENABLED=0 go build -o /src/app .

FROM alpine/3.24
NAME go-service
NETWORK nat
RUN apk add --no-cache ca-certificates openrc
COPY --from=builder /src/app /usr/local/bin/app
COPY app.init /etc/init.d/app
RUN chmod +x /usr/local/bin/app /etc/init.d/app && rc-update add app default
EXPOSE 8080/tcp
AUTOSTART on
```

`FROM ... AS builder` 给阶段命名，`COPY --from=builder` 从已完成的前置
阶段复制文件。也可以写 `COPY --from=0` 使用阶段编号。被引用的阶段必须
出现在当前阶段之前。

`TEMP name ... END` 是单个基础 `FROM` 下的临时构建块，适合隔离编译工具；
需要多个不同基础镜像时应使用上面的 `FROM ... AS ...` 写法，不要混用。

### 7. 行续接和常见错误

`RUN` 支持反斜杠续行：

```text
RUN apk update && \\
    apk add --no-cache curl \\
    ca-certificates
```

常见错误：

- `CMD`/`ENTRYPOINT` 的可执行文件必须存在且可执行；Bocker 不会隐式执行 shell 字符串。
- `COPY` 使用 `../`、绝对宿主机路径或符号链接：构建会拒绝路径穿越和符号链接。
- `EXPOSE` 写成 `8080` 以外的非法端口，或协议写成 `http`：协议只能是 `tcp` 或 `udp`。
- Python、Node 等服务只监听 `0.0.0.0`：如果需要 IPv6，服务应监听 `[::]` 或同时监听 IPv4/IPv6。
- `bocker create` 在错误目录运行：它只读取当前目录的 `./Incusfile`。

### 8. 一套完整检查命令

```bash
bocker build --name demo Incusfile
bocker create
bocker list
bocker exec demo rc-status
bocker set demo port list
bocker restart demo
bocker list
bocker remove container demo
bocker remove image demo
```

删除镜像是交互操作，确认提示中选择“确认删除”即可。构建上下文和
`Incusfile` 只用于生成镜像，最终运行只需要 Bocker 自己的单一二进制。

## State

`BOCKER_STATE_DIR` changes the private state root from `/var/lib/bocker`.
Containers, images, the Unix socket, logs, and the extracted internal runtime
all live below that directory. Bocker never connects to or requires a system
Incus daemon.
