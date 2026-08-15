# Bocker 项目迭代提示词

你正在维护 Bocker 项目。请先完整阅读本文件和 `README.md`，再修改代码。
本文件描述的是当前已经确定的产品行为和工程约束，后续迭代不得无意改变这些约定。

## 产品目标

Bocker 是一个 Linux amd64 容器管理工具，提供独立 CLI 和 Ubuntu Flutter GUI。
它把容器管理需要的 Incus/LXC 运行时嵌入 Bocker，不依赖系统安装的 Incus CLI 或
Incus system service。

核心目标是“安装后直接可用”：普通本机用户执行公开的 `bocker` 命令时不需要在命令前
添加 `sudo`，不需要手动加入 `lxd` 组，也不需要手动运行初始化脚本。

本项目明确选择简单优先的本机权限模型：Bocker 的 API socket 和控制 socket 对所有
本机用户开放。任何本机用户都可以使用完整容器管理能力，这等价于授予本机用户较高的
容器/root 能力；这是产品的明确取舍，不要擅自改回 `lxd` 组、PolicyKit 或额外授权流程。

## 目录和模块

- `cmd/bocker/`：极薄的 Go 可执行入口。
- `internal/bocker/`：CLI 命令、Incus API 封装、嵌入式 daemon、网络、权限和测试。
- `gui/`：Flutter Linux GUI；GUI 使用同版本 bundle 内 CLI。
- `build/`：nfpm deb 配置和 deb 安装脚本。
- `completions/bocker`：Bash 补全及其测试。
- `README.md`：面向用户的安装、使用、卸载和架构文档。
- `PROJECT_PROMPT.md`：本迭代提示词。

## CLI 行为

所有公开命令都必须支持普通用户直接运行，包括：

- `template list/install`
- `image list/build/run/remove`
- `container list/shell/exec/start/stop/restart/remove/export/import`
- `container set` 的域名、端口、自启动和网络设置
- 所有对应的交互式菜单路径

需要宿主机 root 能力的操作通过 Bocker 后台 broker 转发。终端交互命令
（例如 `shell`、`exec`、`export`）仍须保留正常的标准输入输出行为。

所有公开资源命令统一从普通用户 CLI 进入 control socket，由 root 后台调用 Incus API。
当调用者位于真实终端时，broker 必须为后台子进程分配 PTY，并双向转发输入输出，以保留
上下键菜单、raw terminal、窗口尺寸和交互 shell；管道或 GUI 调用则使用普通 stdin/stdout
管道。普通用户 CLI 不应再绕过 control socket 直接执行某一类资源命令。

broker 转发必须是流式的：长时间运行的 `image build`、`template install`、容器操作等
要在执行过程中持续把 stdout/stderr 转发给调用者，不能等整个子进程结束后才一次性返回，
否则用户会误以为命令没有反应。

内部命令 `bocker __daemon` 是 systemd 使用的私有入口，只允许 root 运行；普通用户不应
直接调用它。不要把这个内部约束误认为公开 CLI 需要 root。

普通用户运行 CLI 的前提只有：

1. Bocker deb 已安装。
2. `bocker.service` 正常运行。

普通用户不需要 `sudo`，也不需要 `lxd` 组。

## 后台服务和 socket

后台服务名称固定为 `bocker.service`，systemd unit 由 root 初始化时写入：

```text
/etc/systemd/system/bocker.service
```

服务执行 `/var/lib/bocker/bin/bocker-daemon __daemon`，状态目录由
`BOCKER_STATE_DIR=/var/lib/bocker` 指定。

运行时使用两个 Unix socket：

```text
/var/lib/bocker/incus/unix.socket
/var/lib/bocker/incus/bocker-control.socket
```

为了实现“所有本机用户安装后直接可用”：

- `/var/lib/bocker` 和 `/var/lib/bocker/incus` 至少要可遍历（当前为 `0711`）。
- 两个 socket 当前为 `0666`。
- 不要重新引入 `lxd` 组查找、socket group chown、PolicyKit 或 sudo broker。
- daemon 重启后必须再次确保 socket 权限正确，因为 socket 文件会被重新创建。

`/var/lib/bocker` 内的容器、镜像、密钥、日志和运行时文件仍由 daemon 管理；不要在
普通 CLI 中改变 `BOCKER_STATE_DIR`，也不要让 broker 请求覆盖该路径。

## Debian 打包布局

CLI deb 必须安装：

```text
/usr/bin/bocker
/usr/share/bash-completion/completions/bocker
```

GUI deb 必须安装：

```text
/usr/lib/bocker-gui/
/usr/share/applications/io.bocker.bocker_gui.desktop
/usr/share/pixmaps/io.bocker.bocker_gui.png
```

不要把 deb payload 放到 `/opt`，不要创建 `/usr/local/bin` 或其他额外软链接，
不要修改用户 `.bashrc`。

