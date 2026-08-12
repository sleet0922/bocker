package bocker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// CmdBuild 从 Incusfile 构建镜像。
// 构建完成后用 'bocker image run' 启动容器。
//
// 用法:
//
//	bocker build [Incusfile]              构建镜像 (默认 ./Incusfile)
//	bocker build --name <name> [Incusfile] 覆盖镜像别名
//	bocker build --help                   显示帮助
func CmdBuild(args []string) error {
	networkOverride := hasNetworkOverride(args)
	networkMode, args, err := networkModeFromArgs(args)
	if err != nil {
		return err
	}
	overrideName := ""
	incusfilePath := ""

	i := 0
	for i < len(args) {
		arg := args[i]
		switch arg {
		case "--name":
			if i+1 >= len(args) {
				return fmt.Errorf("--name 需要参数")
			}
			overrideName = args[i+1]
			i += 2
		case "--help", "-h":
			fmt.Print(buildUsage())
			return nil
		default:
			if strings.HasPrefix(arg, "--") {
				return fmt.Errorf("未知参数: %s (使用 --help 查看用法)", arg)
			}
			if incusfilePath != "" {
				return fmt.Errorf("只能指定一个 Incusfile 路径")
			}
			incusfilePath = arg
			i++
		}
	}

	f, err := parseIncusfile(incusfilePath)
	if err != nil {
		return err
	}
	if f.Network != "" && !networkOverride {
		networkMode, err = ParseNetworkMode(f.Network)
		if err != nil {
			return err
		}
	}
	f.Network = string(networkMode)
	if overrideName != "" {
		f.Name = overrideName
	}

	alias := f.Name
	if alias == "" {
		alias = defaultNameFromImage(f.From) + "-built"
	}
	if err := validateBockerName(alias); err != nil {
		return fmt.Errorf("目标镜像名称 %q 无效: %w", alias, err)
	}

	client := NewIncusClient()

	runCount, copyCount, envCount := 0, 0, 0
	for _, stage := range f.Stages {
		for _, step := range stage.Steps {
			switch step.Kind {
			case "RUN":
				runCount++
			case "COPY":
				copyCount++
			case "ENV":
				envCount++
			}
		}
	}

	fmt.Printf("╭─ Incusfile 构建\n")
	fmt.Printf("│ 文件:     %s\n", f.Path)
	fmt.Printf("│ 基础镜像: %s\n", f.From)
	fmt.Printf("│ 目标镜像: %s\n", alias)
	fmt.Printf("│ 网络模式: %s\n", networkMode)
	fmt.Printf("│ RUN: %d  COPY: %d  ENV: %d  EXPOSE: %d  步骤: %d\n", runCount, copyCount, envCount, len(f.Exposes), len(f.Steps))
	if f.Domain != "" {
		fmt.Printf("│ DOMAIN:   %s\n", f.Domain)
	}
	if f.Autostart != nil {
		fmt.Printf("│ AUTOSTART: %s\n", strconv.FormatBool(*f.Autostart))
	}
	if len(f.Entrypoint) > 0 {
		fmt.Printf("│ ENTRYPOINT: %s\n", strings.Join(f.Entrypoint, " "))
	}
	if len(f.Cmd) > 0 {
		fmt.Printf("│ CMD:        %s\n", strings.Join(f.Cmd, " "))
	}
	fmt.Printf("╰─\n\n")

	if err := buildImage(client, f, alias, networkMode); err != nil {
		return err
	}

	fmt.Printf("\n✔ 镜像 %s 构建完成\n", alias)
	fmt.Printf("  使用 'bocker image run %s --name <容器名>' 启动容器\n", alias)
	return nil
}

func buildUsage() string {
	return `bocker image build - 从 Incusfile 构建镜像 (支持多阶段构建)

用法:
  bocker image build [Incusfile]                         构建镜像 (默认 ./Incusfile)
  bocker image build --name <name> [Incusfile]           覆盖镜像别名
  bocker image build --network <bridge|nat> [Incusfile]  覆盖构建网络

构建完成后用 'bocker image run <image> --name <name>' 启动容器。

Incusfile 指令:
  FROM <image> [AS <name>]   基础镜像，开始新构建阶段 (多阶段)
  NAME <name>                镜像别名 + 容器名 (全局)
  NETWORK bridge|nat         网络模式 (bridge=Incus macvlan, nat=Incus bridge)
  WORKDIR <path>             设置后续 RUN/COPY 的工作目录
  RUN <command>              在容器内执行 shell 命令
  COPY [--from=<stage>] <src> <dst>  从宿主机或指定阶段复制
  ENV <KEY>=<VALUE>          设置环境变量
  EXPOSE <port>[/<proto>]    声明端口映射
  DOMAIN <domain>            域名映射
  AUTOSTART on|off           开机自启动
  TEMP <name> ... END        临时构建块 (隔离编译工具链, 不进最终镜像)

TEMP 块示例 (单 FROM all-in-one, 编译产物隔离):
  FROM debian/13
  NAME my-app
  RUN apt-get update && apt-get install -y ca-certificates mysql-server

  TEMP builder
    RUN apt-get update && apt-get install -y golang-go
    WORKDIR /src
    COPY ./main.go .
    RUN go build -o app .
  END

  COPY --from=builder /src/app /usr/local/bin/app
  EXPOSE 8080/tcp
  AUTOSTART on

多阶段构建示例 (分离构建环境与运行时):
  FROM debian/13 AS builder
  WORKDIR /src
  RUN apt-get update && apt-get install -y golang-go
  COPY ./main.go .
  RUN go build -o app .

  FROM debian/13
  RUN apt-get update && apt-get install -y ca-certificates
  COPY --from=builder /src/app /usr/local/bin/app
  EXPOSE 8080/tcp
  DOMAIN myapp.test
  AUTOSTART on

单阶段示例:
  FROM debian/12
  NAME my-nginx
  RUN apt-get update && apt-get install -y nginx
  COPY ./index.html /var/www/html/index.html
  EXPOSE 80/tcp
  AUTOSTART on
`
}

