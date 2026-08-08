package bocker

import (
	"fmt"
	"strings"
)

// CmdSet 容器设置：交互式菜单或直接命令行参数设置。
func CmdSet(args []string) error {
	client := NewIncusClient()
	name := ""
	if len(args) >= 1 {
		name = args[0]
	} else {
		var err error
		name, err = selectContainer("选择要设置的容器")
		if err != nil || name == "" {
			return err
		}
	}

	ct, err := client.GetContainer(name)
	if err != nil {
		return err
	}

	if len(args) >= 2 {
		sub := strings.ToLower(args[1])
		switch sub {
		case "domain":
			if len(args) != 3 {
				return fmt.Errorf("用法: bocker container set %s domain <domain|--unset>", name)
			}
			if args[2] != "--unset" && args[2] != "" {
				return applyDomain(client, ct, args[2])
			}
			return removeDomain(client, ct)
		case "port":
			return cmdSetPort(client, ct, args[2:])
		case "autostart":
			if len(args) != 3 {
				return fmt.Errorf("用法: bocker container set %s autostart on|off", name)
			}
			on, err := parseBoolPayload(args[2])
			if err != nil {
				return fmt.Errorf("autostart 参数无效: %w", err)
			}
			if err := client.SetBootAutostart(name, on); err != nil {
				return err
			}
			if on {
				fmt.Printf("✔ 容器 %s 已开启开机自启动\n", name)
			} else {
				fmt.Printf("✔ 容器 %s 已关闭开机自启动\n", name)
			}
			return nil
		case "network":
			if len(args) != 3 {
				return fmt.Errorf("用法: bocker container set %s network bridge|nat", name)
			}
			if strings.EqualFold(ct.Status, "Running") {
				return fmt.Errorf("容器 %s 正在运行，请先执行 'bocker container stop %s'", name, name)
			}
			mode, err := ParseNetworkMode(args[2])
			if err != nil {
				return err
			}
			if err := client.SetContainerNetwork(name, mode, true); err != nil {
				return err
			}
			fmt.Printf("✔ 容器 %s 网络已设置为 %s\n", name, mode)
			return nil
		default:
			return fmt.Errorf("未知 container set 设置: %s (可用: domain, port, autostart, network)", sub)
		}
	}

	options := []string{
		"域名映射",
		"取消域名映射",
		"端口映射",
		"取消端口映射",
		"开机自启动",
		"关闭开机自启动",
		"网络模式",
	}

	fmt.Printf("容器: %s  (状态: %s, 网络: %s, 自启: %s, 域名: %s, 端口: %s)\n",
		name, strings.ToLower(ct.Status), ct.NetworkMode(), autostartBadge(ct.Autostart()), orNA(ct.Domain()),
		portSummary(ct.PortMappings()))
	choice := selectMenu(options, "选择操作 (↑↓ 选择, Enter 确认, q 退出)")
	if choice < 0 {
		return nil
	}

	switch choice {
	case 0:
		domain := prompt("域名 (如 alpine.test): ")
		return applyDomain(client, ct, domain)
	case 1:
		return removeDomain(client, ct)
	case 2:
		// 进入端口映射交互菜单
		return cmdSetPort(client, ct, nil)
	case 3:
		// 直接进入“取消端口映射”流程：列出当前映射并选择移除
		return cmdSetPortRemoveInteractive(client, ct)
	case 4:
		if err := client.SetBootAutostart(name, true); err != nil {
			return err
		}
		fmt.Printf("✔ 容器 %s 已开启开机自启动\n", name)
	case 5:
		if err := client.SetBootAutostart(name, false); err != nil {
			return err
		}
		fmt.Printf("✔ 容器 %s 已关闭开机自启动\n", name)
	case 6:
		if strings.EqualFold(ct.Status, "Running") {
			return fmt.Errorf("容器 %s 正在运行，请先 stop 后再切换网络", name)
		}
		mode, ok := selectNetworkMode(NetworkMode(ct.NetworkMode()))
		if !ok {
			return nil
		}
		if err := client.SetContainerNetwork(name, mode, true); err != nil {
			return err
		}
		fmt.Printf("✔ 容器 %s 网络已设置为 %s\n", name, mode)
	}
	return nil
}

// cmdSetPortRemoveInteractive 直接进入“取消端口映射”流程：列出当前映射并选择移除。
func cmdSetPortRemoveInteractive(client *IncusClient, ct *Container) error {
	mappings := ct.PortMappings()
	if len(mappings) == 0 {
		fmt.Println("该容器未配置端口映射。")
		return nil
	}
	options := make([]string, 0, len(mappings))
	for _, m := range mappings {
		label := fmt.Sprintf("%d/%s", m.HostPort, m.Protocol)
		if m.HostPort != m.ContainerPort {
			label = fmt.Sprintf("%d/%s -> %d", m.HostPort, m.Protocol, m.ContainerPort)
		}
		options = append(options, label)
	}
	choice := selectMenu(options, "选择要移除的端口映射 (↑↓ 选择, Enter 确认, q 退出)")
	if choice < 0 || choice >= len(mappings) {
		return nil
	}
	m := mappings[choice]
	if err := client.RemovePortMapping(ct.Name, m.HostPort, m.Protocol); err != nil {
		return err
	}
	fmt.Printf("✔ 已移除端口映射 %d/%s\n", m.HostPort, m.Protocol)
	return nil
}

// orNA 空值显示为 N/A。
func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}

// setDomain 设置域名映射，若容器已运行则立即更新 /etc/hosts。
func applyDomain(client *IncusClient, ct *Container, domain string) error {
	if err := validateDomainName(domain); err != nil {
		return fmt.Errorf("域名无效: %w", err)
	}
	if err := client.SetDomain(ct.Name, domain); err != nil {
		return err
	}
	fmt.Printf("✔ 域名映射已保存: %s\n", domain)

	// 容器运行中则立即写入 /etc/hosts
	if strings.EqualFold(ct.Status, "Running") {
		ip := ct.IPv4()
		if ip == "" {
			ip = waitForIP(client, ct.Name, 5)
		}
		if ip != "" {
			addresses := waitForIPAddresses(client, ct.Name, 5, true)
			if len(addresses) == 0 {
				addresses = []string{ip}
			}
			if err := updateHostsAddresses(ct.Name, domain, addresses); err != nil {
				return fmt.Errorf("更新 /etc/hosts 失败: %w", err)
			}
			fmt.Printf("✔ 已更新 /etc/hosts: %s -> %s\n", domain, ip)
		} else {
			fmt.Printf("⚠ 容器未获取到 IPv4，将在下次启动时写入\n")
		}
	} else {
		fmt.Printf("ℹ 容器未运行，将在启动时自动写入 /etc/hosts\n")
	}
	return nil
}

// removeDomain 取消域名映射并移除 /etc/hosts 中的对应行。
func removeDomain(client *IncusClient, ct *Container) error {
	if ct.Domain() == "" {
		fmt.Println("该容器未配置域名映射。")
		return nil
	}
	if err := client.UnsetDomain(ct.Name); err != nil {
		return err
	}
	if err := removeHostsLine(ct.Name); err != nil {
		return fmt.Errorf("移除 /etc/hosts 行失败: %w", err)
	}
	fmt.Printf("✔ 域名映射已取消，并清理 /etc/hosts\n")
	return nil
}
