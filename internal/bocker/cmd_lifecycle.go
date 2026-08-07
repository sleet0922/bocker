package bocker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CmdStart 启动容器，若配置了域名映射则自动更新 /etc/hosts。
func CmdStart(name string) error {
	client := NewIncusClient()

	// 容器已在运行则跳过启动，继续处理域名逻辑
	ct, _ := client.GetContainer(name)
	if ct != nil && strings.EqualFold(ct.Status, "Running") {
		fmt.Printf("ℹ 容器 %s 已在运行\n", name)
	} else {
		fmt.Printf("启动容器 %s ...\n", name)
		if err := client.Start(name); err != nil {
			return err
		}
		// 重新获取容器信息以读取域名配置
		ct, _ = client.GetContainer(name)
	}

	// 等待容器 IPv4；bridge 模式额外补齐宿主机侧 shim 路由。
	ip := ""
	domain := ""
	if ct != nil {
		ip = ct.IPv4()
		domain = ct.Domain()
	}
	if ip == "" {
		ip = waitForIP(client, name, 15)
	}
	if ip != "" && ct != nil && ct.NetworkMode() == string(NetworkBridge) {
		warnAutoHostBridge(AutoConfigureHostBridge(client))
	}

	// 刷新端口映射的 connect 地址，应对 DHCP 重新分配导致容器 IP 变化的场景。
	if refreshed, rerr := client.RefreshPortMappings(name); rerr != nil {
		fmt.Printf("⚠ 刷新端口映射失败: %v\n", rerr)
	} else if refreshed > 0 {
		fmt.Printf("✔ 已刷新 %d 条端口映射的容器 IP (%s)\n", refreshed, ip)
	}

	// 检查域名映射，有则更新 /etc/hosts。
	if domain == "" {
		return nil
	}
	if ip == "" {
		fmt.Printf("⚠ 容器未获取到 IPv4，跳过 hosts 更新\n")
		return nil
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

// CmdRemove 删除容器或镜像。
// 用法:
//
//	bocker remove                     # 交互式: 先选容器/镜像, 再选具体项
//	bocker remove container [名]      # 删容器 (省略名则交互式选择)
//	bocker remove image [别名]        # 删镜像 (省略别名则交互式选择)
//	bocker remove [容器名]            # 兼容旧语法 (无子目标时按容器处理)
//
// 旧命令 uninstall 作为别名保留, 行为同 remove container。
func CmdRemove(args []string) error {
	// 有子目标参数
	if len(args) >= 1 {
		sub := strings.ToLower(args[0])
		switch sub {
		case "container", "ct", "c":
			return removeContainer(args[1:])
		case "image", "img", "i":
			return removeImage(args[1:])
		}
		// 兼容旧语法: 第一个参数像容器名 → 按容器处理
		return removeContainer(args)
	}
	// 无参数: 交互式选容器/镜像
	choice := selectMenu([]string{"容器 (container)", "镜像 (image)"}, "选择要删除的对象类型 (↑↓ 选择, Enter 确认, q 退出)")
	if choice < 0 {
		return nil
	}
	if choice == 0 {
		return removeContainer(nil)
	}
	return removeImage(nil)
}

// removeContainer 删除容器, 并清理 /etc/hosts 与 /32 路由残留。
func removeContainer(args []string) error {
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
	ct, _ := client.GetContainer(name)
	fmt.Printf("停止容器 %s ...\n", name)
	_ = client.Stop(name)
	fmt.Printf("删除容器 %s ...\n", name)
	if err := client.Delete(name); err != nil {
		return err
	}
	// 清理 /etc/hosts 残留行
	if err := removeHostsLine(name); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ 清理 /etc/hosts 失败: %v\n", err)
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

// removeImage 删除镜像别名及其指向的镜像 (无其他别名引用时一并删镜像)。
func removeImage(args []string) error {
	alias := ""
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
	// 二次确认
	confirm := selectMenu([]string{"确认删除", "取消"}, fmt.Sprintf("确认删除镜像 %s? (↑↓ 选择, Enter 确认)", alias))
	if confirm != 0 {
		fmt.Println("已取消")
		return nil
	}
	if err := client.DeleteImageByAlias(alias); err != nil {
		return err
	}
	fmt.Printf("✔ 镜像 %s 已删除\n", alias)
	return nil
}

// CmdStop 停止容器，并清理对应的 /32 路由避免死路由堆积。
func CmdStop(name string) error {
	fmt.Printf("停止容器 %s ...\n", name)
	client := NewIncusClient()
	// 先获取容器 IP 用于后续路由清理
	ct, _ := client.GetContainer(name)
	if err := client.Stop(name); err != nil {
		return err
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

// CmdIn 进入容器（交互式透传 stdio）。
func CmdIn(name string) error {
	return NewIncusClient().Exec(name)
}

// CmdRestart 重启容器：若容器正在运行则先停止再启动，若已停止则直接启动。
// 复用 CmdStop/CmdStart 以保证 /32 路由清理、端口映射刷新、域名 hosts 更新等副作用一致。
func CmdRestart(name string) error {
	client := NewIncusClient()
	ct, _ := client.GetContainer(name)
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
// 语法: bocker exec <容器名> <命令...>
// 示例: bocker exec web ls -la /app
//
//	bocker exec web "curl -s http://localhost/health"
//
// 命令通过 /bin/sh -c 执行，支持管道、重定向等 shell 特性。
// 省略容器名时弹出交互式选择菜单（与 start/stop/in 一致）。
func CmdExec(args []string) error {
	var name string
	var cmdArgs []string
	if len(args) >= 1 {
		name = args[0]
		cmdArgs = args[1:]
	} else {
		// 交互式选择容器
		n, err := selectContainer("选择要执行命令的容器")
		if err != nil {
			return err
		}
		if n == "" {
			return nil
		}
		name = n
	}
	if len(cmdArgs) == 0 {
		return fmt.Errorf("请提供要在 %s 内执行的命令\n示例: bocker exec %s ls -la /app\n      bocker exec %s curl -s http://localhost/health", name, name, name)
	}
	command := strings.Join(cmdArgs, " ")
	if err := NewIncusClient().ExecStreaming(name, command, nil); err != nil {
		return fmt.Errorf("exec %s 失败: %w", name, err)
	}
	return nil
}

// CmdExport 导出容器为 ./容器名_YYYYMMDD_HHMMSS.tar.gz。
func CmdExport(name string) error {
	ts := time.Now().Format("20060102_150405")
	path := fmt.Sprintf("./%s_%s.tar.gz", name, ts)
	fmt.Printf("导出容器 %s -> %s ...\n", name, path)
	if err := NewIncusClient().Export(name, path); err != nil {
		return err
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
	warnAutoHostBridge(AutoConfigureHostBridge(client))
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
