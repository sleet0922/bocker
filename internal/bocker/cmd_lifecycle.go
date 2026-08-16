package bocker

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CmdStart 启动容器，若配置了域名映射则自动更新 /etc/hosts。
func CmdStart(name string) error {
	client := NewIncusClient()

	// 容器已在运行则跳过启动，继续处理域名逻辑
	ct, err := client.GetContainer(name)
	if err != nil {
		return fmt.Errorf("读取容器 %s 失败: %w", name, err)
	}
	if ct != nil && strings.EqualFold(ct.Status, "Running") {
		fmt.Printf("ℹ 容器 %s 已在运行\n", name)
	} else {
		fmt.Printf("启动容器 %s ...\n", name)
		if err := client.Start(name); err != nil {
			return err
		}
		// 重新获取容器信息以读取域名配置
		ct, err = client.GetContainer(name)
		if err != nil {
			return fmt.Errorf("读取已启动容器 %s 失败: %w", name, err)
		}
	}

	// 等待容器 IPv4；bridge 模式额外补齐宿主机侧 shim 路由。
	ip := ""
	domain := ""
	if ct != nil {
		ip = ct.IPv4()
		domain = ct.Domain()
	}
	if ip == "" {
		ip = waitForIP(client, name, 30)
	}
	if ip == "" {
		return fmt.Errorf("容器 %s 在 30 秒内未获取 IPv4；请检查容器网络模式、DHCP 和宿主机网络配置", name)
	}
	if ip != "" && ct != nil && ct.NetworkMode() == string(NetworkBridge) {
		if err := AutoConfigureHostBridge(client); err != nil {
			return fmt.Errorf("配置宿主机 bridge 互通失败: %w", err)
		}
	}

	// 刷新端口映射的 connect 地址，应对 DHCP 重新分配导致容器 IP 变化的场景。
	if refreshed, rerr := client.RefreshPortMappings(name); rerr != nil {
		return fmt.Errorf("刷新端口映射失败: %w", rerr)
	} else if refreshed > 0 {
		fmt.Printf("✔ 已刷新 %d 条端口映射的容器 IP (%s)\n", refreshed, ip)
	}

	// 检查域名映射，有则更新 /etc/hosts。
	if domain == "" {
		return nil
	}
	if ip == "" {
		return fmt.Errorf("容器 %s 未获取到 IPv4，无法更新域名 %s", name, domain)
	}
	addresses := waitForIPAddresses(client, name, 5, true)
	if len(addresses) == 0 {
		addresses = []string{ip}
	}
	if err := updateHostsAddresses(name, domain, addresses); err != nil {
		return fmt.Errorf("更新 /etc/hosts 失败: %w", err)
	}
	fmt.Printf("✔ 已更新 /etc/hosts: %s -> %s\n", domain, ip)
	return nil
}

// CmdRemoveContainer 删除容器，并清理 /etc/hosts 与主机路由残留。
func CmdRemoveContainer(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("container remove 最多接受一个容器名")
	}
	name := ""
	if len(args) >= 1 {
		name = args[0]
	} else {
		n, err := selectContainer("选择要删除的容器")
		if err != nil {
			return err
		}
		if n == "" {
			return nil
		}
		name = n
	}
	client := NewIncusClient()
	// 先获取容器信息用于清理路由
	ct, err := client.GetContainer(name)
	if err != nil {
		return fmt.Errorf("读取容器 %s 失败: %w", name, err)
	}
	fmt.Printf("停止容器 %s ...\n", name)
	if err := client.Stop(name); err != nil {
		return fmt.Errorf("停止容器 %s 失败: %w", name, err)
	}
	fmt.Printf("删除容器 %s ...\n", name)
	if err := client.Delete(name); err != nil {
		return err
	}
	// 清理 /etc/hosts 残留行
	if err := removeHostsLine(name); err != nil {
		return fmt.Errorf("删除容器后清理 /etc/hosts 失败: %w", err)
	}
	// 清理 /32 路由残留
	if ct != nil && ct.UsesBridgeNIC(defaultNICName) {
		if ip := ct.IPv4(); ip != "" {
			removeHostBridgeRoute(ip)
		}
		for _, ip := range ct.IPv6Addresses() {
			removeHostBridgeIPv6Route(ip)
		}
	}
	fmt.Printf("✔ 容器 %s 已删除\n", name)
	return nil
}

