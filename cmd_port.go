package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lxc/incus/v7/shared/api"
)

// PortMapping 描述一条宿主机到容器的端口映射。
type PortMapping struct {
	HostPort      int
	ContainerPort int
	Protocol      string // "tcp" 或 "udp"
	DeviceName    string
	IPv4          bool
	IPv6          bool
}

// portDevicePrefix bocker 管理的端口映射设备名前缀，便于识别与清理。
const portDevicePrefix = "port-"

// parsePortSpec 解析端口映射规格。
// 支持格式：
//
//	8080           -> host=8080, container=8080, proto=tcp
//	8080:80        -> host=8080, container=80,  proto=tcp
//	8080/udp       -> host=8080, container=8080, proto=udp
//	8080:80/udp    -> host=8080, container=80,  proto=udp
//	8080:80/tcp    -> host=8080, container=80,  proto=tcp
//
// spec 为空时返回错误。
func parsePortSpec(spec string) (hostPort, containerPort int, protocol string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		err = fmt.Errorf("端口规格不能为空")
		return
	}
	protocol = "tcp"
	if idx := strings.LastIndex(spec, "/"); idx >= 0 {
		protocol = strings.ToLower(strings.TrimSpace(spec[idx+1:]))
		spec = spec[:idx]
		if protocol != "tcp" && protocol != "udp" {
			err = fmt.Errorf("协议必须是 tcp 或 udp, 得到 %q", protocol)
			return
		}
	}
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) == 1 {
		hostPort, err = strconv.Atoi(parts[0])
		if err != nil {
			err = fmt.Errorf("端口 %q 不是数字", parts[0])
			return
		}
		containerPort = hostPort
	} else {
		hostPort, err = strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			err = fmt.Errorf("宿主端口 %q 不是数字", parts[0])
			return
		}
		containerPort, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			err = fmt.Errorf("容器端口 %q 不是数字", parts[1])
			return
		}
	}
	if hostPort < 1 || hostPort > 65535 {
		err = fmt.Errorf("宿主端口超出范围: %d", hostPort)
		return
	}
	if containerPort < 1 || containerPort > 65535 {
		err = fmt.Errorf("容器端口超出范围: %d", containerPort)
		return
	}
	return
}

// portDeviceName 根据宿主端口和协议生成设备名，形如 port-8080-tcp。
func portDeviceName(hostPort int, protocol string) string {
	return fmt.Sprintf("%s%d-%s", portDevicePrefix, hostPort, protocol)
}

func portDeviceNameForFamily(hostPort int, protocol, family string) string {
	name := portDeviceName(hostPort, protocol)
	if family == "v6" {
		return name + "-v6"
	}
	return name
}

// parsePortDeviceName 从设备名解析回宿主端口和协议。
func parsePortDeviceName(name string) (hostPort int, protocol string, ok bool) {
	hostPort, protocol, _, ok = parsePortDeviceNameWithFamily(name)
	return hostPort, protocol, ok
}

func parsePortDeviceNameWithFamily(name string) (hostPort int, protocol, family string, ok bool) {
	family = "v4"
	if strings.HasSuffix(name, "-v6") {
		family = "v6"
		name = strings.TrimSuffix(name, "-v6")
	}
	if !strings.HasPrefix(name, portDevicePrefix) {
		return 0, "", "", false
	}
	rest := name[len(portDevicePrefix):]
	idx := strings.LastIndex(rest, "-")
	if idx <= 0 || idx+1 >= len(rest) {
		return 0, "", "", false
	}
	port, err := strconv.Atoi(rest[:idx])
	if err != nil || port < 1 || port > 65535 {
		return 0, "", "", false
	}
	proto := rest[idx+1:]
	if proto != "tcp" && proto != "udp" {
		return 0, "", "", false
	}
	return port, proto, family, true
}

// parseConnectPort 从 connect 字符串解析容器端口。
// connect 形如 "tcp:0.0.0.0:80" 或 "tcp::80"。
func parseConnectPort(connect, proto string) int {
	prefix := proto + ":"
	if !strings.HasPrefix(connect, prefix) {
		return 0
	}
	rest := connect[len(prefix):]
	idx := strings.LastIndex(rest, ":")
	if idx < 0 || idx+1 >= len(rest) {
		return 0
	}
	port, err := strconv.Atoi(rest[idx+1:])
	if err != nil {
		return 0
	}
	return port
}

func proxyEndpoint(protocol, address string, port int) string {
	if strings.Contains(address, ":") {
		return fmt.Sprintf("%s:[%s]:%d", protocol, address, port)
	}
	return fmt.Sprintf("%s:%s:%d", protocol, address, port)
}