type templateListItem struct {
	Distro  string `json:"distro"`
	Release string `json:"release"`
	Image   string `json:"image"`
}

// CmdTemplateList 列出远程镜像源中可以安装的模板。
func CmdTemplateList(args []string) error {
	jsonOutput, err := parseJSONOutputOption(args)
	if err != nil {
		return fmt.Errorf("template list: %w", err)
	}
	if !jsonOutput {
		fmt.Println("正在从镜像源获取可安装模板 ...")
	}
	client := NewIncusClient()
	groups, err := client.ListImages()
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return fmt.Errorf("未找到可安装模板")
	}
	if jsonOutput {
		items := make([]templateListItem, 0)
		for _, group := range groups {
			for _, version := range group.Versions {
				items = append(items, templateListItem{
					Distro: group.Distro, Release: version.Release, Image: version.Image,
				})
			}
		}
		return json.NewEncoder(os.Stdout).Encode(items)
	}

	arch := archName()
	total := 0
	for _, g := range groups {
		total += len(g.Versions)
	}
	fmt.Printf("╭─ 可安装模板 (架构: %s, 共 %d 个发行版 %d 个版本)\n", arch, len(groups), total)
	fmt.Println("│ 模板名可用于 template install，也可写入 Incusfile 的 FROM")
	fmt.Println("│")
	for _, g := range groups {
		fmt.Printf("│ %s\n", g.Distro)
		for _, v := range g.Versions {
			fmt.Printf("│   FROM %s   # %s\n", v.Image, v.Release)
		}
	}
	fmt.Println("│")
	fmt.Println("│ 安装示例: bocker template install debian/12 --name demo")
	fmt.Println("╰─")
	return nil
}

// autoConfigureAptMirror 检测容器内 apt 官方源连通性，失败则自动换为清华镜像源。
// 仅对 Debian/Ubuntu 系容器生效；其他发行版 (Alpine/CentOS 等) 跳过。
// 这样用户无需在 Incusfile 里手动加 sed 换源命令。
func autoConfigureAptMirror(client *IncusClient, name string) error {
	// 检测是否为 Debian/Ubuntu
	osRelease, err := client.ReadFile(name, "/etc/os-release")
	if err != nil {
		return nil // 读不到就跳过，不阻塞构建
	}
	if !strings.Contains(osRelease, "ID=debian") && !strings.Contains(osRelease, "ID=ubuntu") {
		return nil // 非 Debian/Ubuntu，跳过
	}

	// 测试官方源连通性 (8 秒超时，避免阻塞构建)
	// 优先用 curl；没有 curl 则用 wget；都没有则不换源 (无法确认是否为网络问题)
	officialHost := "http://deb.debian.org/"
	if strings.Contains(osRelease, "ID=ubuntu") {
		officialHost = "http://archive.ubuntu.com/"
	}

	// 检测工具可用性：必须有 curl 或 wget 之一才能确认网络问题
	_, hasToolErr := client.execQuiet(name, "sh", "-c", "command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1")
	if hasToolErr != nil {
		// curl 和 wget 都没装，无法确认网络问题，跳过自动换源
		// (用户可在 Incusfile 里 RUN apt-get update 看实际报错)
		return nil
	}

	// 用任意一种工具测试官方源连通性
	testCmd := fmt.Sprintf(`command -v curl >/dev/null 2>&1 && curl -sI --max-time 8 -o /dev/null -w '%%{http_code}' %s 2>/dev/null | grep -qE '^[23][0-9][0-9]$' ||
command -v wget >/dev/null 2>&1 && wget -q --timeout=8 --spider %s 2>/dev/null ||
exit 1`, officialHost, officialHost)
	_, testErr := client.execQuiet(name, "sh", "-c", testCmd)
	if testErr == nil {
		return nil // 官方源可访问，不换源
	}

	// 已确认官方源不可达，自动换为清华镜像源
	fmt.Printf("  ⚠ 检测到 apt 官方源不可达，自动换为清华镜像源\n")

	// Debian 12+ 使用 deb822 格式 (/etc/apt/sources.list.d/debian.sources)
	// Debian 11 及更早使用传统格式 (/etc/apt/sources.list)
	// Ubuntu 使用 /etc/apt/sources.list
	// 用 sh 一次性处理所有可能的源文件格式
	sedScript := `set -e
# Debian deb822 格式 (12+)
if [ -f /etc/apt/sources.list.d/debian.sources ]; then
	sed -i 's|http://deb.debian.org|https://mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources
	sed -i 's|http://security.debian.org|https://mirrors.tuna.tsinghua.edu.cn/debian-security|g' /etc/apt/sources.list.d/debian.sources
fi
# Debian 传统格式 (<=11) 与 Ubuntu
if [ -f /etc/apt/sources.list ]; then
	sed -i 's|http://deb.debian.org|https://mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list
	sed -i 's|http://security.debian.org|https://mirrors.tuna.tsinghua.edu.cn/debian-security|g' /etc/apt/sources.list
	sed -i 's|http://archive.ubuntu.com|https://mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list
	sed -i 's|http://security.ubuntu.com|https://mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list
fi
echo "✔ 镜像源已切换为清华源"`
	if err := client.ExecStreaming(name, sedScript, nil); err != nil {
		return fmt.Errorf("切换镜像源失败: %w", err)
	}
	return nil
}

