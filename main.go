package main

import (
	"fmt"
	"os"
)

// Version 工具版本
const Version = "1.1.0"

// MirrorRemote 镜像源在本地的 remote 名称
const MirrorRemote = "mirror-images"

// MirrorURL LXC 镜像源地址（清华源已失效，改用官方源）
const MirrorURL = "https://images.linuxcontainers.org/"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]
	if cmd == "__daemon" {
		if err := runEmbeddedDaemonSupervisor(); err != nil {
			fmt.Fprintf(os.Stderr, "bocker daemon: %v\n", err)
			os.Exit(1)
		}
		return
	}

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
	case "help", "-h", "--help", "--version", "-v", "version", "completion":
		return true
	}
	return false
}

// dispatch 命令分发
func dispatch(cmd string, args []string) error {
	switch cmd {
	case "list", "ls":
		return CmdList()
	case "start":
		return withContainer(args, "选择要启动的容器", CmdStart)
	case "stop":
		return withContainer(args, "选择要停止的容器", CmdStop)
	case "restart":
		return withContainer(args, "选择要重启的容器", CmdRestart)
	case "in":
		return withContainer(args, "选择要进入的容器", CmdIn)
	case "exec":
		return CmdExec(args)
	case "set":
		return CmdSet(args)
	case "export":
		return withContainer(args, "选择要导出的容器", CmdExport)
	case "import":
		return CmdImport(args)
	case "install", "i":
		return CmdInstall(args)
	case "remove", "rm":
		return CmdRemove(args)
	case "uninstall":
		// 旧别名: 行为同 remove container (兼容老用户)
		fmt.Println("提示: uninstall 已改名为 remove, 用法: bocker remove container [名]")
		return CmdRemove(append([]string{"container"}, args...))
	case "build":
		return CmdBuild(args)
	case "create":
		return CmdCreate(args)
	case "run":
		// 旧别名兼容
		fmt.Println("提示: run 已改名为 create, 用法: bocker create [容器名]")
		return CmdCreate(args)
	case "images", "image":
		return CmdImages(args)
	case "completion":
		return CmdCompletion(args)
	case "__complete":
		return CmdCompletionCandidates(args)
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

// withContainer 若 args 中有容器名则直接使用，否则弹出交互式选择菜单。
func withContainer(args []string, label string, fn func(string) error) error {
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
	fmt.Printf(`bocker - 独立容器管理工具 v%s

容器管理:
  bocker list                 | 列出已安装容器
  bocker install              | 安装新容器 (交互式选择发行版)
  bocker install --network <bridge|nat> --permission <normal|super> [镜像] [名称]
  bocker remove [container|image] [名] | 删除容器或镜像 (交互式选择)
  bocker uninstall            | 删除容器 (旧别名, 等同 remove container)
  bocker start   [容器名]     | 启动容器
  bocker stop    [容器名]     | 停止容器
  bocker restart [容器名]     | 重启容器
  bocker in      [容器名]     | 进入容器 shell
  bocker exec    <容器名> <命令...> | 在容器内执行命令 (非交互)
  bocker export  [容器名]     | 导出容器为 tar.gz
  bocker import  [文件] [名]  | 从 tar.gz 导入容器

容器设置 (bocker set <容器名> ...):
  bocker set <容器名> port [规格]      | 端口映射 (规格如 8080:80/tcp)
  bocker set <容器名> port rm <规格>   | 取消端口映射
  bocker set <容器名> port list        | 查看端口映射
  bocker set <容器名> domain <域名>    | 域名映射 (写入 /etc/hosts)
  bocker set <容器名> autostart [on|off] | 开机自启动
  bocker set <容器名> network [bridge|nat] | 切换网络模式，省略时交互选择 (需停止容器)

镜像构建 (类似 Dockerfile):
  bocker build [Incusfile]               | 构建镜像 (默认 ./Incusfile)
  bocker build --name <名> [Incusfile]   | 覆盖镜像别名
  bocker build show                      | 列出可用的基础镜像
  bocker create [容器名] [--permission normal|super] | 从 ./Incusfile 创建+启动容器
  bocker images                          | 列出本地镜像别名 (build 产物)
  bocker completion bash|zsh|fish        | 输出 Tab 补全脚本
  bocker completion install [shell]      | 安装系统级 Tab 补全

  Incusfile 指令:
    FROM <镜像>   NAME <名称>     RUN <命令>
    COPY <源> <目标>   ENV <K=V>
    NETWORK bridge|nat 网络模式 (bridge=局域网直连, nat=地址转换)
    EXPOSE <端口>   DOMAIN <域名>   AUTOSTART on|off
    ENTRYPOINT <命令>   CMD <命令/默认参数>
    TEMP <名称> ... END   临时构建块 (隔离编译工具链)

其他:
  bocker help                 | 显示此帮助

网络模式也可通过 BOCKER_NETWORK=bridge|nat 设置，默认 bridge。

提示: [容器名] 省略时进入交互式选择菜单
`, Version)
}

// CmdImages 列出本地镜像别名 (bocker build 的产物)。
// 显示别名、镜像指纹(短)、大小、创建时间。
func CmdImages(args []string) error {
	client := NewIncusClient()
	infos, err := client.ListLocalImageAliasesWithDetails()
	if err != nil {
		return fmt.Errorf("读取本地镜像列表失败: %w", err)
	}
	if len(infos) == 0 {
		fmt.Println("本地无镜像别名。用 'bocker build' 构建镜像。")
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