// CmdRemoveImage 删除镜像别名及其指向的镜像 (无其他别名引用时一并删镜像)。
func CmdRemoveImage(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("image remove 最多接受一个镜像名")
	}
	alias := ""
	interactive := len(args) == 0
	if len(args) >= 1 {
		alias = args[0]
	} else {
		client := NewIncusClient()
		aliases, err := client.ListLocalImageAliases()
		if err != nil {
			return fmt.Errorf("读取本地镜像列表失败: %w", err)
		}
		if len(aliases) == 0 {
			fmt.Println("本地无镜像别名。")
			return nil
		}
		choice := selectMenu(aliases, "选择要删除的镜像 (↑↓ 选择, Enter 确认, q 退出)")
		if choice < 0 {
			return nil
		}
		alias = aliases[choice]
	}
	// 先验证别名存在 (DeleteImageByAlias 内部会再次校验, 这里提前报错避免无谓确认)
	client := NewIncusClient()
	if _, err := client.GetImageAliasEntry(alias); err != nil {
		return fmt.Errorf("镜像别名 %s 不存在", alias)
	}
	if interactive {
		confirm := selectMenu([]string{"确认删除", "取消"}, fmt.Sprintf("确认删除镜像 %s? (↑↓ 选择, Enter 确认)", alias))
		if confirm != 0 {
			fmt.Println("已取消")
			return nil
		}
	}
	if err := client.DeleteImageByAlias(alias); err != nil {
		return err
	}
	fmt.Printf("✔ 镜像 %s 已删除\n", alias)
	return nil
}

// CmdStop 停止容器，并清理域名映射与对应的 /32 路由，避免宿主机
// 继续把请求发往已离线的容器。
func CmdStop(name string) error {
	fmt.Printf("停止容器 %s ...\n", name)
	client := NewIncusClient()
	// 先获取容器 IP 用于后续路由清理
	ct, err := client.GetContainer(name)
	if err != nil {
		return fmt.Errorf("读取容器 %s 失败: %w", name, err)
	}
	if err := client.Stop(name); err != nil {
		return err
	}
	if err := removeHostsLine(name); err != nil {
		return fmt.Errorf("停止容器后清理 /etc/hosts 失败: %w", err)
	}
	// 清理 /32 路由
	if ct != nil && ct.UsesBridgeNIC(defaultNICName) {
		if ip := ct.IPv4(); ip != "" {
			removeHostBridgeRoute(ip)
		}
		for _, ip := range ct.IPv6Addresses() {
			removeHostBridgeIPv6Route(ip)
		}
	}
	return nil
}

// CmdShell 进入容器（交互式透传 stdio）。
func CmdShell(name string) error {
	return NewIncusClient().Exec(name)
}

// CmdRestart 重启容器：若容器正在运行则先停止再启动，若已停止则直接启动。
// 复用 CmdStop/CmdStart 以保证 /32 路由清理、端口映射刷新、域名 hosts 更新等副作用一致。
func CmdRestart(name string) error {
	client := NewIncusClient()
	ct, err := client.GetContainer(name)
	if err != nil {
		return fmt.Errorf("读取容器 %s 失败: %w", name, err)
	}
	if ct != nil && strings.EqualFold(ct.Status, "Running") {
		if err := CmdStop(name); err != nil {
			return fmt.Errorf("停止容器失败: %w", err)
		}
	} else {
		fmt.Printf("ℹ 容器 %s 未运行，直接启动\n", name)
	}
	return CmdStart(name)
}

// CmdExec 在容器内执行命令（非交互，stdout/stderr 实时输出）。
// 语法: bocker container exec <容器名> <命令...>
// 示例: bocker container exec web ls -la /app
//
//	bocker container exec web curl -s http://localhost/health
//
// 参数会原样传入容器进程，不经过 shell 重新解析。需要 shell 语法时请显式
// 传入解释器，例如: bocker container exec web sh -c 'echo hi | cat'。
func CmdExec(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("用法: bocker container exec <name> <command...>")
	}
	name := args[0]
	if err := NewIncusClient().ExecStreamingArgs(name, args[1:], nil); err != nil {
		return fmt.Errorf("exec %s 失败: %w", name, err)
	}
	return nil
}

// CmdExecInteractive 选择容器并询问要执行的命令。
// 输入按 shell 风格拆分为参数数组后原样传给容器进程（支持引号与转义，
// 但不会经过宿主机 shell 执行，也不会做变量展开）。
func CmdExecInteractive() error {
	name, err := selectContainer("选择要执行命令的容器")
	if err != nil || name == "" {
		return err
	}
	command := prompt("要执行的命令: ")
	if strings.TrimSpace(command) == "" {
		fmt.Println("未输入命令，已取消。")
		return nil
	}
	args, splitErr := shellSplit(command)
	if splitErr != nil {
		return fmt.Errorf("命令无法解析: %w", splitErr)
	}
	if len(args) == 0 {
		fmt.Println("未输入命令，已取消。")
		return nil
	}
	return CmdExec(append([]string{name}, args...))
}