// buildImage 执行多阶段构建流程：按顺序构建各阶段，最终阶段发布为镜像。
// 中间阶段的容器保持运行 (供后续阶段 COPY --from 引用)，最终统一清理。
// 单阶段 Incusfile (只有一个 FROM) 走相同的代码路径，行为与旧版一致。
func buildImage(client *IncusClient, f *Incusfile, alias string, networkMode NetworkMode) error {
	stages := f.Stages
	contextDir := filepath.Dir(f.Path)
	stageContainers := make([]string, len(stages))
	// 确保清理所有阶段容器 (含中间阶段和最终阶段)
	defer func() {
		for i := len(stageContainers) - 1; i >= 0; i-- {
			name := stageContainers[i]
			if name == "" {
				continue
			}
			fmt.Printf("▶ 清理阶段容器 %s ...\n", name)
			if err := client.Stop(name); err != nil {
				_ = client.StopForce(name)
			}
			if err := client.Delete(name); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ 删除阶段容器 %s 失败: %v\n", name, err)
			}
		}
	}()

	totalStages := len(stages)
	for si, stage := range stages {
		stageContainer := fmt.Sprintf("bocker-build-%d-%d-s%d", os.Getpid(), time.Now().UnixNano(), si)
		stageContainers[si] = stageContainer
		stageLabel := stage.Name
		if stageLabel == "" {
			stageLabel = fmt.Sprintf("stage %d", si+1)
		}

		isLast := si == totalStages-1
		role := "中间阶段"
		if isLast {
			role = "最终阶段"
		}
		fmt.Printf("\n▶ [%d/%d] %s %q (镜像 %s) ...\n", si+1, totalStages, role, stageLabel, stage.From)

		// 启动阶段容器
		if err := client.LaunchWithNetworkAndPermissionAndFingerprint(stage.From, stage.BaseFingerprint, stageContainer, networkMode, PermissionNormal); err != nil {
			return fmt.Errorf("启动阶段 %d 容器失败: %w", si+1, err)
		}
		actualBase, err := client.BaseImageFingerprint(stageContainer)
		if err != nil {
			return fmt.Errorf("读取阶段 %d 基础镜像 fingerprint 失败: %w", si+1, err)
		}
		if actualBase == "" {
			return fmt.Errorf("阶段 %d 未返回基础镜像 fingerprint", si+1)
		}
		f.Stages[si].BaseFingerprint = actualBase
		stage.BaseFingerprint = actualBase
		fmt.Printf("  阶段 %d 基础 fingerprint: %s\n", si+1, actualBase)

		// 等待网络就绪
		ip := waitForIP(client, stageContainer, 30)
		if ip == "" {
			fmt.Printf("⚠ 阶段 %d 容器未获取 IPv4，RUN 命令可能因网络问题失败\n", si+1)
		} else {
			fmt.Printf("  阶段 %d IPv4: %s\n", si+1, ip)
		}

		// 执行步骤
		workdir := "/"
		runEnv := map[string]string{}
		var collectedEnvs []EnvSpec
		total := len(stage.Steps)
		for i, step := range stage.Steps {
			switch step.Kind {
			case "WORKDIR":
				if path.IsAbs(step.Workdir) {
					workdir = path.Clean(step.Workdir)
				} else {
					workdir = path.Clean(path.Join(workdir, step.Workdir))
				}
				fmt.Printf("  [阶段%d %d/%d] WORKDIR %s\n", si+1, i+1, total, workdir)
				if err := client.ExecStreaming(stageContainer, "mkdir -p "+shellQuote(workdir), nil); err != nil {
					return fmt.Errorf("WORKDIR 创建目录失败: %w", err)
				}
			case "ENV":
				fmt.Printf("  [阶段%d %d/%d] ENV %s=%s\n", si+1, i+1, total, step.Env.Key, step.Env.Value)
				runEnv[step.Env.Key] = step.Env.Value
				collectedEnvs = append(collectedEnvs, step.Env)
			case "COPY":
				fromLabel := "宿主机"
				if step.Copy.From != "" {
					fromLabel = "阶段 " + step.Copy.From
				}
				fmt.Printf("  [阶段%d %d/%d] COPY (from %s) %s -> %s\n", si+1, i+1, total, fromLabel, step.Copy.Src, step.Copy.Dst)
				dst := resolveContainerPath(workdir, step.Copy.Dst)
				if step.Copy.From != "" {
					// 跨容器复制: --from=<stage_name 或 数字索引>
					srcStageIdx, err := resolvePriorStage(stages, si, step.Copy.From)
					if err != nil {
						return err
					}
					srcContainer := stageContainers[srcStageIdx]
					if err := client.CopyBetweenContainers(srcContainer, step.Copy.Src, stageContainer, dst); err != nil {
						return fmt.Errorf("COPY --from=%s 失败: %w", step.Copy.From, err)
					}
				} else {
					// 从宿主机复制
					if err := applyCopyDst(client, stageContainer, step.Copy, contextDir, dst); err != nil {
						return err
					}
				}
			case "RUN":
				cmd := step.Run
				if workdir != "/" && workdir != "" {
					cmd = "cd " + shellQuote(workdir) + " && " + cmd
				}
				fmt.Printf("  [阶段%d %d/%d] RUN: %s\n", si+1, i+1, total, step.Run)
				if err := client.ExecStreaming(stageContainer, cmd, runEnv); err != nil {
					return fmt.Errorf("阶段 %d RUN 失败: %s\n  %w", si+1, step.Run, err)
				}
			}
		}

		// 持久化 ENV 到容器文件系统 (供镜像运行时使用)
		if len(collectedEnvs) > 0 {
			if err := applyEnvs(client, stageContainer, collectedEnvs); err != nil {
				return err
			}
		}
		if isLast && hasRuntimeCommand(f) {
			if err := installRuntimeService(client, stageContainer, f); err != nil {
				return err
			}
		}

		// 最终阶段：停止并发布镜像
		if isLast {
			fmt.Printf("\n▶ [最终阶段] 停止并发布镜像 %s ...\n", alias)
			if err := client.Stop(stageContainer); err != nil {
				_ = client.StopForce(stageContainer)
			}
			properties := buildImageProperties(f)
			if err := client.PublishImage(stageContainer, alias, properties); err != nil {
				return fmt.Errorf("发布镜像失败: %w", err)
			}
			fmt.Printf("✔ 镜像已发布: %s\n", alias)
		} else {
			fmt.Printf("  ✔ 阶段 %q 完成 (保持运行供后续阶段引用)\n", stageLabel)
		}
	}
	return nil
}

