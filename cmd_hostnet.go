package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

const (
	defaultHostBridgeName = "bocker-br0"
	hostShimCIDREnv       = "BOCKER_HOST_SHIM_CIDR"
)

// AutoConfigureHostBridge 配置 bridge 模式的宿主机 shim，让宿主机可以访问
// 当前已运行的 bridge 容器。该函数是幂等的：重复调用只会刷新地址和路由。
//
// 背景：Linux macvlan 默认隔离 parent 物理网卡与其 macvlan 子接口，容器能访问
// 路由器/局域网其他机器，但宿主机与容器之间不会经由物理网卡“折返”。解决办法是在
// 宿主机上也创建一个 macvlan 子接口，并把容器 IP 路由到该子接口。
func AutoConfigureHostBridge(client *IncusClient) error {
	targets := runningBridgeContainers(client)
	if len(targets) == 0 {
		return nil
	}
	return ensureHostBridgeConnectivity(client, targets)
}

// removeHostBridgeRoute 移除宿主机侧 bocker-br0 上指向指定 IP 的 /32 路由。
// 容器停止/删除时调用，避免死路由堆积。
func removeHostBridgeRoute(ip string) {
	if ip == "" {
		return
	}
	// 静默执行，路由不存在不算错误
	_ = exec.Command("ip", "route", "del", ip+"/32", "dev", defaultHostBridgeName).Run()
}

func warnAutoHostBridge(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ 自动配置宿主机 bridge 互通失败: %v\n", err)
	}
}

type bridgeRouteTarget struct {
	Name  string
	IP    net.IP
	MAC   string
	Route string
}

func ensureHostBridgeConnectivity(client *IncusClient, targets []bridgeRouteTarget) error {
	if err := ensureCommand("ip"); err != nil {
		return err
	}

	parent, err := detectBridgeParent()
	if err != nil {
		return err
	}

	reservedIPs := make([]net.IP, 0, len(targets))
	for i := range targets {
		if targets[i].Route == "" {
			if targets[i].IP == nil || targets[i].IP.To4() == nil {
				return fmt.Errorf("路由目标 %q 没有可用 IPv4", targets[i].Name)
			}
			targets[i].Route = targets[i].IP.String() + "/32"
		}
		normalized, err := normalizeRouteTarget(targets[i].Route)
		if err != nil {
			return fmt.Errorf("路由目标 %q 无效: %w", targets[i].Route, err)
		}
		targets[i].Route = normalized
		if targets[i].IP == nil {
			targets[i].IP = routeTargetIPv4(normalized)
		}
		if targets[i].IP != nil {
			reservedIPs = append(reservedIPs, targets[i].IP)
		}
	}

	shimCIDR, err := autoHostShimCIDR(parent, reservedIPs)
	if err != nil {
		return err
	}
	shimIP, _, err := net.ParseCIDR(shimCIDR)
	if err != nil || shimIP == nil || shimIP.To4() == nil {
		return fmt.Errorf("shim 地址 %q 无效", shimCIDR)
	}
	shimIP = shimIP.To4()

	if err := ensureHostBridge(parent, defaultHostBridgeName); err != nil {
		return err
	}
	if err := configureHostBridgeIsolation(parent, defaultHostBridgeName); err != nil {
		return err
	}
	if err := replaceAddr(defaultHostBridgeName, shimCIDR); err != nil {
		return err
	}
	if err := linkUp(defaultHostBridgeName); err != nil {
		return err
	}

	hostIP, _, _ := firstGlobalIPv4CIDR(parent)
	hostIPStr := ""
	if hostIP != nil {
		hostIPStr = hostIP.String()
	}
	shimMAC, _ := linkMAC(defaultHostBridgeName)

	for _, target := range targets {
		if err := replaceRoute(target.Route, defaultHostBridgeName, shimIP.String()); err != nil {
			return err
		}
		if target.IP != nil && strings.TrimSpace(target.MAC) != "" {
			if err := replaceStaticARP(target.IP.String(), target.MAC, defaultHostBridgeName); err != nil {
				return err
			}
		}
		if client != nil && target.Name != "" && shimMAC != "" {
			_ = replaceContainerStaticARP(client, target.Name, hostIPStr, shimMAC, defaultNICName)
			_ = replaceContainerStaticARP(client, target.Name, shimIP.String(), shimMAC, defaultNICName)
		}
	}
	return nil
}

