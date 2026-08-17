# Bocker

Bocker 是一个面向 Linux 的独立容器工具。它把容器管理需要的 Incus
守护进程和 LXC 运行时嵌入单个 `bocker` 可执行文件，通过自己的 Unix
socket 管理容器，不依赖系统安装的 Incus CLI 或 Incus 守护进程。

Bocker 专注于常用工作流：安装镜像、创建和管理容器、配置网络和端口、
导入导出备份，以及使用 Dockerfile 风格的 `Incusfile` 构建镜像。

## 1. 环境要求

- Linux amd64。当前嵌入式运行时不支持其他架构。
- 容器运行时由 `bocker.service` 负责宿主机 namespace、cgroup、网络、挂载和存储；
  日常 CLI 操作通过本地 Unix socket 完成，不需要 root 或 `sudo`。
- 控制 socket 对本机用户开放，所有能登录该主机的用户都可管理 Bocker 容器并触发
  Bocker 支持的宿主机网络与 hosts 修改。这是单机共享管理模型，不是不同本机用户之间的
  权限隔离边界；不要在不互信的多用户主机上部署 Bocker。
- 宿主机需要以下命令：`ip`、`nsenter`、`dnsmasq`、`rsync`、`tar`、`unsquashfs` 和 `xz`。
- 首次部署后台服务时，如果缺少 `setfattr`，管理员需要安装 `attr` 软件包；服务启动后
  普通用户不需要为任何 Bocker 命令加 `sudo`。
- 需要访问 `https://images.linuxcontainers.org/` 下载公开镜像。

Bocker 的状态默认存放在 `/var/lib/bocker`，包括容器、镜像、Unix socket、
日志和解压后的私有运行时。运行时不会安装到系统 `PATH`。
systemd 使用 `/var/lib/bocker/bin/bocker-daemon` 中的受管副本启动守护进程；
CLI 或 GUI 升级后会自动同步该副本并迁移服务配置。

## 2. 安装

从源码构建（需要 Go 1.25 或更高版本）：

```bash
make build-cli
```

安装到系统：

```bash
install -m 0755 bocker /usr/local/bin/bocker
install -m 0644 completions/bocker /usr/share/bash-completion/completions/bocker
bocker --version
```

Debian 包安装时会自动初始化并启动后台服务；完成后，日常的 `bocker` CLI 和 GUI
操作都直接以普通用户运行，不需要 `sudo`。

Debian 包按标准系统目录安装且不创建额外软链接：CLI 位于 `/usr/bin/bocker`，Bash
补全位于 `/usr/share/bash-completion/completions/bocker`，GUI 私有运行文件位于
`/usr/lib/bocker-gui`，桌面入口位于 `/usr/share/applications`，图标位于
`/usr/share/pixmaps`。源码手工安装的 CLI 使用 `/usr/local/bin/bocker`，两者不混用。

Debian 包会自动安装 Bash 补全文件。重新打开终端后，输入 `bocker ` 并按 Tab
即可补全 `template`、`image`、`container`、动作和常用选项。

第一次执行需要等待 Bocker 解压运行时、启动 `bocker.service` 并初始化默认
存储池。服务日志位于 `/var/lib/bocker/logs/`。

### Ubuntu 桌面 GUI

`gui/` 提供了基于 Flutter Material 3 的 Ubuntu 桌面界面，覆盖容器、镜像、
构建和常用设置管理，同时保留所有原有 CLI 用法。GUI 直接调用同捆 CLI；CLI
通过 Bocker 后台 socket 执行需要宿主机权限的操作，不弹出提权提示。
GUI 包会携带同版本 `bocker` 二进制，并优先使用该副本以避免版本不兼容。

开发运行：

```bash
cd gui
BOCKER_BINARY="$PWD/../bocker" flutter run -d linux
```

构建桌面包：

```bash
make build-gui
```

`make build-cli` 仅构建独立终端版 `bocker`，适合服务器或只使用命令行的环境。
`make build-gui` 构建 Ubuntu 桌面包，并将相同版本的 `bocker` 放入 GUI bundle。
GUI bundle 自带 `install_desktop.sh`，直接以当前桌面用户执行即可将 GUI 安装到
`~/.local/opt/bocker-gui`，并注册应用菜单和桌面启动器；不要通过 `sudo` 执行此脚本。
从 GitHub 下载 GUI 包时，解压后进入 `bundle/` 执行 `./install_desktop.sh` 即可完成
同样的桌面安装。
`make build` 是 `make build-cli` 的简写。