func resolveContainerPath(workdir, destination string) string {
	explicitDirectory := strings.HasSuffix(destination, "/")
	resolved := destination
	if path.IsAbs(destination) {
		resolved = path.Clean(destination)
	} else {
		resolved = path.Join(workdir, destination)
	}
	if explicitDirectory && resolved != "/" {
		resolved += "/"
	}
	return resolved
}

// stageNames 返回可用阶段名列表 (用于错误提示)
func stageNames(stages []Stage, before int) []string {
	var names []string
	for i := 0; i < before && i < len(stages); i++ {
		if stages[i].Name != "" {
			names = append(names, stages[i].Name)
		} else {
			names = append(names, fmt.Sprintf("%d", i))
		}
	}
	return names
}

func resolvePriorStage(stages []Stage, current int, reference string) (int, error) {
	if index, err := strconv.Atoi(reference); err == nil {
		if index < 0 || index >= current {
			return 0, fmt.Errorf("COPY --from=%s: 阶段索引必须指向当前阶段之前的阶段", reference)
		}
		return index, nil
	}
	for i := 0; i < len(stages); i++ {
		if !strings.EqualFold(stages[i].Name, reference) {
			continue
		}
		if i >= current {
			return 0, fmt.Errorf("COPY --from=%s: 只能引用当前阶段之前已完成的阶段", reference)
		}
		return i, nil
	}
	return 0, fmt.Errorf("COPY --from=%s: 找不到该阶段 (可用: %v)", reference, stageNames(stages, current))
}

// buildImageProperties 将 Incusfile 的运行时指令编码为镜像属性，供 bocker image run 读取。
func buildImageProperties(f *Incusfile) map[string]string {
	p := map[string]string{}
	if f.Name != "" {
		p["user.bocker.name"] = f.Name
	}
	if len(f.Exposes) > 0 {
		p["user.bocker.expose"] = exposeString(f.Exposes)
	}
	if f.Domain != "" {
		p["user.bocker.domain"] = f.Domain
	}
	if f.Autostart != nil {
		p["user.bocker.autostart"] = strconv.FormatBool(*f.Autostart)
	}
	if f.Network != "" {
		p[containerNetworkConfig] = f.Network
	}
	if f.From != "" {
		p["user.bocker.base_image"] = f.From
	}
	if len(f.Stages) > 0 && f.Stages[len(f.Stages)-1].BaseFingerprint != "" {
		p["user.bocker.base_fingerprint"] = f.Stages[len(f.Stages)-1].BaseFingerprint
	}
	if data, err := json.Marshal(dedupeEnvSpecs(f.Env)); err == nil && len(f.Env) > 0 {
		p["user.bocker.env"] = string(data)
	}
	if data, err := json.Marshal(f.Entrypoint); err == nil && len(f.Entrypoint) > 0 {
		p["user.bocker.entrypoint"] = string(data)
	}
	if data, err := json.Marshal(f.Cmd); err == nil && len(f.Cmd) > 0 {
		p["user.bocker.cmd"] = string(data)
	}
	return p
}

