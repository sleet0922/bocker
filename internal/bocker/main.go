package bocker

import (
	"encoding/json"
	"fmt"
	"os"
)

// Version 工具版本
const Version = "3.0.7"

// MirrorRemote 镜像源在本地的 remote 名称
const MirrorRemote = "mirror-images"

// MirrorURL LXC 镜像源地址（清华源已失效，改用官方源）
const MirrorURL = "https://images.linuxcontainers.org/"

// Main is the process entry point used by cmd/bocker.
func Main() {
	if len(os.Args) >= 2 && os.Args[1] == "__daemon" {
		if err := runEmbeddedDaemonSupervisor(); err != nil {
			fmt.Fprintf(os.Stderr, "bocker daemon: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}
	if shouldUsePrivilegedBroker(os.Args[1:]) {
		exitCode, err := runPrivilegedBrokerCommand(os.Args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "✘ %v\n", err)
			os.Exit(1)
		}
		if exitCode != 0 {
			os.Exit(exitCode)
		}
		return
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	// 轻量命令：跳过启动期网络探测，保证 help/version 等即时响应
	if isLightweightCommand(cmd) {
		if err := dispatch(cmd, args); err != nil {
			fmt.Fprintf(os.Stderr, "✘ %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := dispatch(cmd, args); err != nil {
		fmt.Fprintf(os.Stderr, "✘ %v\n", err)
		os.Exit(1)
	}
}

// isLightweightCommand 判断命令是否为轻量级（无需连接 Incus）。
// help/version 等纯本地命令跳过所有启动期副作用，保证即时响应。
func isLightweightCommand(cmd string) bool {
	switch cmd {
	case "help", "-h", "--help", "--version", "-v", "version":
		return true
	}
	return false
}

// dispatch 命令分发
func dispatch(cmd string, args []string) error {
	switch cmd {
	case "template":
		return dispatchTemplate(args)
	case "image":
		return dispatchImage(args)
	case "container":
		return dispatchContainer(args)
	case "help", "-h", "--help":
		printUsage()
		return nil
	case "--version", "-v", "version":
		fmt.Printf("bocker v%s\n", Version)
		return nil
	default:
		return fmt.Errorf("未知命令: %s (使用 'bocker help' 查看可用命令)", cmd)
	}
}

func dispatchTemplate(args []string) error {
	if len(args) == 0 {
		choice := selectMenu([]string{
			"list - 查看远程模板",
			"install - 选择模板并安装容器",
		}, "选择 template 操作 (↑↓ 选择, Enter 确认, q 退出)")
		switch choice {
		case 0:
			return CmdTemplateList(nil)
		case 1:
			return CmdInstall(nil)
		default:
			return nil
		}
	}
	switch args[0] {
	case "list":
		return CmdTemplateList(args[1:])
	case "install":
		return CmdInstall(args[1:])
	default:
		return fmt.Errorf("未知 template 动作: %s (可用: list, install)", args[0])
	}
}

func dispatchImage(args []string) error {
	if len(args) == 0 {
		choice := selectMenu([]string{
			"list - 查看本地镜像",
			"build - 按 Incusfile 制作镜像",
			"run - 选择镜像并启动容器",
			"remove - 删除本地镜像",
		}, "选择 image 操作 (↑↓ 选择, Enter 确认, q 退出)")
		switch choice {
		case 0:
			return CmdImages(nil)
		case 1:
			return CmdBuild(nil)
		case 2:
			return CmdRun(nil)
		case 3:
			return CmdRemoveImage(nil)
		default:
			return nil
		}
	}
	switch args[0] {
	case "build":
		return CmdBuild(args[1:])
	case "list":
		return CmdImages(args[1:])
	case "run":
		return CmdRun(args[1:])
	case "remove":
		return CmdRemoveImage(args[1:])
	default:
		return fmt.Errorf("未知 image 动作: %s (可用: build, list, run, remove)", args[0])
	}
}

func dispatchContainer(args []string) error {
	if len(args) == 0 {
		choice := selectMenu([]string{
			"list - 查看所有容器",
			"shell - 进入容器命令行",
			"exec - 在容器内执行命令",
			"start - 启动容器",
			"stop - 停止容器",
			"restart - 重启容器",
			"set - 修改容器设置",
			"export - 导出容器备份",
			"import - 导入容器备份",
			"remove - 删除容器",
		}, "选择 container 操作 (↑↓ 选择, Enter 确认, q 退出)")
		switch choice {
		case 0:
			return CmdList(nil)
		case 1:
			return withContainer(nil, "选择要进入的容器", "shell", CmdShell)
		case 2:
			return CmdExecInteractive()
		case 3:
			return withContainer(nil, "选择要启动的容器", "start", CmdStart)
		case 4:
			return withContainer(nil, "选择要停止的容器", "stop", CmdStop)
		case 5:
			return withContainer(nil, "选择要重启的容器", "restart", CmdRestart)
		case 6:
			return CmdSet(nil)
		case 7:
			return withContainer(nil, "选择要导出的容器", "export", CmdExport)
		case 8:
			return CmdImport(nil)
		case 9:
			return CmdRemoveContainer(nil)
		default:
			return nil
		}
	}
	switch args[0] {
	case "list":
		return CmdList(args[1:])
	case "shell":
		return withContainer(args[1:], "选择要进入的容器", "shell", CmdShell)
	case "exec":
		return CmdExec(args[1:])
	case "start":
		return withContainer(args[1:], "选择要启动的容器", "start", CmdStart)
	case "stop":
		return withContainer(args[1:], "选择要停止的容器", "stop", CmdStop)
	case "restart":
		return withContainer(args[1:], "选择要重启的容器", "restart", CmdRestart)
	case "remove":
		return CmdRemoveContainer(args[1:])
	case "export":
		return withContainer(args[1:], "选择要导出的容器", "export", CmdExport)
	case "import":
		return CmdImport(args[1:])
	case "set":
		return CmdSet(args[1:])
	default:
		return fmt.Errorf("未知 container 动作: %s", args[0])
	}
}

// withContainer 若 args 中有容器名则直接使用，否则弹出交互式选择菜单。
func withContainer(args []string, label, action string, fn func(string) error) error {
	if len(args) > 1 {
		return fmt.Errorf("container %s 最多接受一个容器名", action)
	}
	if len(args) >= 1 {
		return fn(args[0])
	}
	name, err := selectContainer(label)
	if err != nil {
		return err
	}
	if name == "" {
		return nil
	}
	return fn(name)
}

// selectContainer 列出所有容器供用户选择，返回选中容器名。
func selectContainer(label string) (string, error) {
	client := NewIncusClient()
	cs, err := client.ListContainers()
	if err != nil {
		return "", err
	}
	if len(cs) == 0 {
		fmt.Println("暂无容器。")
		return "", nil
	}
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Name
	}
	choice := selectMenu(names, label+" (↑↓ 选择, Enter 确认, q 退出)")
	if choice < 0 {
		return "", nil
	}
	return names[choice], nil
}

func printUsage() {
	fmt.Printf("bocker - 独立容器管理工具 v%s\n\n", Version)
	fmt.Println("用法: bocker <资源> <动作> [参数]")
	fmt.Println("资源: template=远程模板，image=本地镜像，container=已经创建的容器")
	fmt.Println("说明: [内容] 可以省略，<内容> 必须填写。")

	printUsageSection("远程模板 template", []usageEntry{
		{"bocker template", "打开 template 操作菜单"},
		{"bocker template list [--json]", "列出远程模板；--json 给 GUI/脚本使用"},
		{"bocker template install [template] [--name <name>]", "下载模板，创建并启动容器"},
	})

	printUsageSection("本地镜像 image", []usageEntry{
		{"bocker image", "打开 image 操作菜单"},
		{"bocker image build [Incusfile]", "按 Incusfile 制作本地镜像"},
		{"bocker image list [--json]", "列出本地镜像；--json 给 GUI/脚本使用"},
		{"bocker image run [image] [--name <name>]", "选择或指定本地镜像，创建并启动容器"},
		{"bocker image remove [image]", "删除本地镜像"},
	})

	printUsageSection("已有容器 container", []usageEntry{
		{"bocker container", "打开 container 操作菜单"},
		{"bocker container list [--json]", "列出容器；--json 给 GUI/脚本使用"},
		{"bocker container shell [name]", "进入容器命令行"},
		{"bocker container exec <name> <command...>", "在容器里执行一条命令"},
		{"bocker container start [name]", "启动容器"},
		{"bocker container stop [name]", "停止容器"},
		{"bocker container restart [name]", "重启容器"},
		{"bocker container remove [name]", "删除容器"},
		{"bocker container export [name]", "导出容器备份"},
		{"bocker container import [file] [name]", "导入容器备份"},
		{"bocker container set <name> <setting> ...", "修改端口、域名、自启动或网络"},
	})

	printUsageSection("创建容器时可用", []usageEntry{
		{"--network <nat|bridge>", "NAT 私有网络或局域网直连；默认 bridge"},
		{"--permission <normal|super>", "普通权限或放宽隔离；默认 normal"},
		{"--name <name>", "指定新容器的名称"},
	})

	fmt.Println("\n示例: bocker template install debian:12 --name demo --network nat")
	fmt.Println("      bocker image build --name web-image ./Incusfile")
	fmt.Println("      bocker image run web-image --name web-01")
	fmt.Println("补全: CLI Debian 包会安装 Bash 自动补全；重新打开终端后生效。")
	fmt.Println("提示: 安装完成后普通用户可直接使用；日常操作不需要 sudo。")
}

type usageEntry struct {
	command     string
	description string
}

func printUsageSection(title string, entries []usageEntry) {
	fmt.Printf("\n%s:\n", title)
	for _, entry := range entries {
		fmt.Printf("  %-56s %s\n", entry.command, entry.description)
	}
}

// CmdImages 列出本地镜像别名 (bocker image build 的产物)。
// 显示别名、镜像指纹(短)、大小、创建时间。
func CmdImages(args []string) error {
	jsonOutput, err := parseJSONOutputOption(args)
	if err != nil {
		return fmt.Errorf("image list: %w", err)
	}
	client := NewIncusClient()
	infos, err := client.ListLocalImageAliasesWithDetails()
	if err != nil {
		return fmt.Errorf("读取本地镜像列表失败: %w", err)
	}
	if jsonOutput {
		type imageListItem struct {
			Name        string `json:"name"`
			Size        string `json:"size"`
			Created     string `json:"created"`
			Fingerprint string `json:"fingerprint"`
		}
		items := make([]imageListItem, 0, len(infos))
		for _, info := range infos {
			fingerprint := info.Target
			if len(fingerprint) > 12 {
				fingerprint = fingerprint[:12]
			}
			items = append(items, imageListItem{
				Name: info.Name, Size: humanSize(info.Size),
				Created: info.CreatedAt.Format("2006-01-02 15:04"), Fingerprint: fingerprint,
			})
		}
		return json.NewEncoder(os.Stdout).Encode(items)
	}
	if len(infos) == 0 {
		fmt.Println("本地没有 build 制作的镜像。查看远程模板请用 'bocker template list'。")
		return nil
	}
	fmt.Printf("╭─ 本地镜像 (共 %d 个)\n", len(infos))
	fmt.Println("│ 别名                          大小      创建时间          指纹(短)")
	fmt.Println("│ ──────────────────────────── ──────── ──────────────── ────────────")
	for _, info := range infos {
		fp := info.Target
		if len(fp) > 12 {
			fp = fp[:12]
		}
		fmt.Printf("│ %-29s %-8s %-16s %s\n", truncName(info.Name, 29), humanSize(info.Size), info.CreatedAt.Format("2006-01-02 15:04"), fp)
	}
	fmt.Println("╰─")
	return nil
}

func truncName(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	switch exp {
	case 0:
		return fmt.Sprintf("%.1fK", float64(bytes)/float64(div))
	case 1:
		return fmt.Sprintf("%.1fM", float64(bytes)/float64(div))
	case 2:
		return fmt.Sprintf("%.1fG", float64(bytes)/float64(div))
	default:
		return fmt.Sprintf("%.1fT", float64(bytes)/float64(div))
	}
}