源码采用标准 Go 项目布局：`cmd/bocker/` 是精简的可执行入口，
`internal/bocker/` 包含 CLI 命令、容器运行时适配和对应测试，`gui/` 是独立的
Flutter 桌面前端。

### 卸载 Debian 包

只移除程序包并保留容器数据：

```bash
sudo systemctl disable --now bocker.service 2>/dev/null || true
sudo apt purge bocker bocker-gui
sudo systemctl daemon-reload
```

如果确认不再需要容器、镜像和 Bocker 运行时，再执行完整清理。命令只针对
Bocker 明确创建的路径，不要把 `/opt/incus` 或其他 Incus 数据目录整体删除：

```bash
sudo rm -f /etc/systemd/system/bocker.service
sudo rm -f /etc/systemd/system/service.d/90-bocker-super.conf
sudo rm -f /usr/local/bin/bocker
sudo rm -f /usr/bin/bocker
sudo rm -f /usr/share/bash-completion/completions/bocker
sudo rm -f /usr/share/applications/io.bocker.bocker_gui.desktop
sudo rm -f /usr/share/pixmaps/io.bocker.bocker_gui.png
sudo rm -rf --one-file-system /usr/lib/bocker-gui
sudo rm -rf --one-file-system /var/lib/bocker
sudo rm -rf --one-file-system /var/lib/incus-lxcfs
sudo rm -rf --one-file-system /opt/incus/lib/lxc/rootfs
sudo systemctl daemon-reload
```

如果曾配置容器域名，先备份 `/etc/hosts`，再删除包含 `# bocker:` 标记的行；
停止服务后，若仍有 Bocker 专用网络设备，再确认名称后删除 Ethernet shim
`bocker-shim0`、Wi-Fi 回退桥 `bocker-br0` 或 NAT 桥 `bocker-nat`，不要删除其他网络设备：

```bash
sudo cp -a /etc/hosts /etc/hosts.bocker-uninstall-backup
sudo sed -i '/# bocker:/d' /etc/hosts
ip link show bocker-shim0 2>/dev/null && sudo ip link delete bocker-shim0 || true
ip link show bocker-br0 2>/dev/null && sudo ip link delete bocker-br0 || true
ip link show bocker-nat 2>/dev/null && sudo ip link delete bocker-nat || true
```

如果曾经用 bundle 安装过当前用户 GUI，还要以该用户执行：

```bash
rm -rf --one-file-system "$HOME/.local/opt/bocker-gui"
rm -f "$HOME/.local/share/applications/io.bocker.bocker_gui.desktop"
desktop_dir="$(xdg-user-dir DESKTOP 2>/dev/null || printf '%s/Desktop' "$HOME")"
rm -f "$desktop_dir/Bocker GUI.desktop"
```

## 3. 快速开始

推荐明确指定 NAT 网络和镜像，避免进入交互菜单：

```bash
bocker template list
bocker template install debian:12 --name debian-12 --network nat --permission normal
bocker container list
bocker container exec debian-12 cat /etc/os-release
bocker container shell debian-12
```

也可以运行交互式安装：

```bash
bocker template install
```

它会依次让你选择网络模式、权限模式、发行版和版本，然后询问容器名。
省略生命周期命令的容器名时，也会打开交互式选择菜单。

## 4. 命令参考

命令使用统一的 `bocker <资源> <动作>` 结构。三个资源分别是远程模板
`template`、本地镜像 `image` 和已有容器 `container`。

所有操作都必须使用完整的资源命令，不提供顶层快捷命令。省略模板、镜像或
容器名时，可交互操作会打开选择菜单。

`container exec` 按参数数组直接执行，不会隐式经过宿主机 shell；需要管道、
重定向等 shell 语法时，显式使用 `sh -c`，例如
`bocker container exec demo sh -c 'id | cat'`。交互式菜单和 GUI 里输入的
命令行会按 shell 风格拆分为参数（支持单双引号和反斜杠转义，不做变量展开），
因此 `ls -la /tmp` 会得到 `ls`、`-la`、`/tmp` 三个独立参数。