const runtimeEntrypointPath = "/usr/local/lib/bocker-entrypoint"

func hasRuntimeCommand(f *Incusfile) bool {
	return len(f.Entrypoint) > 0 || len(f.Cmd) > 0
}

func runtimeCommand(f *Incusfile) []string {
	if len(f.Entrypoint) == 0 {
		return append([]string(nil), f.Cmd...)
	}
	command := append([]string(nil), f.Entrypoint...)
	return append(command, f.Cmd...)
}

// installRuntimeInit adds an application service without replacing the image
// init process. This keeps normal systemd/OpenRC networking (including DHCP)
// intact while making CMD/ENTRYPOINT start automatically.
func installRuntimeService(client *IncusClient, name string, f *Incusfile) error {
	command := runtimeCommand(f)
	if len(command) == 0 {
		return nil
	}
	var script strings.Builder
	script.WriteString("#!/bin/sh\n")
	script.WriteString("[ -f /etc/bocker.env ] && set -a && . /etc/bocker.env && set +a\n")
	script.WriteString("exec")
	for _, arg := range command {
		script.WriteByte(' ')
		script.WriteString(shellQuote(arg))
	}
	script.WriteByte('\n')
	if err := client.ExecStreaming(name, "mkdir -p /usr/local/lib /etc/systemd/system /etc/init.d", nil); err != nil {
		return fmt.Errorf("create runtime service directories: %w", err)
	}
	if err := client.PushFile(name, runtimeEntrypointPath, []byte(script.String()), "0755"); err != nil {
		return fmt.Errorf("write runtime entrypoint: %w", err)
	}

	// The image keeps its own init as PID 1. Use its native service manager to
	// start the declared application once boot and networking are available.
	serviceCmd := `if test -d /run/systemd/system && command -v systemctl >/dev/null 2>&1; then
  cat > /etc/systemd/system/bocker-entrypoint.service <<'EOF'
[Unit]
Description=Bocker application entrypoint
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-/etc/bocker.env
ExecStart=/usr/local/lib/bocker-entrypoint
Restart=on-failure
RestartSec=5
StartLimitIntervalSec=60
StartLimitBurst=5

[Install]
WantedBy=multi-user.target
EOF
  systemctl enable bocker-entrypoint.service
elif test -x /sbin/openrc-run; then
  cat > /etc/init.d/bocker-entrypoint <<'EOF'
#!/sbin/openrc-run
name="bocker-entrypoint"
command="/usr/local/lib/bocker-entrypoint"
command_background="yes"
pidfile="/run/bocker-entrypoint.pid"

depend() {
  need net
}
EOF
  chmod 0755 /etc/init.d/bocker-entrypoint
  rc-update add bocker-entrypoint default
else
  echo 'Bocker CMD/ENTRYPOINT requires systemd or OpenRC in the base image' >&2
  exit 1
fi`
	if err := client.ExecStreaming(name, serviceCmd, nil); err != nil {
		return fmt.Errorf("enable CMD/ENTRYPOINT service (base image must use systemd or OpenRC): %w", err)
	}
	return nil
}

// runFromBuiltImage 从已构建的镜像启动正式容器，并应用 EXPOSE/DOMAIN/AUTOSTART。
func runFromBuiltImage(client *IncusClient, alias string, f *Incusfile, networkMode NetworkMode, permission PermissionMode) error {
	name := f.Name
	if name == "" {
		name = defaultNameFromImage(alias)
	}
	if err := validateBockerName(name); err != nil {
		return fmt.Errorf("容器名称 %q 无效: %w", name, err)
	}

	// 检查同名容器是否已存在
	if existing, err := client.GetContainer(name); err == nil && existing != nil {
		return fmt.Errorf("容器 %s 已存在，请先删除或使用 --name 指定其他名称", name)
	} else if err != nil && !isInstanceNotFound(err) {
		return fmt.Errorf("检查容器 %s 是否存在失败: %w", name, err)
	}
	if err := validateRuntimePortMappings(f.Exposes, nil); err != nil {
		return err
	}
	containers, err := client.ListContainers()
	if err != nil {
		return fmt.Errorf("检查现有端口映射失败: %w", err)
	}
	if err := validateRuntimePortMappings(f.Exposes, containers); err != nil {
		return err
	}

	fmt.Printf("▶ 启动容器 %s (镜像 %s) ...\n", name, alias)
	if err := client.LaunchLocalImageWithNetworkPermissionAndConfig(alias, name, networkMode, permission, incusEnvironmentConfig(f.Env)); err != nil {
		return fmt.Errorf("启动容器失败: %w", err)
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		_ = removeHostsLine(name)
		if err := client.Stop(name); err != nil {
			_ = client.StopForce(name)
		}
		if err := client.Delete(name); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ 回滚容器 %s 失败: %v\n", name, err)
		}
	}()

	ip := waitForIP(client, name, 30)
	if ip != "" {
		if err := AutoConfigureHostBridge(client); err != nil {
			return fmt.Errorf("配置宿主机 bridge 互通失败: %w", err)
		}
	} else {
		if f.Domain != "" || len(f.Exposes) > 0 {
			return fmt.Errorf("容器未获取 IPv4，无法应用 DOMAIN 或 EXPOSE 配置")
		}
	}

	// AUTOSTART
	if f.Autostart != nil {
		if err := client.SetBootAutostart(name, *f.Autostart); err != nil {
			return fmt.Errorf("设置 AUTOSTART 失败: %w", err)
		}
	}

	// DOMAIN
	if f.Domain != "" {
		if err := client.SetDomain(name, f.Domain); err != nil {
			return fmt.Errorf("设置 DOMAIN 失败: %w", err)
		} else if ip != "" {
			addresses := waitForIPAddresses(client, name, 5, true)
			if len(addresses) == 0 {
				addresses = []string{ip}
			}
			if err := updateHostsAddresses(name, f.Domain, addresses); err != nil {
				return fmt.Errorf("更新 /etc/hosts 失败: %w", err)
			}
			fmt.Printf("✔ 域名映射: %s -> %s\n", f.Domain, ip)
		}
	}

	// EXPOSE -> 端口映射
	if len(f.Exposes) > 0 && ip != "" {
		for _, exp := range f.Exposes {
			if err := client.AddPortMapping(name, exp.Port, exp.Port, exp.Protocol); err != nil {
				return fmt.Errorf("端口映射 %d/%s 失败: %w", exp.Port, exp.Protocol, err)
			}
			fmt.Printf("✔ 端口映射: %d/%s\n", exp.Port, exp.Protocol)
		}
	}

	completed = true
	return nil
}