// PortMappings 从容器已加载的 Devices 中提取 bocker 管理的端口映射，按 host 端口升序。
func (ct *Container) PortMappings() []PortMapping {
	byMapping := map[string]*PortMapping{}
	seen := map[string]bool{}
	for _, devs := range []map[string]map[string]string{ct.Devices, ct.ExpandedDevices} {
		for devName, dev := range devs {
			if seen[devName] {
				continue
			}
			if dev["type"] != "proxy" {
				continue
			}
			hostPort, proto, family, ok := parsePortDeviceNameWithFamily(devName)
			if !ok {
				continue
			}
			containerPort := parseConnectPort(dev["connect"], proto)
			if containerPort == 0 {
				continue
			}
			seen[devName] = true
			key := fmt.Sprintf("%d/%s", hostPort, proto)
			mapping := byMapping[key]
			if mapping == nil {
				mapping = &PortMapping{HostPort: hostPort, ContainerPort: containerPort, Protocol: proto, DeviceName: devName}
				byMapping[key] = mapping
			}
			if strings.Contains(dev["listen"], ":[::]:") {
				mapping.IPv4 = true
				mapping.IPv6 = true
			} else if family == "v6" {
				mapping.IPv6 = true
			} else {
				mapping.IPv4 = true
			}
		}
	}
	result := make([]PortMapping, 0, len(byMapping))
	for _, mapping := range byMapping {
		result = append(result, *mapping)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].HostPort != result[j].HostPort {
			return result[i].HostPort < result[j].HostPort
		}
		return result[i].Protocol < result[j].Protocol
	})
	return result
}

// portSummary 用于在容器状态行中紧凑展示端口映射。
func portSummary(mappings []PortMapping) string {
	if len(mappings) == 0 {
		return "N/A"
	}
	parts := make([]string, 0, len(mappings))
	for _, m := range mappings {
		families := []string{}
		if m.IPv4 {
			families = append(families, "v4")
		}
		if m.IPv6 {
			families = append(families, "v6")
		}
		suffix := ""
		if len(families) > 0 {
			suffix = "(" + strings.Join(families, ",") + ")"
		}
		if m.HostPort == m.ContainerPort {
			parts = append(parts, fmt.Sprintf("%d/%s%s", m.HostPort, m.Protocol, suffix))
		} else {
			parts = append(parts, fmt.Sprintf("%d->%d/%s%s", m.HostPort, m.ContainerPort, m.Protocol, suffix))
		}
	}
	return strings.Join(parts, ", ")
}

func portFamilyLabel(m PortMapping) string {
	families := []string{}
	if m.IPv4 {
		families = append(families, "IPv4")
	}
	if m.IPv6 {
		families = append(families, "IPv6")
	}
	if len(families) == 0 {
		return ""
	}
	return " [" + strings.Join(families, "+") + "]"
}

// AddPortMapping 添加或替换一条端口映射。
//
// 实现说明：bocker 的 bridge 容器使用局域网直连网卡，Incus 守护进程不跟踪该模式的
// IP 地址（无 volatile.<nic>.last_state.ip_addresses），因此 nat=true 的 proxy 设备
// 无法工作。这里改用非 NAT 的用户态 proxy，并在 connect 中写入容器当前 IPv4。
// bocker 已经在宿主机侧创建了 bridge shim（bocker-br0），宿主机可以直达容器 IP。
//
// 容器运行时立即生效；容器未运行或未获取到 IPv4 时返回错误。
// 容器重启后若 IPv4 发生变化，CmdStart 会调用 RefreshPortMappings 自动刷新。
func (c *IncusClient) AddPortMapping(name string, hostPort, containerPort int, protocol string) error {
	if err := c.ready(); err != nil {
		return err
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return fmt.Errorf("获取容器 %q 失败: %w", name, err)
	}
	ct := convertContainer(full)
	ip := ct.IPv4()
	if ip == "" {
		return fmt.Errorf("容器 %s 未运行或未获取到 IPv4，请先启动容器后再添加端口映射", name)
	}
	put := writableInstance(full)
	if put.Devices == nil {
		put.Devices = api.DevicesMap{}
	}
	devName := portDeviceName(hostPort, protocol)
	put.Devices[devName] = map[string]string{
		"type":    "proxy",
		"listen":  fmt.Sprintf("%s:0.0.0.0:%d", protocol, hostPort),
		"connect": fmt.Sprintf("%s:%s:%d", protocol, ip, containerPort),
	}
	if ipv6 := ct.IPv6(); ipv6 != "" {
		// Incus' proxy implementation binds an IPv6 wildcard as a dual-stack
		// socket on normal Linux hosts. A second 0.0.0.0 listener would conflict,
		// so one [::] proxy serves both client families and connects over IPv6.
		put.Devices[devName] = map[string]string{
			"type":    "proxy",
			"listen":  fmt.Sprintf("%s:[::]:%d", protocol, hostPort),
			"connect": proxyEndpoint(protocol, ipv6, containerPort),
		}
	}
	delete(put.Devices, portDeviceNameForFamily(hostPort, protocol, "v6"))
	return c.updateInstance(name, etag, put)
}