只输入 `bocker template`、`bocker image` 或 `bocker container` 会打开对应的
动作菜单。列表命令的 `--json` 用于 GUI 和脚本，它会输出机器可解析的 JSON
数组；普通终端查看列表时不需要使用。

### 远程模板

| 命令 | 作用 |
| --- | --- |
| `bocker template` | 打开模板操作菜单 |
| `bocker template list [--json]` | 列出 Debian、Ubuntu 等可以安装的模板 |
| `bocker template install [template] [--name <name>]` | 选择或指定模板，创建并启动容器 |

### 本地镜像

| 命令 | 作用 |
| --- | --- |
| `bocker image` | 打开镜像操作菜单 |
| `bocker image build [Incusfile]` | 构建镜像，默认读取当前目录的 `./Incusfile` |
| `bocker image build --name <name> [Incusfile]` | 覆盖镜像名称 |
| `bocker image build --network bridge\|nat [Incusfile]` | 覆盖构建阶段网络模式 |
| `bocker image build --permission normal\|super [Incusfile]` | 设置所有构建阶段权限；Debian 13 systemd 构建可用 `super` |
| `bocker image build --build-arg KEY=VALUE [Incusfile]` | 覆盖构建期 `ARG`；可重复指定 |
| `bocker image list [--json]` | 列出本地镜像 |
| `bocker image run [image] [--name <name>]` | 选择或指定本地镜像，创建并启动容器 |
| `bocker image remove [image]` | 删除本地镜像 |

典型流程是先构建本地镜像，再用该镜像启动容器：

```bash
bocker image build --name hello-image Incusfile
bocker image run hello-image --name hello
bocker container list
```

### 已有容器

| 命令 | 作用 |
| --- | --- |
| `bocker container` | 打开容器操作菜单 |
| `bocker container list [--json]` | 列出容器 |
| `bocker container start [name]` | 启动容器 |
| `bocker container stop [name]` | 停止容器 |
| `bocker container restart [name]` | 重启容器 |
| `bocker container shell [name]` | 进入容器 shell |
| `bocker container exec <name> <command...>` | 在容器内执行非交互命令 |
| `bocker container remove [name]` | 删除容器 |
| `bocker container export [name]` | 导出容器备份 |
| `bocker container import [file] [name]` | 导入容器备份 |

### 容器设置

```bash
bocker container set <name> port 8080:80/tcp
bocker container set <name> port list
bocker container set <name> port rm 8080/tcp
bocker container set <name> domain web.test
bocker container set <name> domain --unset
bocker container set <name> autostart on
bocker container set <name> network nat
```

切换网络前必须先执行 `bocker container stop <name>`。省略 `set` 的设置项会进入菜单。

### 网络、权限和环境变量

```text
--network bridge|nat
--permission normal|super
BOCKER_NETWORK=bridge|nat
BOCKER_BRIDGE_PARENT=<宿主机物理网卡>
BOCKER_BRIDGE_CIDR=<无线回退 Bridge 的 IPv4 CIDR>
BOCKER_NAT_CIDR=<IPv4 CIDR>
BOCKER_NAT_IPV6_CIDR=<IPv6 CIDR|auto|none>
BOCKER_STATE_DIR=<状态目录>
BOCKER_IMAGE_SERVER=<SimpleStreams 镜像源>
BOCKER_AUTO_APT_MIRROR=on|off
BOCKER_APT_MIRROR_URL=<apt 镜像站根地址>
```

`--network` 用于模板安装、镜像构建/运行和容器导入；`--permission` 用于模板
安装、镜像运行和容器导入。网络命令行选项优先于 `BOCKER_NETWORK` 环境变量。

`BOCKER_IMAGE_SERVER` 覆盖模板列表和容器创建使用的 SimpleStreams 镜像源，
默认是官方 `https://images.linuxcontainers.org/`；官方源不可达或较慢时，
可以设为国内镜像，例如 `BOCKER_IMAGE_SERVER=https://mirrors.tuna.tsinghua.edu.cn/lxc-images/`。
地址必须是 http/https。

`BOCKER_AUTO_APT_MIRROR=on` 在 `image build` 的每个阶段容器内检测 Debian/Ubuntu
官方 apt 源连通性，官方源不可达时自动把 `/etc/apt` 源切换到镜像站（默认清华 TUNA
`https://mirrors.tuna.tsinghua.edu.cn`，可用 `BOCKER_APT_MIRROR_URL` 覆盖）。
该开关默认关闭，保证默认构建内容可复现、供应链不被静默改变。

