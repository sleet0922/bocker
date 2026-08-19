# Bocker

Bocker 使用 Incus 提供容器和镜像运行时，并提供严格的 YAML 构建描述文件。它面向需要
在本机运行服务、固定基础镜像、隔离构建工具链的 Linux 项目。

## 快速开始

```yaml
version: 1
name: hello
network: nat
stages:
  - from: alpine/3.24
    steps:
      - exec:
          command: mkdir
          args: [-p, /opt/hello]
      - shell: |
          set -eu
          printf 'hello from bocker\n' > /opt/hello/message.txt
    runtime:
      entrypoint: [/bin/cat]
      cmd: [/opt/hello/message.txt]
      autostart: true
```

保存为 `Incusfile.yaml` 后执行：

```bash
bocker image build Incusfile.yaml
bocker image run hello --name hello
bocker container exec hello cat /opt/hello/message.txt
```

默认构建文件是当前目录的 `Incusfile.yaml`。旧的逐行 Incusfile 格式不再支持。

## 构建命令

```text
bocker image build [Incusfile.yaml]
bocker image build --name <name> [Incusfile.yaml]
bocker image build --network <bridge|nat> [Incusfile.yaml]
bocker image build --permission <normal|super> [Incusfile.yaml]
bocker image build --build-arg KEY=VALUE [Incusfile.yaml]
```

`--name` 和 `--network` 覆盖 YAML 中的全局配置；`--build-arg` 只能覆盖 YAML `args` 中
已经声明的变量。构建参数不会自动写入最终镜像。

## Incusfile.yaml

文件必须是一个 YAML 文档，并且顶层 `version` 必须为 `1`：

```yaml
version: 1
args:
  GO_VERSION: "1.26.6"
  APP_ENV: production
mirror: china
name: web-api
network: nat
stages: []
```

支持的顶层字段只有：`version`、`args`、`mirror`、`name`、`network`、`stages`。
未知字段、重复 key、YAML alias、多个文档和旧文本指令都会直接报错。

### 阶段

每个阶段必须包含 `from`，可选 `name` 和 `steps`。阶段按数组顺序构建；`copy.from` 只能
引用之前已经完成的阶段。

```yaml
stages:
  - name: builder
    from: alpine/3.24
    steps:
      - workdir: /src
      - copy:
          sources: [package.json, package-lock.json]
          destination: /src/
      - exec:
          command: npm
          args: [ci]
      - shell: |
          set -eu
          npm test
          npm run build
  - from: alpine/3.24
    steps:
      - copy:
          from: builder
          sources: [/src/dist]
          destination: /opt/web-api/
```

阶段步骤必须且只能包含以下一种字段：`exec`、`shell`、`pkg`、`workdir`、`copy`、`env`、
`mise`。

### exec 与 shell

`exec` 使用原始 argv 调用，不经过 shell，适合命令和参数可以明确拆分的场景：

```yaml
- exec:
    command: chmod
    args:
      - "0755"
      - /var/lib/livekit
      - /var/log/graduation-project
      - /opt/graduation-project/logs
      - /etc/graduation-project
```

需要管道、重定向、变量、条件或多条命令时必须明确使用 `shell`：

```yaml
- shell: |
    set -eu
    test -f /etc/app/config
    printf '%s\n' ready > /var/log/app/status
```

这样不会把 shell 解析、参数边界和普通 argv 命令混在一起。

### pkg、copy、env、workdir、mise

```yaml
steps:
  - pkg: [ca-certificates, curl]
  - workdir: /src
  - env:
      CGO_ENABLED: "0"
      APP_ENV: production
  - copy:
      sources: [go.mod, go.sum]
      destination: /src/
  - mise:
      tool: go
      version: "1.26.6"
```

`mise` 只能出现在非最终阶段；将构建工具和缓存留在 builder 阶段，再用 `copy.from` 复制产物。

`pkg` 根据基础系统使用 apt 或 apk，并清理索引。`copy` 的源路径必须位于 YAML 文件所在
上下文目录内，不能穿越路径或符号链接；多个源时目标必须是目录。`mise` 只接受精确版本，
工具、插件和缓存只存在于构建阶段。

### runtime

`runtime` 只能出现在最终阶段：

```yaml
runtime:
  env:
    APP_ENV: production
  entrypoint: [/usr/local/bin/server]
  cmd: [--port, "8080"]
  expose:
    - port: 8080
      protocol: tcp
  domain: web.test
  autostart: true
```

支持字段为 `env`、`entrypoint`、`cmd`、`expose`、`domain`、`autostart`。`entrypoint` 和
`cmd` 是 argv 数组；Bocker 会在 Debian/Ubuntu 的 systemd 或 Alpine 的 OpenRC 中安装原生
服务。`expose` 创建同号主机端口映射，协议为 `tcp` 或 `udp`。

### 参数与镜像固定

参数可在配置字符串字段中使用 `${NAME}`；`shell` 还会把声明的参数作为环境变量传入：

```yaml
version: 1
args:
  BASE_IMAGE: alpine/3.24
  PORT: "8080"
stages:
  - from: ${BASE_IMAGE}
    steps:
      - exec: {command: echo, args: ["${PORT}"]}
```

参数可以互相引用，循环引用和未声明引用会报错。发布构建可以固定基础镜像 fingerprint：

```yaml
from: images:alpine/3.24@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
```

## 国内软件源

`mirror: china` 或 `mirror: tuna` 使用清华 TUNA；也可以填写结构兼容的 HTTP/HTTPS 镜像站
根地址。它会应用到所有构建阶段。`pkg` 仍然要求显式列出软件包，不会猜测依赖。

## 测试项目

仓库包含多个 YAML 项目：

```text
testdata/yaml-projects/hello
testdata/yaml-projects/multi-stage
testdata/yaml-projects/runtime
testdata/mise/node
testdata/mise/python
testdata/mise/rust
testdata/packages/alpine
testdata/packages/ubuntu
```

运行代码检查：

```bash
make check
```

真实项目构建与断言需要本机 Bocker/Incus 服务运行：

```bash
make test-e2e
```

## 运行时配置

```bash
bocker image list
bocker image run <image> --name <container>
bocker container list
bocker container exec <container> <command> [args...]
bocker container stop <container>
bocker container remove <container>
```

`bocker --version`、`bocker help` 可查看版本和完整命令帮助。
