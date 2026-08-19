# Incusfile 编写说明

Bocker 的构建文件叫 `Incusfile`，没有文件扩展名，但文件内容是严格 YAML。
不要写成 `Incusfile.yaml`，也不要使用旧的逐行指令格式。

写好以后，在 `Incusfile` 所在目录执行下面的命令即可检查并构建：

```bash
bocker image build
```

## 先看一个最小例子

把下面内容完整保存为 `Incusfile`：

```yaml
version: 1
name: hello
network: nat

stages:
  - from: alpine/3.24
    steps:
      - exec:
          command: mkdir
          args: [-p, /app]
      - write:
          path: /app/message.txt
          mode: "0644"
          content: |
            hello from Incusfile
    runtime:
      entrypoint: [/bin/cat]
      cmd: [/app/message.txt]
      autostart: true
```

然后在同一目录执行：

```bash
bocker image build
```

## YAML 最基本的规则

### 1. 缩进只能用空格

推荐每层缩进两个空格。不要按 Tab 键。

```yaml
stages:
  - from: alpine/3.24
    steps:
      - exec:
          command: echo
          args: [hello]
```

每一层都必须对齐。`command` 比 `exec` 多缩进两格，`args` 与 `command` 对齐。

### 2. 冒号后面要有空格

正确：

```yaml
name: hello
```

错误：

```yaml
name:hello
```

### 3. `-` 表示列表中的一项

YAML 有两种最常见的数据：

- `name: hello` 是一个“字段和值”。
- `- hello` 是列表中的一项。

`stages`、`steps`、`pkg`、`copy.sources`、`exec.args` 使用列表。例如：

```yaml
exec:
  command: printf
  args:
    - hello
    - world
```

同一个 `exec` 也可以把列表写成一行：

```yaml
exec:
  command: printf
  args: [hello, world]
```

注意，顶层的构建变量 `args` 不是列表，而是“变量名: 值”的字段集合：

```yaml
args:
  APP_VERSION: "1.0.0"
  APP_ENV: production
```

`[]` 表示空列表，`{}` 表示空的字段集合。可选字段没有内容时，通常直接不写，比写空值更清楚。

### 4. 字符串中的特殊内容要加引号

版本号、权限、包含冒号或 `${...}` 的内容建议加双引号：

```yaml
GO_VERSION: "1.26.6"
mode: "0600"
value: "${APP_VERSION}"
```

`version: 1` 是文件格式版本，必须是数字 `1`；它不是 Go、Node 或应用程序版本。

### 5. 多行文字要写在 `|` 后面

`|` 表示下面缩进的所有行都属于同一段文字：

```yaml
- shell: |
    set -eu
    echo first
    echo second
```

`set -eu` 和两个 `echo` 必须对齐。如果其中一行少缩进，YAML 会在错误的位置结束这段文字。

### 6. 字段顺序和步骤顺序不是一回事

`version`、`name`、`network` 等字段前后调换不会改变含义。`stages` 和 `steps` 是列表，
列表顺序会改变构建结果：阶段按从上到下构建，步骤也按从上到下执行。

### 7. 每个步骤只能写一种类型

正确：

```yaml
- exec:
    command: chmod
    args: ["0755", /app]
```

错误：一个步骤同时出现 `exec` 和 `shell`：

```yaml
- exec: {command: echo, args: [hello]}
  shell: echo hello
```

Incusfile 使用严格校验。字段拼错、同名字段写两次、使用 YAML 锚点，都会直接报错，不会被静默忽略。

## 文件的整体结构

一个完整的 `Incusfile` 通常按这个顺序写：

```yaml
version: 1
args:
  APP_VERSION: "1.0.0"
mirror: china
name: my-app
network: nat

stages:
  - from: debian/13
    steps:
      - pkg: [ca-certificates]
    runtime:
      autostart: true
```

顶层字段说明：

| 字段 | 必填 | 写法 | 作用 |
| --- | --- | --- | --- |
| `version` | 是 | `1` | Incusfile 格式版本，必须为 `1` |
| `args` | 否 | `KEY: VALUE` | 构建时变量 |
| `mirror` | 否 | `china` 或 URL | apt/apk 软件包源 |
| `name` | 否 | 镜像名 | 构建出的镜像名 |
| `network` | 否 | `nat` 或 `bridge` | 构建容器的网络方式 |
| `stages` | 是 | 列表 | 一个或多个构建阶段 |

`stages` 至少要有一项。最后一个阶段是最终镜像，前面的阶段只是构建阶段。

## 软件源 `mirror`

最简单的写法是：

```yaml
mirror: china
```