服务日志位于 `/var/lib/bocker/logs/`，单个日志超过 10MB 会自动轮转：最新
1MB 保留在 `.1` 备份文件，活动日志立即清空，不会无限增长。

## 5. 网络模式

| 模式 | 实现 | 适用场景 |
| --- | --- | --- |
| `nat` | Bocker 管理的 `bocker-nat` bridge，IPv4/IPv6 NAT | 默认推荐，容器访问外网但不直接暴露在局域网 |
| `bridge` | Incus macvlan，使用宿主机物理网卡 | 容器需要直接获得局域网地址 |

默认模式是 `bridge`，可用 `BOCKER_NETWORK=nat` 改为 NAT。Bridge 模式会
自动探测默认路由的物理网卡；探测失败时设置 `BOCKER_BRIDGE_PARENT`。

在 Wi-Fi 等不支持 macvlan 的网卡上，Bridge 模式自动回退到 Bocker 管理的
`bocker-br0`，默认使用 `10.0.200.0/24` 和 NAT，以保证容器仍可联网；可用
`BOCKER_BRIDGE_CIDR` 调整该网段。NAT 默认使用 `10.0.100.0/24`，并自动创建 IPv6 ULA 网络。可用
`BOCKER_NAT_CIDR` 和 `BOCKER_NAT_IPV6_CIDR` 调整，IPv6 设为 `none` 可关闭。
Bocker 对外只接受 `bridge` 和 `nat`，不接受底层 Incus 名称如 `macvlan`、
`bridged` 或 `ovn`。

## 6. 容器权限

权限按容器保存，默认是 `normal`：

```bash
bocker template install debian:12 --name debian-normal --permission normal
bocker template install debian:12 --name debian-super --permission super
bocker image run trusted-image --name trusted --permission super
```

`normal` 使用 Incus 非特权容器（`security.privileged=false`），保留默认的
AppArmor 和 capability 隔离。`super` 会启用特权容器、嵌套 LXC、移除容器 AppArmor
和 capability 限制，并放宽容器
内部 systemd 的隔离设置。它不会修改其他容器或宿主机的 systemd 配置，
但只应对可信软件使用。

## 7. Incusfile

`Incusfile` 是 Bocker 的构建描述文件。`bocker image build` 的上下文目录就是
`Incusfile` 所在目录，`COPY` 不能访问上下文之外的文件或符号链接。

### 指令

| 指令 | 说明 |
| --- | --- |
| `ARG KEY[=VALUE]` | 声明全局构建参数；必须位于首个 `FROM` 前，不写入最终镜像 |
| `FROM <image>[@<64位fingerprint>] [AS <stage>]` | 基础镜像并开始一个构建阶段；发布构建可用 fingerprint 固定基础镜像 |
| `NAME <name>` | 最终镜像别名和默认容器名 |
| `NETWORK bridge\|nat` | 构建和创建时的网络模式 |
| `WORKDIR <path>` | 设置后续 `RUN` 和相对 `COPY` 的工作目录 |
| `RUN <command>` | 在构建容器内通过 `/bin/sh -c` 执行 |
| `COPY <src> <dst>` | 从构建上下文复制文件 |
| `COPY --from=<stage> <src> <dst>` | 从前置构建阶段复制产物 |
| `ENV KEY=VALUE` | 设置构建阶段及最终运行容器的持久环境变量 |
| `EXPOSE <port>[/tcp\|udp]` | 创建运行时端口映射 |
| `DOMAIN <domain>` | 启动时更新宿主机 `/etc/hosts` |
| `AUTOSTART on\|off` | 设置容器开机自启动 |
| `ENTRYPOINT ["..."]` | 设置固定应用命令（仅 JSON exec form） |
| `CMD ["..."]` | 设置默认命令或参数（仅 JSON exec form） |
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
bocker image build --name hello-image Incusfile
bocker image run hello-image --name hello
bocker container exec hello cat /hello.txt
```

### ARG 与 ENV

`ARG` 只在构建期间存在，适合版本号、镜像地址和软件包名称；它必须集中声明在
第一个 `FROM` 前，并对普通阶段和 `TEMP` 阶段全局可见。`ENV` 必须位于 `FROM`
之后，既供后续 `RUN` 使用，也会写入最终镜像并传给正式容器。

```text
ARG BASE_IMAGE=debian/13
ARG PG_VERSION=18
ARG PGDG_MIRROR=https://mirrors.cloud.tencent.com/postgresql/repos/apt