// RefreshPortMappings 在容器启动后刷新所有 bocker 管理的端口映射的 connect 地址，
// 使其指向容器当前的 IPv4。用于应对 DHCP 重新分配导致 IP 变化的场景。
// 若容器无 IPv4 或没有端口映射，则不做任何操作。
func (c *IncusClient) RefreshPortMappings(name string) (int, error) {
	if err := c.ready(); err != nil {
		return 0, err
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return 0, fmt.Errorf("获取容器 %q 失败: %w", name, err)
	}
	ct := convertContainer(full)
	ipv4 := ct.IPv4()
	ipv6 := ct.IPv6()
	if ipv4 == "" && ipv6 == "" {
		return 0, nil
	}
	changed := false
	refreshed := 0
	for devName, dev := range full.Devices {
		hostPort, proto, family, ok := parsePortDeviceNameWithFamily(devName)
		if !ok || dev["type"] != "proxy" {
			continue
		}
		port := parseConnectPort(dev["connect"], proto)
		if port == 0 {
			continue
		}
		address := ipv4
		if ipv6 != "" && family == "v4" && strings.Contains(dev["listen"], ":0.0.0.0:") {
			dev["listen"] = fmt.Sprintf("%s:[::]:%d", proto, hostPort)
			address = ipv6
		} else if family == "v6" || strings.Contains(dev["listen"], ":[::]:") {
			address = ipv6
		}
		if address == "" {
			continue
		}
		newConnect := proxyEndpoint(proto, address, port)
		if dev["connect"] == newConnect {
			continue
		}
		dev["connect"] = newConnect
		full.Devices[devName] = dev
		changed = true
		refreshed++
	}
	if !changed {
		return 0, nil
	}
	put := writableInstance(full)
	if err := c.updateInstance(name, etag, put); err != nil {
		return 0, err
	}
	return refreshed, nil
}

// RemovePortMapping 移除一条端口映射。不存在时返回错误。
func (c *IncusClient) RemovePortMapping(name string, hostPort int, protocol string) error {
	if err := c.ready(); err != nil {
		return err
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return fmt.Errorf("获取容器 %q 失败: %w", name, err)
	}
	put := writableInstance(full)
	devName := portDeviceName(hostPort, protocol)
	if _, ok := put.Devices[devName]; !ok {
		return fmt.Errorf("未找到端口映射 %d/%s", hostPort, protocol)
	}
	delete(put.Devices, devName)
	delete(put.Devices, portDeviceNameForFamily(hostPort, protocol, "v6"))
	return c.updateInstance(name, etag, put)
}

// cmdSetPort 处理 bocker set <name> port ... 子命令。
//
// 用法：
//
//	bocker set <name> port                  交互式菜单
//	bocker set <name> port <spec>           添加/替换映射
//	bocker set <name> port rm <spec>        移除映射
//	bocker set <name> port list             列出映射
func cmdSetPort(client *IncusClient, ct *Container, args []string) error {
	name := ct.Name

	if len(args) >= 1 {
		flag := strings.ToLower(args[0])
		switch flag {
		case "--list", "-l", "list":
			return printPortMappings(client, name)
		case "--rm", "--unset", "rm", "unset", "remove", "del":
			if len(args) < 2 {
				return fmt.Errorf("用法: bocker set %s port rm <host_port[/proto]>", name)
			}
			hostPort, _, proto, err := parsePortSpec(args[1])
			if err != nil {
				return err
			}
			if err := client.RemovePortMapping(name, hostPort, proto); err != nil {
				return err
			}
			fmt.Printf("✔ 已移除端口映射 %d/%s\n", hostPort, proto)
			return nil
		}
		if !strings.HasPrefix(args[0], "-") {
			spec := args[0]
			hostPort, containerPort, proto, err := parsePortSpec(spec)
			if err != nil {
				return err
			}
			if err := client.AddPortMapping(name, hostPort, containerPort, proto); err != nil {
				return err
			}
			fmt.Printf("✔ 端口映射已添加: %d/%s -> %d/%s\n", hostPort, proto, containerPort, proto)
			return nil
		}
		return fmt.Errorf("未知参数: %s", args[0])
	}

	// 无额外参数 -> 交互式菜单
	mappings := ct.PortMappings()
	if len(mappings) > 0 {
		fmt.Println("当前端口映射:")
		for _, m := range mappings {
			if m.HostPort == m.ContainerPort {
				fmt.Printf("  %d/%s\n", m.HostPort, m.Protocol)
			} else {
				fmt.Printf("  %d/%s -> %d\n", m.HostPort, m.Protocol, m.ContainerPort)
			}
		}
	} else {
		fmt.Println("当前无端口映射。")
	}

	options := []string{"添加端口映射"}
	for _, m := range mappings {
		label := fmt.Sprintf("移除 %d/%s", m.HostPort, m.Protocol)
		if m.HostPort != m.ContainerPort {
			label = fmt.Sprintf("移除 %d/%s -> %d", m.HostPort, m.Protocol, m.ContainerPort)
		}
		options = append(options, label)
	}
	choice := selectMenu(options, "选择操作 (↑↓ 选择, Enter 确认, q 退出)")
	if choice < 0 {
		return nil
	}
	if choice == 0 {
		hostPort, containerPort, proto, ok := promptPortSpec()
		if !ok {
			return nil
		}
		if err := client.AddPortMapping(name, hostPort, containerPort, proto); err != nil {
			return err
		}
		fmt.Printf("✔ 端口映射已添加: %d/%s -> %d/%s\n", hostPort, proto, containerPort, proto)
		return nil
	}
	idx := choice - 1
	if idx < 0 || idx >= len(mappings) {
		return nil
	}
	m := mappings[idx]
	if err := client.RemovePortMapping(name, m.HostPort, m.Protocol); err != nil {
		return err
	}
	fmt.Printf("✔ 已移除端口映射 %d/%s\n", m.HostPort, m.Protocol)
	return nil
}