GUI deb 内的 `/usr/lib/bocker-gui/install_desktop.sh` 仅用于源码 bundle 的桌面用户
安装，不是 deb 安装步骤，也不应通过 `sudo` 执行。

## deb 安装初始化

`build/postinstall.sh` 会被 nfpm 嵌入两个 deb，不是用户需要另外下载的文件。
安装完成后脚本以 root 调用 `bocker container list --json`，从而自动：

- 解压 Bocker 私有运行时。
- 写入或更新 `bocker.service`。
- 执行 `systemctl enable --now bocker.service`。
- 初始化默认存储和 socket。

脚本必须幂等，失败不能破坏 deb 安装流程；已运行的服务再次执行应能安全升级和重启。
CLI deb 优先使用 `/usr/bin/bocker`，GUI-only 安装时可回退到
`/usr/lib/bocker-gui/bocker`。

## 运行时产生的路径

这些不是 deb 的静态 payload，但首次初始化或运行容器时可能出现：

```text
/var/lib/bocker/
/var/lib/incus-lxcfs
/opt/incus/lib/lxc/rootfs
```

还可能创建 Bocker 专用网络设备 `bocker-br0`、`bocker-nat`、路由，以及在
`/etc/hosts` 中写入带 `# bocker:<container>` 标记的域名行。

不要删除整个 `/opt/incus` 或其他 Incus 数据目录；只处理 Bocker 明确创建的路径。

## 卸载要求

文档必须同时说明“保留容器数据”和“完整清理”两种方式。

仅卸载程序并保留数据：

```bash
sudo systemctl disable --now bocker.service 2>/dev/null || true
sudo apt purge bocker bocker-gui
sudo systemctl daemon-reload
```

完整清理前必须确认不再需要容器、镜像和运行时，然后删除 Bocker 生成的 unit、程序、
GUI、`/var/lib/bocker`、`/var/lib/incus-lxcfs` 和 `/opt/incus/lib/lxc/rootfs`。
如果配置过域名，先备份 `/etc/hosts`，再删除带 `# bocker:` 的行；网络设备只在确认
属于 Bocker 时删除。

保持 `README.md` 的卸载章节与真实路径同步。不要把删除整个 `/opt/incus`、系统其他
Incus 数据或无关网络设备写进卸载命令。

## 开发、测试和验证

每次 Go 或权限相关修改后至少运行：

```bash
make check
git diff --check
```

打包前运行：

```bash
make build-cli-deb
make build-gui-deb
dpkg-deb -f bocker.deb Package Version Architecture
dpkg-deb -f bocker-gui.deb Package Version Architecture
```

安装测试需要管理员权限，但 CLI 测试本身不得在命令前加 `sudo`：

```bash
sudo dpkg -i ./bocker.deb ./bocker-gui.deb
bocker --version
bocker container list --json
bocker image list --json
bocker template list --json
```

还要验证：

- `systemctl is-enabled bocker.service` 为 enabled。
- `systemctl is-active bocker.service` 为 active。
- 两个 socket 为 `0666`，父目录可遍历。
- 普通用户（可用不属于 `lxd` 组的测试账号）能访问 API socket 和 broker。
- GUI `/usr/lib/bocker-gui/bocker_gui` 能以普通用户启动。
- 交互式菜单中的 privileged 操作没有绕过 broker。

不要为了测试删除用户现有容器或镜像；需要破坏性测试时使用唯一的临时名称并清理。

## 版本和发布

版本号必须同步更新：

- `Makefile` 的 `VERSION`。
- `internal/bocker/main.go` 的 `Version`。
- `gui/pubspec.yaml` 的 Flutter 版本（build number 按现有约定）。

发布前确认 `make check`、两个 deb 安装测试和 `git status` 干净，然后：

```bash
git add -A
git commit -m "..."
git push origin main
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
gh release create vX.Y.Z bocker.deb bocker-gui.deb \
  --title "Bocker vX.Y.Z" \
  --notes "..."
```

发布资产必须是刚刚构建并验证过的 `bocker.deb` 和 `bocker-gui.deb`。

## 迭代纪律

- 先读现有代码和测试，再修改；优先复用已有 Incus、broker、网络和打包逻辑。
- 保持改动范围明确，不做无关重构或格式化。
- 使用 `apply_patch` 编辑文件；不要用脚本覆盖整个文件。
- 不要回滚用户已有的无关改动，不要使用 `git reset --hard` 或 `git checkout --`。
- 任何权限、socket、systemd、卸载和打包行为的改变都必须同步更新测试和 `README.md`。
- 不要把“普通用户”实现成每条命令要求 sudo；公开 CLI 的体验必须保持安装后直接可用。
- 完成修改后继续执行测试、打包、安装验证和必要的 GitHub 发布，不要停在只给方案的阶段。