func autoHostShimCIDR(parent string, reserved []net.IP) (string, error) {
	if cidr := strings.TrimSpace(os.Getenv(hostShimCIDREnv)); cidr != "" {
		if err := validateHostShimCIDR(cidr); err != nil {
			return "", fmt.Errorf("%s=%q 无效: %w", hostShimCIDREnv, cidr, err)
		}
		if ipConflictsWithReservedCIDR(cidr, reserved) {
			return "", fmt.Errorf("%s=%q 与容器 IP 冲突", hostShimCIDREnv, cidr)
		}
		return cidr, nil
	}

	// 复用已存在的 shim 地址：shim 接口一旦配置好 /32 地址就长期有效，
	// 重复执行 ping 探测 (每次约 1s) 是命令延迟的主要来源。
	// 仅在 shim 尚无地址或地址与容器 IP 冲突时才重新选择。
	if existing := existingShimCIDR(); existing != "" {
		if !ipConflictsWithReservedCIDR(existing, reserved) {
			return existing, nil
		}
	}

	hostIP, ipNet, err := firstGlobalIPv4CIDR(parent)
	if err != nil {
		return "", err
	}

	gateway := defaultGatewayFor(parent)
	candidate, err := pickHostShimIP(parent, hostIP, gateway, ipNet, reserved)
	if err != nil {
		return "", err
	}
	return candidate.String() + "/32", nil
}

// existingShimCIDR 返回 bocker-br0 接口上已配置的全局 /32 IPv4 CIDR。
// 不存在或非 /32 时返回空串，触发重新选择。
func existingShimCIDR() string {
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "dev", defaultHostBridgeName, "scope", "global").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "inet" || i+1 >= len(fields) {
				continue
			}
			cidr := fields[i+1]
			ip, ipNet, err := net.ParseCIDR(cidr)
			if err != nil || ip == nil || ip.To4() == nil {
				continue
			}
			ones, bits := ipNet.Mask.Size()
			if bits != 32 || ones != 32 {
				continue
			}
			return cidr
		}
	}
	return ""
}

func firstGlobalIPv4CIDR(parent string) (net.IP, *net.IPNet, error) {
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "dev", parent, "scope", "global").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("读取父网卡 %s IPv4 地址失败: %w", parent, err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "inet" || i+1 >= len(fields) {
				continue
			}
			ip, ipNet, err := net.ParseCIDR(fields[i+1])
			if err != nil || ip == nil || ip.To4() == nil {
				continue
			}
			return ip.To4(), ipNet, nil
		}
	}
	return nil, nil, fmt.Errorf("父网卡 %s 没有可用的全局 IPv4 地址", parent)
}

func defaultGatewayFor(parent string) net.IP {
	out, err := exec.Command("ip", "-4", "route", "show", "default", "dev", parent).Output()
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(out))
	for i, field := range fields {
		if field == "via" && i+1 < len(fields) {
			if ip := net.ParseIP(fields[i+1]); ip != nil {
				return ip.To4()
			}
		}
	}
	return nil
}

func pickHostShimIP(parent string, hostIP, gateway net.IP, ipNet *net.IPNet, reserved []net.IP) (net.IP, error) {
	ones, bits := ipNet.Mask.Size()
	if bits != 32 || ones < 24 {
		return nil, fmt.Errorf("无法自动为 %s/%d 安全选择 shim IP；请设置 %s，例如 %s=192.168.3.254/32", ipNet.IP, ones, hostShimCIDREnv, hostShimCIDREnv)
	}

	network := ipv4ToUint32(ipNet.IP)
	mask := ipv4ToUint32(net.IP(ipNet.Mask))
	broadcast := network | ^mask

	host := ipv4ToUint32(hostIP)
	gw := uint32(0)
	if gateway != nil {
		gw = ipv4ToUint32(gateway)
	}

	// 从网段末尾向前选择，常见家庭/办公网络一般网关在 .1，宿主机在中间地址。
	for n := broadcast - 1; n > network; n-- {
		if n == host || (gw != 0 && n == gw) || containsIPv4Uint32(reserved, n) {
			continue
		}
		ip := uint32ToIPv4(n)
		if ipv4ProbablyUsed(parent, ip) {
			continue
		}
		return ip, nil
	}

	return nil, fmt.Errorf("无法在 %s 中自动选择未占用的 shim IP；请设置 %s", ipNet.String(), hostShimCIDREnv)
}