func validateRuntimePortMappings(exposes []PortSpec, containers []Container) error {
	requested := make(map[string]bool, len(exposes))
	for _, exp := range exposes {
		key := fmt.Sprintf("%d/%s", exp.Port, exp.Protocol)
		if requested[key] {
			return fmt.Errorf("EXPOSE 重复声明端口 %s", key)
		}
		requested[key] = true
	}
	for _, container := range containers {
		for _, mapping := range container.PortMappings() {
			key := fmt.Sprintf("%d/%s", mapping.HostPort, mapping.Protocol)
			if requested[key] {
				return fmt.Errorf("EXPOSE 端口 %s 已被容器 %s 使用", key, container.Name)
			}
		}
	}
	return nil
}

// applyEnvs 将 ENV 指令写入容器的 /etc/environment 和 /etc/profile.d/bocker-env.sh。
// /etc/environment 由 PAM 在登录会话中加载，profile.d 脚本由 sh 登录 shell 加载。
// 幂等性：/etc/bocker.env 为覆盖式写入；/etc/environment 先移除 bocker 管理的行再追加，避免重复构建污染。
func applyEnvs(client *IncusClient, name string, envs []EnvSpec) error {
	if len(envs) == 0 {
		return nil
	}
	envs = dedupeEnvSpecs(envs)
	var envBuf bytes.Buffer
	for _, e := range envs {
		envBuf.WriteString(fmt.Sprintf("%s=%s\n", e.Key, shellQuote(e.Value)))
	}
	if err := client.PushFile(name, "/etc/bocker.env", envBuf.Bytes(), "0644"); err != nil {
		return fmt.Errorf("写入 /etc/bocker.env 失败: %w", err)
	}

	// profile.d 脚本: 让登录 shell 自动导出变量
	profile := "#!/bin/sh\n# Generated by bocker build\n[ -f /etc/bocker.env ] && set -a && . /etc/bocker.env && set +a\n"
	if err := client.PushFile(name, "/etc/profile.d/bocker-env.sh", []byte(profile), "0755"); err != nil {
		return fmt.Errorf("写入 /etc/profile.d/bocker-env.sh 失败: %w", err)
	}

	// /etc/environment (PAM 读取)：移除 bocker 已管理的 KEY= 行（幂等），再追加本次的
	envKeys := map[string]bool{}
	for _, e := range envs {
		envKeys[e.Key] = true
	}
	existing, _ := client.ReadFile(name, "/etc/environment")
	var envFile bytes.Buffer
	for _, line := range strings.Split(existing, "\n") {
		trimmed := strings.TrimSpace(line)
		skip := false
		if idx := strings.IndexByte(trimmed, '='); idx >= 0 {
			key := strings.TrimSpace(trimmed[:idx])
			skip = envKeys[key]
		}
		if !skip && trimmed != "" {
			envFile.WriteString(line + "\n")
		}
	}
	for _, e := range envs {
		envFile.WriteString(fmt.Sprintf("%s=%s\n", e.Key, quoteEnvironmentValue(e.Value)))
	}
	if err := client.PushFile(name, "/etc/environment", envFile.Bytes(), "0644"); err != nil {
		return fmt.Errorf("更新 /etc/environment 失败: %w", err)
	}
	return nil
}

