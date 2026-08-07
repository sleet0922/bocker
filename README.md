# Bocker

Bocker 是一个面向 Linux 的独立容器工具。它把容器管理需要的 Incus
守护进程和 LXC 运行时嵌入单个 `bocker` 可执行文件，通过自己的 Unix
socket 管理容器，不依赖系统安装的 Incus CLI 或 Incus 守护进程。

Bocker 专注于常用工作流：安装镜像、创建和管理容器、配置网络和端口、
导入导出备份，以及使用 Dockerfile 风格的 `Incusfile` 构建镜像。

## 1. 环境要求

- Linux amd64。当前嵌入式运行时不支持其他架构。
- 必须以 root 运行。容器运行时需要管理 namespace、cgroup、网络、挂载和宿主机存储。
- 宿主机需要以下命令：`ip`、`nsenter`、`dnsmasq`、`rsync`、`tar`、`unsquashfs` 和 `xz`。
- 首次以 root 运行时，如果缺少 `setfattr`，Bocker 会直接执行
  `apt-get update` 和 `apt-get install -y attr`，不需要 `sudo`。
- 需要访问 `https://images.linuxcontainers.org/` 下载公开镜像。

Bocker 的状态默认存放在 `/var/lib/bocker`，包括容器、镜像、Unix socket、
日志和解压后的私有运行时。运行时不会安装到系统 `PATH`。

## 2. 安装

从源码构建（需要 Go 1.25 或更高版本）：

```bash
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags '-s -w' -o bocker ./cmd/bocker
```

安装到系统：

```bash
install -m 0755 bocker /usr/local/bin/bocker
bocker --version
```

第一次执行需要等待 Bocker 解压运行时、启动 `bocker.service` 并初始化默认
存储池。服务日志位于 `/var/lib/bocker/logs/`。

### Ubuntu 桌面 GUI

`gui/` 提供了基于 Flutter Material 3 的 Ubuntu 桌面界面，覆盖容器、镜像、
构建和常用设置管理，同时保留所有原有 CLI 用法。GUI 通过 Bocker 的私有本地
权限代理执行操作：普通桌面用户首次操作授权后，后续操作无需重复输入密码。
GUI 包会携带同版本 `bocker` 二进制，并优先使用该副本以避免版本不兼容。

开发运行：

```bash
cd gui
BOCKER_BINARY="$PWD/../bocker" flutter run -d linux
```

构建桌面包：

```bash
cd gui
./build_release.sh
```

源码采用标准 Go 项目布局：`cmd/bocker/` 是精简的可执行入口，
`internal/bocker/` 包含 CLI 命令、容器运行时适配和对应测试，`gui/` 是独立的
Flutter 桌面前端。

## 3. 快速开始

推荐明确指定 NAT 网络和镜像，避免进入交互菜单：

```bash
bocker install --network nat --permission normal debian:12 debian-12
bocker list
bocker exec debian-12 cat /etc/os-release
bocker in debian-12
```

也可以运行交互式安装：

```bash
bocker install
```

它会依次让你选择网络模式、权限模式、发行版和版本，然后询问容器名。
省略生命周期命令的容器名时，也会打开交互式选择菜单。

## 4. 命令参考

### 容器和镜像

| 命令 | 作用 |
| --- | --- |
| `bocker install [image] [name]` | 下载镜像并创建、启动容器 |
| `bocker list` / `bocker ls` | 列出容器 |
| `bocker start [name]` | 启动容器 |
| `bocker stop [name]` | 停止容器 |
| `bocker restart [name]` | 重启容器 |
| `bocker in [name]` | 进入容器 shell |
| `bocker exec <name> <command...>` | 在容器内执行非交互命令 |
| `bocker remove container [name]` | 删除容器 |
| `bocker remove image [alias]` | 删除镜像别名 |
| `bocker images` | 列出本地镜像别名 |
| `bocker export [name]` | 导出容器备份 |
| `bocker import [file] [name]` | 导入容器备份 |

旧别名 `run`（等同于 `create`）和 `uninstall`（等同于删除容器）仍保留兼容。

### 构建和创建

| 命令 | 作用 |
| --- | --- |
| `bocker build [Incusfile]` | 构建镜像，默认读取当前目录的 `./Incusfile` |
| `bocker build --name <name> [Incusfile]` | 覆盖镜像别名 |
| `bocker build --network bridge\|nat [Incusfile]` | 覆盖构建阶段网络模式 |
| `bocker build show` | 列出可用于 `FROM` 的远程基础镜像 |
| `bocker create [name]` | 从当前目录的 `Incusfile` 创建并启动容器 |

典型流程是先 `build` 发布镜像，再用 `create` 启动容器：

```bash
bocker build Incusfile
bocker create
bocker list
```

### 容器设置

```bash
bocker set <name> port 8080:80/tcp
bocker set <name> port list
bocker set <name> port rm 8080/tcp
bocker set <name> domain web.test
bocker set <name> domain --unset
bocker set <name> autostart on
bocker set <name> network nat
```

切换网络前必须先停止容器。省略 `set` 的子命令会进入设置菜单。

### 全局选项和环境变量

```text
--network bridge|nat
--permission normal|super
BOCKER_NETWORK=bridge|nat
BOCKER_BRIDGE_PARENT=<宿主机物理网卡>
BOCKER_NAT_CIDR=<IPv4 CIDR>
BOCKER_NAT_IPV6_CIDR=<IPv6 CIDR|auto|none>
BOCKER_STATE_DIR=<状态目录>
```

命令行选项优先于环境变量。`BOCKER_MACVLAN_PARENT` 仍作为旧版本的网卡
配置别名接受。

## 5. 网络模式