FROM ${BASE_IMAGE}
RUN printf '%s\n' "deb ${PGDG_MIRROR} trixie-pgdg main" > /etc/apt/sources.list.d/pgdg.list
RUN apt-get update && apt-get install -y "postgresql-${PG_VERSION}"

ENV APP_ENV=production
RUN test "$APP_ENV" = production
```

在 `FROM`、`NAME`、`NETWORK`、`WORKDIR`、`COPY`、`ENV`、`EXPOSE`、
`DOMAIN`、`AUTOSTART`、`ENTRYPOINT` 和 `CMD` 中使用 `${NAME}`。`RUN` 由
shell 执行，可使用 `$NAME` 或 `${NAME}`。需要保留字面量 `${NAME}` 时，在非
`RUN` 指令中写成 `$${NAME}`。

构建时可覆盖一个或多个已声明参数：

```bash
bocker image build \
  --build-arg PG_VERSION=18 \
  --build-arg PGDG_MIRROR=https://apt.postgresql.org/pub/repos/apt \
  --permission super \
  Incusfile
```

未声明、重复或缺少 `KEY=VALUE` 的 `--build-arg` 会直接报错。ARG 不会隐式读取
宿主机同名环境变量，其声明和值也不会自动进入镜像属性或 `/etc/environment`；如果
主动在 `ENV`、`CMD`、`ENTRYPOINT` 或 `RUN` 生成的文件中引用 ARG，展开结果会按该
指令的正常语义写入镜像。构建命令和参数仍可能出现在日志中，因此不要使用 ARG
传递密码、Token 或私钥。

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

`ENTRYPOINT` 是固定命令，`CMD` 是追加参数。仅支持 JSON 数组，避免 shell-like
写法在服务中被当作字面参数执行；需要 shell 时请显式写为
`["/bin/sh", "-c", "command"]`。应用需要监听 `0.0.0.0`；需要 IPv6 时还应监听
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
基础镜像下隔离编译工具链，临时阶段不会进入最终镜像。`EXPOSE`、`DOMAIN`、
`AUTOSTART`、`ENTRYPOINT` 和 `CMD` 只允许出现在最终阶段；`EXPOSE` 使用同号
宿主机端口，因此同一宿主机上不能同时运行声明相同端口的两个实例。

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

- 报无法连接 Bocker 后台服务：确认 `bocker.service` 已启动；不要给每条命令加 `sudo`。
- 服务因 `setfattr` 退出：确认已安装 `attr`；root 首次运行会自动处理。
- Bridge 无法创建：设置正确的 `BOCKER_BRIDGE_PARENT`，或改用 `--network nat`。
- `image build` 找不到 `Incusfile`：检查文件路径和当前工作目录。
- 镜像列表获取失败：检查宿主机能否访问 `https://images.linuxcontainers.org/`，
  或设置 `BOCKER_IMAGE_SERVER` 指向国内 SimpleStreams 镜像
  （如 `https://mirrors.tuna.tsinghua.edu.cn/lxc-images/`）。
- Debian 13 等 systemd 257+ 发行版在部分宿主机/内核上，非特权（`normal`）容器内的
  systemd-networkd 会因上游 RuntimeDirectory 归属问题卡在启动状态，容器拿不到 IPv4
  （`journalctl` 里可见 `owned by 0:0 ... refusing`）。这是上游 systemd 在用户命名空间
  中的问题，不是 Bocker 缺陷；改用 `--permission super`，或使用 Debian 12 / Ubuntu LTS
  等 systemd 252~255 的模板即可。

## 9. 状态目录

默认状态目录是 `/var/lib/bocker`，可通过 `BOCKER_STATE_DIR` 修改：

```bash
BOCKER_STATE_DIR=/srv/bocker bocker container list
```

该目录包含容器、镜像、Unix socket、日志、运行时文件和守护进程状态。
Bocker 不连接系统 Incus 服务。