`china` 和 `tuna` 含义相同，都会使用清华 TUNA。也可以填写兼容 Debian、Ubuntu 或 Alpine
目录结构的 HTTP/HTTPS 镜像站根地址：

```yaml
mirror: https://mirrors.tuna.tsinghua.edu.cn
```

这个设置会在每个阶段开始执行步骤之前修改 apt 或 apk 软件包源。它只负责系统软件包源，
不会改变 `from` 基础镜像的下载地址。

使用 `mise` 安装 Go、Node、Python 或 Rust 时，Bocker 还会自动配置对应的工具下载或依赖源。
这些语言源由 `mise` 步骤配置，不取决于是否写了 `mirror`，也可以在 `mise` 前面用 `env`
覆盖相应环境变量。

## 阶段怎么写

每个阶段至少需要 `from`：

```yaml
stages:
  - from: debian/13
    steps:
      - pkg: [curl]
```

阶段可用字段如下：

| 字段 | 必填 | 作用 |
| --- | --- | --- |
| `from` | 是 | 这个阶段使用的基础镜像 |
| `name` | 否 | 阶段名，供后面的 `copy.from` 引用 |
| `steps` | 否 | 按顺序执行的构建步骤 |
| `runtime` | 否 | 最终镜像的运行配置，只能出现在最后一个阶段 |

需要确保每次构建都使用完全相同的基础镜像时，可以在镜像名后添加 64 位 fingerprint：

```yaml
from: debian/13@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
```

不固定 fingerprint 时，`from: debian/13` 会使用该镜像名当前指向的版本。

给阶段起名字，后面的阶段就能用 `copy.from` 复制它的产物：

```yaml
stages:
  - name: builder
    from: alpine/3.24
    steps:
      - workdir: /src
      - copy:
          sources: [build.sh]
          destination: /src/
      - exec:
          command: chmod
          args: ["0755", /src/build.sh]
      - exec:
          command: /src/build.sh
          args: []

  - from: alpine/3.24
    steps:
      - copy:
          from: builder
          sources: [/src/output/app]
          destination: /usr/local/bin/app
```

`copy.from` 只能引用前面已经完成的阶段，不能引用后面的阶段。

## 常用步骤

### `workdir`：设置后续步骤的工作目录

```yaml
- workdir: /src
```

相对路径会相对于当前工作目录计算。构建开始时工作目录是 `/`。

### `copy`：复制文件

从 Incusfile 所在目录复制：

```yaml
- copy:
    sources: [go.mod, go.sum, cmd, internal]
    destination: /src/
```

多个 `sources` 时，`destination` 必须是目录，并以 `/` 结尾；当前工作目录本身也可以写成 `.`。

从前一个阶段复制：

```yaml
- copy:
    from: builder
    sources: [/out/app]
    destination: /usr/local/bin/app
```

只能复制构建上下文目录中的文件，不能用 `../` 越过该目录，符号链接也不能指向目录外。

### `exec`：执行一个命令

`exec` 不经过 shell，`command` 和 `args` 是严格分开的参数：

```yaml
- exec:
    command: chmod
    args: ["0755", /usr/local/bin/app]
```

没有参数时，`args` 可以省略，也可以写成 `args: []`。

有管道、重定向、`&&` 或条件判断时，用 `shell`，不要把 shell 语法塞进 `args`。

可以用 `capture` 把标准输出保存成后续构建变量：

```yaml
- exec:
    command: openssl
    args: [rand, -hex, "24"]
    capture: DB_PASSWORD

- write:
    path: /etc/app.env
    mode: "0600"
    content: |
      DB_PASSWORD=${DB_PASSWORD}
```

### `shell`：执行一整段 shell

```yaml
- shell: |
    set -eu
    mkdir -p /out
    go test ./...
    go build -o /out/app ./cmd/app
```

只有 `shell` 可以使用管道、重定向、变量赋值、`if`、`for` 等 shell 语法。

### `pkg`：安装系统软件包

```yaml
- pkg: [ca-certificates, curl, git]
```

Bocker 会根据基础系统选择 `apt-get` 或 `apk`，安装完成后清理 apt 索引。

### `env`：给后续构建步骤设置环境变量

```yaml
- env:
    CGO_ENABLED: "0"
    APP_ENV: production
```

它会立刻提供给当前阶段后面的步骤。只有写在最终阶段里的 `env` 才会保存到最终镜像，
builder 阶段的 `env` 不会跨阶段保留。只想声明可覆盖的构建参数时，使用顶层 `args`。

### `mise`：安装固定版本的构建工具

只能放在非最终阶段：

```yaml
- mise:
    tool: go
    version: "1.26.6"
```