// printPortMappings 打印容器当前的端口映射列表。
func printPortMappings(client *IncusClient, name string) error {
	ct, err := client.GetContainer(name)
	if err != nil {
		return err
	}
	mappings := ct.PortMappings()
	if len(mappings) == 0 {
		fmt.Printf("容器 %s 当前无端口映射。\n", name)
		return nil
	}
	fmt.Printf("容器 %s 端口映射:\n", name)
	for _, m := range mappings {
		if m.HostPort == m.ContainerPort {
			fmt.Printf("  %d/%s%s\n", m.HostPort, m.Protocol, portFamilyLabel(m))
		} else {
			fmt.Printf("  %d/%s -> %d%s\n", m.HostPort, m.Protocol, m.ContainerPort, portFamilyLabel(m))
		}
	}
	return nil
}

// promptPortSpec 三段式引导输入端口映射规格。
// 返回 (hostPort, containerPort, protocol, ok)，ok=false 表示用户取消。
//
// 引导流程:
//
//  1. 宿主机端口 (必填, 1-65535, 输入错误可重试)
//  2. 容器端口   (回车默认 = 宿主机端口, 输入错误可重试)
//  3. 协议       (回车默认 tcp, 可选 tcp/udp)
func promptPortSpec() (hostPort, containerPort int, protocol string, ok bool) {
	fmt.Println()
	fmt.Println("── 端口映射 (三段式输入) ──")

	// 1. 宿主机端口 (最多重试 3 次，避免死循环)
	for attempt := 0; attempt < 3; attempt++ {
		hostStr := strings.TrimSpace(prompt("  宿主机端口 (1-65535): "))
		if hostStr == "" {
			return 0, 0, "", false // 用户主动取消
		}
		hp, err := strconv.Atoi(hostStr)
		if err != nil || hp < 1 || hp > 65535 {
			fmt.Printf("  ✘ 宿主机端口无效: %q (必须是 1-65535 的数字，请重试)\n", hostStr)
			continue
		}
		hostPort = hp
		break
	}
	if hostPort == 0 {
		fmt.Println("  ✘ 连续 3 次输入无效，已取消")
		return 0, 0, "", false
	}

	// 2. 容器端口 (回车默认 = 宿主机端口, 最多重试 3 次)
	for attempt := 0; attempt < 3; attempt++ {
		cpStr := strings.TrimSpace(prompt(fmt.Sprintf("  容器端口   (回车默认 %d): ", hostPort)))
		if cpStr == "" {
			containerPort = hostPort
			break
		}
		cp, err := strconv.Atoi(cpStr)
		if err != nil || cp < 1 || cp > 65535 {
			fmt.Printf("  ✘ 容器端口无效: %q (必须是 1-65535 的数字，请重试)\n", cpStr)
			continue
		}
		containerPort = cp
		break
	}
	if containerPort == 0 {
		fmt.Println("  ✘ 连续 3 次输入无效，已取消")
		return 0, 0, "", false
	}

	// 3. 协议 (回车默认 tcp)
	protoChoice := selectMenu([]string{"tcp", "udp"}, "  协议 (↑↓ 选择, Enter 确认)")
	if protoChoice < 0 {
		return 0, 0, "", false
	}
	protocol = []string{"tcp", "udp"}[protoChoice]

	ok = true
	return
}