| 模式 | 实现 | 适用场景 |
| --- | --- | --- |
| `nat` | Bocker 管理的 `bocker-nat` bridge，IPv4/IPv6 NAT | 默认推荐，容器访问外网但不直接暴露在局域网 |
| `bridge` | Incus macvlan，使用宿主机物理网卡 | 容器需要直接获得局域网地址 |

默认模式是 `bridge`，可用 `BOCKER_NETWORK=nat` 改为 NAT。Bridge 模式会
自动探测默认路由的物理网卡；探测失败时设置 `BOCKER_BRIDGE_PARENT`。

NAT 默认使用 `10.0.100.0/24`，并自动创建 IPv6 ULA 网络。可用
`BOCKER_NAT_CIDR` 和 `BOCKER_NAT_IPV6_CIDR` 调整，IPv6 设为 `none` 可关闭。
Bocker 对外只接受 `bridge` 和 `nat`，不接受底层 Incus 名称如 `macvlan`、
`bridged` 或 `ovn`。

## 6. 容器权限

权限按容器保存，默认是 `normal`：

```bash
bocker install --permission normal debian:12 debian-normal
bocker install --permission super debian:12 debian-super
bocker create --permission super
```

`super` 会启用嵌套 LXC、移除容器 AppArmor 和 capability 限制，并放宽容器
内部 systemd 的隔离设置。它不会修改其他容器或宿主机的 systemd 配置，
但只应对可信软件使用。

## 7. Incusfile

`Incusfile` 是 Bocker 的构建描述文件。`bocker build` 的上下文目录就是
`Incusfile` 所在目录，`COPY` 不能访问上下文之外的文件或符号链接。

### 指令

| 指令 | 说明 |
| --- | --- |
| `FROM <image> [AS <stage>]` | 基础镜像并开始一个构建阶段 |
| `NAME <name>` | 最终镜像别名和默认容器名 |
| `NETWORK bridge\|nat` | 构建和创建时的网络模式 |
| `WORKDIR <path>` | 设置后续 `RUN` 和相对 `COPY` 的工作目录 |
| `RUN <command>` | 在构建容器内通过 `/bin/sh -c` 执行 |
| `COPY <src> <dst>` | 从构建上下文复制文件 |
| `COPY --from=<stage> <src> <dst>` | 从前置构建阶段复制产物 |
| `ENV KEY=VALUE` | 设置镜像环境变量 |
| `EXPOSE <port>[/tcp\|udp]` | 创建运行时端口映射 |
| `DOMAIN <domain>` | 启动时更新宿主机 `/etc/hosts` |
| `AUTOSTART on\|off` | 设置容器开机自启动 |
| `ENTRYPOINT [...]` | 设置固定应用命令 |
| `CMD [...]` | 设置默认命令或参数 |
| `TEMP <name> ... END` | 在临时阶段安装构建工具并复制产物 |

### 最小示例

```text
FROM alpine/3.24
NAME hello
NETWORK nat
RUN echo 'hello from bocker' > /hello.txt
AUTOSTART on
```

```bash
bocker build Incusfile
bocker create
bocker exec hello cat /hello.txt
```

### 运行应用

定义 `ENTRYPOINT` 或 `CMD` 后，Bocker 会为最终镜像生成原生服务：Debian/Ubuntu
使用 systemd，Alpine 使用 OpenRC。容器自己的 init 仍是 PID 1。

```text
FROM debian/12
NAME web
NETWORK nat
RUN apt-get update && apt-get install -y --no-install-recommends python3
COPY app.py /opt/app.py
ENTRYPOINT ["/usr/bin/python3", "/opt/app.py"]
CMD ["--port", "8080"]
EXPOSE 8080/tcp
DOMAIN web.test
AUTOSTART on
```

`ENTRYPOINT` 是固定命令，`CMD` 是追加参数。JSON 数组是推荐写法，也支持
带引号的 shell-like 写法。应用需要监听 `0.0.0.0`；需要 IPv6 时还应监听
`[::]`。

### 多阶段构建

编译器可以放在前置阶段，最终镜像只复制运行产物：

```text
FROM alpine/3.24 AS builder
NETWORK nat
RUN apk add --no-cache go
WORKDIR /src
COPY go.mod .
COPY main.go .
RUN go build -o /src/app .

FROM alpine/3.24
NAME go-service
NETWORK nat
COPY --from=builder /src/app /usr/local/bin/app
ENTRYPOINT ["/usr/local/bin/app"]
EXPOSE 8080/tcp
```

`COPY --from` 只能引用当前阶段之前的阶段。`TEMP name ... END` 适合单个
基础镜像下隔离编译工具链，临时阶段不会进入最终镜像。

## 8. 故障排查

查看版本和帮助：

```bash
bocker --version
bocker help
```

检查内置服务：

```bash
systemctl status bocker.service --no-pager
journalctl -u bocker.service -n 50 --no-pager
tail -n 80 /var/lib/bocker/logs/incusd.log
```

常见问题：

- 报 `Bocker must run as root`：切换到 root，或使用 `sudo bocker ...`。
- 服务因 `setfattr` 退出：确认已安装 `attr`；root 首次运行会自动处理。
- Bridge 无法创建：设置正确的 `BOCKER_BRIDGE_PARENT`，或改用 `--network nat`。
- `bocker create` 找不到 `Incusfile`：在包含 `./Incusfile` 的目录执行。
- 镜像列表获取失败：检查宿主机能否访问 `https://images.linuxcontainers.org/`。

## 9. 状态目录

默认状态目录是 `/var/lib/bocker`，可通过 `BOCKER_STATE_DIR` 修改：

```bash
BOCKER_STATE_DIR=/srv/bocker bocker list
```

该目录包含容器、镜像、Unix socket、日志、运行时文件和守护进程状态。
Bocker 不连接系统 Incus 服务。