支持的工具由 Bocker 的 mise 集成提供。版本必须写精确版本，不能写 `latest`。
Go、Node、Python、Rust 会自动配置对应的国内依赖源。要覆盖默认值，把 `env` 写在 `mise`
前面，因为步骤按从上到下执行。

### `write`：写入文件

```yaml
- write:
    path: /etc/app/app.env
    mode: "0600"
    content: |
      APP_ENV=production
      PORT=8080
```

`mode` 是四位八进制权限，常用值是 `0644`、`0600`、`0755`。省略时默认为 `0644`。
写入前要先用 `workdir`、`exec mkdir -p` 或其他步骤创建父目录。

### `download`：下载并校验文件

```yaml
- download:
    output: /tmp/app.tar.gz
    extract: /src
    attempts:
      - url: https://example.com/app-1.0.0.tar.gz
        sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
        format: tar.gz
      - url: https://backup.example.com/app-1.0.0.zip
        sha256: abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
        format: zip
        timeout: 60
        tries: 3
        move:
          from: /src/example-app-1.0.0
          to: /src/app-1.0.0
    verify:
      path: /src/app-1.0.0/VERSION
      pattern: "s/^version=//p"
      value: "1.0.0"
```

字段含义：

- `output`：压缩包的临时保存路径，必填；步骤结束时会删除该压缩包。
- `extract`：解压目录，必填；这个目录需要事先存在。
- `attempts`：下载源列表，至少一项；当前地址下载失败时，Bocker 会继续尝试下一项。
- `url`：下载地址，必填。
- `sha256`：压缩包的 64 位 SHA-256，必填；校验失败会停止构建。
- `format`：压缩格式，必填，只能写 `tar.gz`、`tgz` 或 `zip`。
- `timeout`：单次下载超时秒数，省略时为 `30`。
- `tries`：这个地址的尝试次数，省略时为 `1`。
- `move`：可选。某个备用压缩包解压后的目录名不同，就用 `from`、`to` 将它移动到统一路径。
- `verify`：可选。解压完成后，用 `sed -n` 和 `pattern` 从 `path` 读取内容，结果必须与 `value` 完全相同。

备用源的 `move` 只会在该备用源成功解压后执行。只有 `wget` 下载失败才会切换到下一项；
如果下载完成但 SHA-256、解压、`move` 或最终 `verify` 失败，构建会立即终止。

### `service`：在构建阶段操作服务

```yaml
- service:
    start: [postgresql.service]
    stop: [postgresql.service]
    enable: [postgresql.service, app.service]
```

每个动作的值都是服务名列表。服务步骤使用参数调用 `systemctl`，不是自由格式 shell。

## 构建变量 `args`

先声明变量：

```yaml
args:
  GO_VERSION: "1.26.6"
  APP_NAME: my-app
```

使用 `${变量名}`：

```yaml
stages:
  - from: debian/13
    steps:
      - mise:
          tool: go
          version: "${GO_VERSION}"
      - workdir: /src/${APP_NAME}
```

命令行可以覆盖已经声明的变量：

```bash
bocker image build --build-arg GO_VERSION=1.27.0
```

没有在 `args` 中声明的构建变量会报错。构建变量不会自动保存到最终镜像；需要运行时使用时，
请明确写入 `env` 或 `write`。

## 最终运行配置 `runtime`

`runtime` 只能写在最后一个阶段：

```yaml
runtime:
  env:
    APP_ENV: production
  entrypoint: [/usr/local/bin/app]
  cmd: [--config, /etc/app/config.yaml]
  expose:
    - port: 8080
      protocol: tcp
  domain: app.test
  autostart: true
```

字段含义：

- `env`：容器启动时的环境变量。
- `entrypoint`：程序及其固定参数，必须是数组。
- `cmd`：传给 entrypoint 的参数，必须是数组。
- `expose`：需要映射的端口，`protocol` 写 `tcp` 或 `udp`；省略协议时默认为 `tcp`。
- `domain`：Bocker 自动维护的宿主机域名。
- `autostart`：宿主机启动时是否自动启动容器。

## 最容易出错的地方

- 文件名必须是 `Incusfile`，内容才是 YAML；不要添加 `.yaml`。
- `version` 必须写 `1`。
- YAML 缩进用空格，不能用 Tab。
- `stages`、`steps`、`args` 后面的内容必须对齐。
- 一个步骤只能有一个类型字段。
- 顶层字段顺序无关，`stages` 和 `steps` 的列表顺序非常重要。
- `exec` 负责明确的 argv；管道和重定向写到 `shell`。
- `mise` 只能放在非最终阶段。
- `runtime` 只能放在最终阶段。
- 多阶段复制时，`copy.from` 只能指向前面的阶段。
- 旧的 `FROM`、`RUN`、`COPY` 逐行格式不支持。