func routeTargetIPv4(target string) net.IP {
	ip, ipNet, err := net.ParseCIDR(target)
	if err != nil || ip == nil || ip.To4() == nil {
		return nil
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 || ones != 32 {
		return nil
	}
	return ip.To4()
}

func containsIPv4Uint32(ips []net.IP, n uint32) bool {
	for _, ip := range ips {
		if ip == nil || ip.To4() == nil {
			continue
		}
		if ipv4ToUint32(ip) == n {
			return true
		}
	}
	return false
}

func ipConflictsWithReservedCIDR(cidr string, reserved []net.IP) bool {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil || ip == nil || ip.To4() == nil {
		return false
	}
	return containsIPv4Uint32(reserved, ipv4ToUint32(ip))
}

func ipv4ProbablyUsed(parent string, ip net.IP) bool {
	if _, err := exec.LookPath("ping"); err != nil {
		return false
	}

	ipStr := ip.String()
	_ = exec.Command("ip", "neigh", "flush", ipStr, "dev", parent).Run()
	_ = exec.Command("ping", "-4", "-c", "1", "-W", "1", "-I", parent, ipStr).Run()

	out, err := exec.Command("ip", "neigh", "show", ipStr, "dev", parent).Output()
	if err != nil {
		return false
	}
	s := string(out)
	return strings.Contains(s, "lladdr") &&
		!strings.Contains(s, "FAILED") &&
		!strings.Contains(s, "INCOMPLETE")
}

func ipv4ToUint32(ip net.IP) uint32 {
	return binary.BigEndian.Uint32(ip.To4())
}

func uint32ToIPv4(n uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip
}

func ensureCommand(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("找不到 %s 命令: %w", name, err)
	}
	return nil
}