func dedupeEnvSpecs(envs []EnvSpec) []EnvSpec {
	seen := make(map[string]bool, len(envs))
	result := make([]EnvSpec, 0, len(envs))
	for i := len(envs) - 1; i >= 0; i-- {
		if seen[envs[i].Key] {
			continue
		}
		seen[envs[i].Key] = true
		result = append(result, envs[i])
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func quoteEnvironmentValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

// applyCopy executes one COPY instruction from the Incusfile context.
func applyCopy(client *IncusClient, name string, cp CopySpec, contextDir string) error {
	return applyCopyDst(client, name, cp, contextDir, cp.Dst)
}

// applyCopyDst resolves sources through a context directory file descriptor.
// The kernel enforces both beneath-context traversal and no-symlink resolution,
// so replacing a path after validation cannot make the root-owned builder read
// a file outside the context.
func applyCopyDst(client *IncusClient, name string, cp CopySpec, contextDir, dst string) error {
	absContext, err := filepath.Abs(contextDir)
	if err != nil {
		return fmt.Errorf("解析 contextDir 失败: %w", err)
	}
	rel, err := copyContextRelativePath(absContext, cp.Src)
	if err != nil {
		return fmt.Errorf("COPY 源 %q 位于构建上下文之外 (路径穿越被拒绝)", cp.Src)
	}
	contextFD, err := unix.Open(absContext, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("打开构建上下文失败: %w", err)
	}
	defer unix.Close(contextFD)
	return copyContextEntry(client, name, contextFD, rel, dst)
}

func copyContextRelativePath(contextDir, source string) (string, error) {
	if filepath.IsAbs(source) {
		return "", fmt.Errorf("absolute COPY sources are not supported")
	}
	rel := filepath.Clean(source)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source escapes context %s", contextDir)
	}
	return filepath.ToSlash(rel), nil
}

func copyContextEntry(client *IncusClient, name string, contextFD int, source, dst string) error {
	fd, err := openContextEntry(contextFD, source, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		return fmt.Errorf("打开 COPY 源 %s 失败: %w", source, err)
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("读取 COPY 源 %s 元数据失败: %w", source, err)
	}
	mode := stat.Mode & unix.S_IFMT
	switch mode {
	case unix.S_IFREG:
		return pushContextFile(client, name, fd, source, dst, os.FileMode(stat.Mode))
	case unix.S_IFDIR:
		return copyContextDirectory(client, name, contextFD, fd, source, dst)
	default:
		return fmt.Errorf("COPY 只支持普通文件和目录: %s", source)
	}
}

func openContextEntry(contextFD int, source string, flags int) (int, error) {
	how := &unix.OpenHow{Flags: uint64(flags), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS}
	fd, err := unix.Openat2(contextFD, source, how)
	if err == nil || (err != unix.ENOSYS && err != unix.EINVAL) {
		return fd, err
	}
	return openContextEntryFallback(contextFD, source, flags)
}

// openContextEntryFallback maintains the same no-symlink and beneath-context
// invariant for kernels without openat2 by walking one component at a time
// from an already-open context directory fd.
func openContextEntryFallback(contextFD int, source string, flags int) (int, error) {
	parts := strings.Split(source, "/")
	current, err := unix.Dup(contextFD)
	if err != nil {
		return -1, err
	}
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			unix.Close(current)
			return -1, fmt.Errorf("invalid COPY path component")
		}
		openFlags := flags
		if i < len(parts)-1 {
			openFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		}
		next, err := unix.Openat(current, part, openFlags, 0)
		unix.Close(current)
		if err != nil {
			return -1, err
		}
		current = next
	}
	return current, nil
}

func copyContextDirectory(client *IncusClient, name string, contextFD, dirFD int, source, dst string) error {
	dst = path.Clean(dst)
	if dst != "/" {
		if err := client.ExecStreaming(name, "mkdir -p "+shellQuote(dst), nil); err != nil {
			return fmt.Errorf("创建目标目录 %s 失败: %w", dst, err)
		}
	}
	dupFD, err := unix.Dup(dirFD)
	if err != nil {
		return fmt.Errorf("复制 COPY 目录描述符失败: %w", err)
	}
	dir := os.NewFile(uintptr(dupFD), source)
	if dir == nil {
		return fmt.Errorf("打开 COPY 目录 %s 失败", source)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("读取 COPY 目录 %s 失败: %w", source, err)
	}
	for _, entry := range entries {
		childDst := path.Join(dst, entry.Name())
		if err := copyContextEntry(client, name, contextFD, path.Join(source, entry.Name()), childDst); err != nil {
			return err
		}
	}
	return nil
}