// CmdExport 导出容器为当前目录中的唯一 tar.gz 文件。
func CmdExport(name string) error {
	ts := time.Now().Format("20060102_150405")
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Errorf("生成导出文件名失败: %w", err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("读取当前工作目录失败: %w", err)
	}
	path := filepath.Join(workingDirectory, fmt.Sprintf("%s_%s_%s.tar.gz", name, ts, hex.EncodeToString(suffix)))
	fmt.Printf("导出容器 %s -> %s ...\n", name, path)
	if err := NewIncusClient().Export(name, path); err != nil {
		return err
	}
	if uidText := strings.TrimSpace(os.Getenv(callerUIDEnv)); uidText != "" {
		uid, uidErr := strconv.Atoi(uidText)
		gid, gidErr := strconv.Atoi(strings.TrimSpace(os.Getenv(callerGIDEnv)))
		if uidErr != nil || gidErr != nil || uid < 0 || gid < 0 {
			return fmt.Errorf("导出文件所有者信息无效")
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("设置导出文件所有者失败: %w", err)
		}
	}
	fmt.Printf("✔ 已导出到 %s\n", path)
	return nil
}

// CmdImport 从备份文件导入容器。有参数则直接使用，无参数则扫描当前目录 .tar.gz 供选择。
func CmdImport(args []string) error {
	permissionMode, args, err := permissionModeFromArgs(args)
	if err != nil {
		return err
	}
	networkOverride := hasNetworkOverride(args)
	networkMode, args, err := networkModeFromArgs(args)
	if err != nil {
		return err
	}
	path := ""
	name := ""
	if len(args) >= 1 {
		path = args[0]
	} else {
		// 扫描当前目录下的 .tar.gz 文件
		matches, _ := filepath.Glob("*.tar.gz")
		if len(matches) == 0 {
			return fmt.Errorf("当前目录未找到 .tar.gz 文件")
		}
		choice := selectMenu(matches, "选择要导入的文件 (↑↓ 选择, Enter 确认, q 退出)")
		if choice < 0 {
			return nil
		}
		path = matches[choice]
	}
	if len(args) >= 2 {
		name = args[1]
	}
	if len(args) > 2 {
		return fmt.Errorf("import 只接受备份文件和可选容器名")
	}
	if name != "" {
		if err := validateBockerName(name); err != nil {
			return fmt.Errorf("容器名称 %q 无效: %w", name, err)
		}
	}
	fmt.Printf("导入 %s ...\n", path)
	client := NewIncusClient()
	var before map[string]bool
	if name == "" {
		before = containerNameSet(client)
	}
	if name != "" {
		if err := client.Import(path, name); err != nil {
			return err
		}
		if networkOverride {
			if err := client.ConfigureImportedNetworkWithMode(name, networkMode); err != nil {
				return err
			}
		} else if err := client.ConfigureImportedNetwork(name); err != nil {
			return err
		}
		if err := client.EnsurePermission(name, permissionMode); err != nil {
			return err
		}
	} else {
		if err := client.Import(path, ""); err != nil {
			return err
		}
		importedName := findImportedContainerName(client, before)
		if importedName == "" {
			return fmt.Errorf("导入完成，但无法确认新容器名，未配置网络")
		}
		if networkOverride {
			if err := client.ConfigureImportedNetworkWithMode(importedName, networkMode); err != nil {
				return err
			}
		} else if err := client.ConfigureImportedNetwork(importedName); err != nil {
			return err
		}
		if err := client.EnsurePermission(importedName, permissionMode); err != nil {
			return err
		}
	}
	if err := AutoConfigureHostBridge(client); err != nil {
		return fmt.Errorf("配置宿主机 bridge 互通失败: %w", err)
	}
	fmt.Printf("✔ 导入完成\n")
	return nil
}

func containerNameSet(client *IncusClient) map[string]bool {
	cs, err := client.ListContainers()
	if err != nil {
		return nil
	}
	names := make(map[string]bool, len(cs))
	for _, ct := range cs {
		names[ct.Name] = true
	}
	return names
}

func findImportedContainerName(client *IncusClient, before map[string]bool) string {
	if before == nil {
		return ""
	}
	cs, err := client.ListContainers()
	if err != nil {
		return ""
	}
	importedName := ""
	for _, ct := range cs {
		if before[ct.Name] {
			continue
		}
		if importedName != "" {
			return ""
		}
		importedName = ct.Name
	}
	return importedName
}