func ensureHostBridge(parent, ifname string) error {
	if parent == "" {
		return fmt.Errorf("父网卡不能为空")
	}
	if ifname == "" {
		return fmt.Errorf("shim 接口名不能为空")
	}

	if err := exec.Command("ip", "link", "show", parent).Run(); err != nil {
		return fmt.Errorf("父网卡 %s 不存在或不可用: %w", parent, err)
	}

	if err := exec.Command("ip", "link", "show", ifname).Run(); err == nil {
		if existingParent, ok := linkParent(ifname); ok && existingParent != parent {
			_ = exec.Command("ip", "link", "del", ifname).Run()
		} else {
			return nil
		}
	}

	cmd := exec.Command("ip", "link", "add", ifname, "link", parent, "type", "macvlan", "mode", "bridge")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("创建 bridge shim 失败: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// linkParent 返回类似 "bocker-br0@ens18" 中的父接口名 "ens18"。
func linkParent(ifname string) (string, bool) {
	out, err := exec.Command("ip", "-o", "link", "show", ifname).Output()
	if err != nil {
		return "", false
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return "", false
	}
	name := strings.TrimSuffix(fields[1], ":")
	idx := strings.LastIndex(name, "@")
	if idx < 0 || idx+1 >= len(name) {
		return "", false
	}
	return name[idx+1:], true
}

// configureHostBridgeIsolation 避免宿主机物理网卡和 macvlan shim 在同一二层
// 网络内互相替对方的 IPv4 地址响应 ARP。否则路由器可能会看到同一个宿主机 IP
// 同时对应物理网卡 MAC 和 bocker-br0 的虚拟 MAC（ARP flux）。
//
// 该函数只调整运行时 sysctl，不会修改物理网卡 MAC，也不会修改物理网卡 IPv4。
func configureHostBridgeIsolation(parent, ifname string) error {
	settings := []struct {
		path  string
		value string
		desc  string
	}{
		{"/proc/sys/net/ipv4/conf/" + parent + "/arp_ignore", "1", parent + " arp_ignore"},
		{"/proc/sys/net/ipv4/conf/" + parent + "/arp_announce", "2", parent + " arp_announce"},
		{"/proc/sys/net/ipv4/conf/" + ifname + "/arp_ignore", "1", ifname + " arp_ignore"},
		{"/proc/sys/net/ipv4/conf/" + ifname + "/arp_announce", "2", ifname + " arp_announce"},
		// shim 只承担 IPv4 /32 路由用途，关闭它的 IPv6 RA/自动地址可避免路由器
		// 把 bocker-br0 当成一台额外 IPv6 终端展示。
		{"/proc/sys/net/ipv6/conf/" + ifname + "/accept_ra", "0", ifname + " accept_ra"},
		{"/proc/sys/net/ipv6/conf/" + ifname + "/autoconf", "0", ifname + " autoconf"},
		{"/proc/sys/net/ipv6/conf/" + ifname + "/disable_ipv6", "1", ifname + " disable_ipv6"},
	}

	for _, setting := range settings {
		if err := writeProcSysIfExists(setting.path, setting.value); err != nil {
			return fmt.Errorf("配置 %s 失败: %w", setting.desc, err)
		}
	}

	// 如果 shim 之前已经通过 RA 获得了 IPv6 地址，尽量清掉；失败不影响 IPv4 互通。
	_ = exec.Command("ip", "-6", "addr", "flush", "dev", ifname).Run()
	return nil
}

func writeProcSysIfExists(path, value string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.WriteFile(path, []byte(value+"\n"), 0o644)
}

func replaceAddr(ifname, cidr string) error {
	cmd := exec.Command("ip", "addr", "replace", cidr, "dev", ifname)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("配置 shim 地址失败: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func linkUp(ifname string) error {
	cmd := exec.Command("ip", "link", "set", ifname, "up")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("启用 shim 接口失败: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func linkMAC(ifname string) (string, error) {
	data, err := os.ReadFile("/sys/class/net/" + ifname + "/address")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func replaceRoute(target, ifname, src string) error {
	args := []string{"route", "replace", target, "dev", ifname}
	if src != "" {
		args = append(args, "src", src)
	}
	cmd := exec.Command("ip", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("配置路由失败: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func replaceStaticARP(ip, mac, ifname string) error {
	if strings.TrimSpace(ip) == "" || strings.TrimSpace(mac) == "" {
		return nil
	}
	cmd := exec.Command("ip", "neigh", "replace", ip, "lladdr", mac, "nud", "permanent", "dev", ifname)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("配置静态 ARP 失败: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func replaceContainerStaticARP(client *IncusClient, name, ip, mac, nic string) error {
	if strings.TrimSpace(ip) == "" || strings.TrimSpace(mac) == "" {
		return nil
	}
	_, err := client.execQuiet(name, "ip", "neigh", "replace", ip, "lladdr", mac, "nud", "permanent", "dev", nic)
	return err
}

func runningBridgeContainers(client *IncusClient) []bridgeRouteTarget {
	cs, err := client.ListContainers()
	if err != nil {
		return nil
	}

	seen := map[string]bool{}
	targets := []bridgeRouteTarget{}
	for _, ct := range cs {
		if !strings.EqualFold(ct.Status, "Running") {
			continue
		}
		ip := ct.IPv4()
		if ip == "" || seen[ip] {
			continue
		}
		if !ct.UsesBridgeNIC(defaultNICName) {
			continue
		}
		seen[ip] = true
		targets = append(targets, bridgeRouteTarget{
			Name:  ct.Name,
			IP:    net.ParseIP(ip).To4(),
			MAC:   ct.NICMAC(defaultNICName),
			Route: ip + "/32",
		})
	}
	return targets
}

func validateCIDR(cidr string) error {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}
	if ip == nil || ip.To4() == nil {
		return errors.New("仅支持 IPv4 CIDR")
	}
	return nil
}

func validateHostShimCIDR(cidr string) error {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}
	if ip == nil || ip.To4() == nil {
		return errors.New("仅支持 IPv4 CIDR")
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 || ones != 32 {
		return errors.New("shim 地址必须使用 /32，避免宿主机整段局域网路由被改走 shim")
	}
	return nil
}

func normalizeRouteTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("不能为空")
	}

	if strings.Contains(target, "/") {
		if err := validateCIDR(target); err != nil {
			return "", err
		}
		return target, nil
	}

	ip := net.ParseIP(target)
	if ip == nil || ip.To4() == nil {
		return "", errors.New("仅支持 IPv4 地址或 IPv4 CIDR")
	}
	return target + "/32", nil
}