func pushContextFile(client *IncusClient, name string, fd int, source, dstPath string, mode os.FileMode) error {
	dupFD, err := unix.Dup(fd)
	if err != nil {
		return fmt.Errorf("复制 COPY 文件描述符失败: %w", err)
	}
	file := os.NewFile(uintptr(dupFD), source)
	if file == nil {
		return fmt.Errorf("打开 COPY 文件 %s 失败", source)
	}
	defer file.Close()
	explicitDirectory := strings.HasSuffix(dstPath, "/") || dstPath == "."
	dstPath = path.Clean(dstPath)
	// 判断目标是否是目录：
	// 1. 显式以 / 结尾或为 . → 视为目录
	// 2. 容器内 test -d 成功 → 是目录
	// 是目录则追加源文件名作为最终目标
	if explicitDirectory {
		dstPath = path.Join(dstPath, path.Base(source))
	} else {
		// 在容器内检查目标是否是已存在的目录
		if _, err := client.execQuiet(name, "test", "-d", dstPath); err == nil {
			dstPath = path.Join(dstPath, path.Base(source))
		}
	}
	// 确保目标父目录存在 (与 Docker COPY 行为一致)
	if parent := path.Dir(dstPath); parent != "" && parent != "/" && parent != "." {
		if err := client.ExecStreaming(name, "mkdir -p "+shellQuote(parent), nil); err != nil {
			return fmt.Errorf("创建目标父目录 %s 失败: %w", parent, err)
		}
	}
	modeStr := fmt.Sprintf("%04o", mode.Perm())
	return client.PushFileReader(name, dstPath, file, modeStr)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// CmdRun 从本地镜像创建并启动容器，并恢复构建时保存的运行配置。
func CmdRun(args []string) error {
	name, args, err := nameOptionFromArgs(args)
	if err != nil {
		return err
	}
	permissionOverride := hasPermissionOverride(args)
	permissionMode, args, err := permissionModeFromArgs(args)
	if err != nil {
		return err
	}
	networkOverride := hasNetworkOverride(args)
	networkMode, args, err := networkModeFromArgs(args)
	if err != nil {
		return err
	}
	if len(args) > 1 {
		return fmt.Errorf("用法: bocker image run [image] [--name <name>]")
	}

	client := NewIncusClient()
	alias := ""
	interactiveImage := len(args) == 0
	if !interactiveImage {
		alias = args[0]
	} else {
		aliases, listErr := client.ListLocalImageAliases()
		if listErr != nil {
			return fmt.Errorf("读取本地镜像列表失败: %w", listErr)
		}
		if len(aliases) == 0 {
			fmt.Println("本地没有可运行的镜像。请先执行 'bocker image build [Incusfile]'。")
			return nil
		}
		choice := selectMenu(aliases, "选择要运行的本地镜像 (↑↓ 选择, Enter 确认, q 退出)")
		if choice < 0 {
			return nil
		}
		alias = aliases[choice]
	}
	if err := validateBockerName(alias); err != nil {
		return fmt.Errorf("镜像名称 %q 无效: %w", alias, err)
	}
	if name == "" {
		defaultName := defaultNameFromImage(alias)
		name = prompt(fmt.Sprintf("容器名称 (回车默认 %s): ", defaultName))
		if name == "" {
			name = defaultName
		}
	}
	if err := validateBockerName(name); err != nil {
		return fmt.Errorf("容器名称 %q 无效: %w", name, err)
	}

	if interactiveImage && !networkOverride {
		selected, ok := selectNetworkMode(networkMode)
		if !ok {
			return nil
		}
		networkMode = selected
		networkOverride = true
	}
	if interactiveImage && !permissionOverride {
		selected, ok := selectPermissionMode(permissionMode)
		if !ok {
			return nil
		}
		permissionMode = selected
	}
	properties, err := client.GetImageProperties(alias)
	if err != nil {
		return fmt.Errorf("读取本地镜像 %s 失败: %w", alias, err)
	}
	f, err := runtimeConfigFromImageProperties(name, properties)
	if err != nil {
		return fmt.Errorf("镜像 %s 的运行配置无效: %w", alias, err)
	}
	if !networkOverride {
		if value := properties[containerNetworkConfig]; value != "" {
			networkMode, err = ParseNetworkMode(value)
			if err != nil {
				return fmt.Errorf("镜像 %s 的网络配置无效: %w", alias, err)
			}
		}
	}

	fmt.Printf("▶ 从本地镜像 %s 创建并启动容器 %s\n", alias, name)
	if len(f.Exposes) > 0 {
		fmt.Printf("  EXPOSE: %s\n", exposeString(f.Exposes))
	}
	if f.Domain != "" {
		fmt.Printf("  DOMAIN:  %s\n", f.Domain)
	}
	if f.Autostart != nil {
		fmt.Printf("  AUTOSTART: %s\n", strconv.FormatBool(*f.Autostart))
	}
	fmt.Println()

	return runFromBuiltImage(client, alias, f, networkMode, permissionMode)
}

func runtimeConfigFromImageProperties(name string, properties map[string]string) (*Incusfile, error) {
	f := &Incusfile{
		Name:    name,
		Exposes: parseExposeString(properties["user.bocker.expose"]),
		Domain:  properties["user.bocker.domain"],
	}
	if value := properties["user.bocker.autostart"]; value != "" {
		autostart, err := parseBoolPayload(value)
		if err != nil {
			return nil, fmt.Errorf("AUTOSTART: %w", err)
		}
		f.Autostart = &autostart
	}
	if value := properties["user.bocker.env"]; value != "" {
		if err := json.Unmarshal([]byte(value), &f.Env); err != nil {
			return nil, fmt.Errorf("ENV: %w", err)
		}
		for _, env := range f.Env {
			if err := validateEnvKey(env.Key); err != nil {
				return nil, fmt.Errorf("ENV: %w", err)
			}
		}
		f.Env = dedupeEnvSpecs(f.Env)
	}
	return f, nil
}

func incusEnvironmentConfig(envs []EnvSpec) map[string]string {
	config := make(map[string]string, len(envs))
	for _, env := range dedupeEnvSpecs(envs) {
		config["environment."+env.Key] = env.Value
	}
	return config
}
