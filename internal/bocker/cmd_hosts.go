package bocker

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const hostsPath = "/etc/hosts"

var hostsUpdateMu sync.Mutex

// hostsMark 用于标记 bocker 管理的 /etc/hosts 行，便于按容器名更新/移除。
func hostsMark(name string) string {
	return "# bocker:" + name
}

// atomicWriteHosts 原子写入 /etc/hosts：先写临时文件再 rename，避免写入过程中
// 进程崩溃或系统断电导致 /etc/hosts 损坏（系统级关键文件，损坏会导致网络解析失效）。
func atomicWriteHosts(data []byte) error {
	return withHostsLock(func() error { return writeHostsLocked(data) })
}

func withHostsLock(update func() error) error {
	hostsUpdateMu.Lock()
	defer hostsUpdateMu.Unlock()

	lock, err := os.OpenFile(filepath.Join(filepath.Dir(hostsPath), ".hosts.bocker.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("创建 hosts 锁失败: %w", err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("锁定 hosts 文件失败: %w", err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN) //nolint:errcheck
	return update()
}

func writeHostsLocked(data []byte) error {
	info, err := os.Stat(hostsPath)
	if err != nil {
		return fmt.Errorf("读取 %s 元数据失败: %w", hostsPath, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(hostsPath), ".hosts.bocker.*")
	if err != nil {
		return fmt.Errorf("创建 hosts 临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	// 写入失败也要清理临时文件
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("同步临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Chmod(tmpPath, info.Mode().Perm()); err != nil {
		return fmt.Errorf("设置临时文件权限失败: %w", err)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if err := os.Chown(tmpPath, int(stat.Uid), int(stat.Gid)); err != nil {
			return fmt.Errorf("设置 hosts 临时文件所有者失败: %w", err)
		}
	}
	if err := os.Rename(tmpPath, hostsPath); err != nil {
		return fmt.Errorf("原子替换 %s 失败（原文件未修改）: %w", hostsPath, err)
	}
	if dir, err := os.Open(filepath.Dir(hostsPath)); err == nil {
		defer dir.Close()
		if err := dir.Sync(); err != nil {
			return fmt.Errorf("同步 hosts 目录失败: %w", err)
		}
	}
	return nil
}

// updateHosts 更新 /etc/hosts：将该容器域名的行更新为新 IP，不存在则追加。
// 行格式: "<ip> <domain>  # bocker:<容器名>"
// 幂等性：移除所有该 mark 的旧行后追加新行，避免重复构建产生多行重复条目。
func updateHosts(name, domain, ip string) error {
	if err := validateBockerName(name); err != nil {
		return fmt.Errorf("容器名称无效: %w", err)
	}
	if err := validateDomainName(domain); err != nil {
		return fmt.Errorf("域名无效: %w", err)
	}
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("IP 地址 %q 无效", ip)
	}
	mark := hostsMark(name)
	return withHostsLock(func() error {
		data, err := os.ReadFile(hostsPath)
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", hostsPath, err)
		}
		lines := strings.Split(string(data), "\n")

		// 移除所有匹配 mark 的旧行（防止历史重复条目），再追加唯一的新行
		out := make([]string, 0, len(lines)+1)
		for _, line := range lines {
			if !strings.HasSuffix(strings.TrimSpace(line), mark) {
				out = append(out, line)
			}
		}
		out = append(out, fmt.Sprintf("%s\t%s\t%s", ip, domain, mark))
		return writeHostsLocked([]byte(strings.Join(out, "\n")))
	})
}

// updateHostsAddresses writes one marked hosts entry per global address so a
// Bocker domain resolves through both IPv4 and IPv6.
func updateHostsAddresses(name, domain string, ips []string) error {
	if err := validateBockerName(name); err != nil {
		return fmt.Errorf("invalid container name: %w", err)
	}
	if err := validateDomainName(domain); err != nil {
		return fmt.Errorf("invalid domain name: %w", err)
	}

	validIPs := make([]string, 0, len(ips))
	seen := map[string]bool{}
	for _, ip := range ips {
		ip = strings.TrimSpace(ip)
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("invalid IP address %q", ip)
		}
		if !seen[ip] {
			seen[ip] = true
			validIPs = append(validIPs, ip)
		}
	}
	if len(validIPs) == 0 {
		return fmt.Errorf("at least one IP address is required")
	}

	mark := hostsMark(name)
	return withHostsLock(func() error {
		data, err := os.ReadFile(hostsPath)
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", hostsPath, err)
		}
		lines := strings.Split(string(data), "\n")
		out := make([]string, 0, len(lines)+len(validIPs))
		for _, line := range lines {
			if !strings.HasSuffix(strings.TrimSpace(line), mark) {
				out = append(out, line)
			}
		}
		for _, ip := range validIPs {
			out = append(out, fmt.Sprintf("%s\t%s\t%s", ip, domain, mark))
		}
		return writeHostsLocked([]byte(strings.Join(out, "\n")))
	})
}

// removeHostsLine 从 /etc/hosts 移除该容器的映射行。
func removeHostsLine(name string) error {
	if err := validateBockerName(name); err != nil {
		return fmt.Errorf("容器名称无效: %w", err)
	}
	mark := hostsMark(name)
	return withHostsLock(func() error {
		data, err := os.ReadFile(hostsPath)
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", hostsPath, err)
		}
		lines := strings.Split(string(data), "\n")
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			if !strings.HasSuffix(strings.TrimSpace(line), mark) {
				out = append(out, line)
			}
		}
		return writeHostsLocked([]byte(strings.Join(out, "\n")))
	})
}

// waitForIP 轮询容器 IPv4，最多等待 maxWait 秒。
func waitForIP(client *IncusClient, name string, maxWait int) string {
	for i := 0; i < maxWait; i++ {
		ct, err := client.GetContainer(name)
		if err == nil && ct.IPv4() != "" {
			return ct.IPv4()
		}
		time.Sleep(time.Second)
	}
	return ""
}

// waitForIPAddresses waits for IPv4 and, when available, IPv6. IPv6 router
// advertisements commonly arrive shortly after DHCPv4, so callers that write
// a dual-stack hosts entry use this instead of capturing only the first IPv4.
func waitForIPAddresses(client *IncusClient, name string, maxWait int, requireIPv6 bool) []string {
	var last []string
	for i := 0; i < maxWait; i++ {
		ct, err := client.GetContainer(name)
		if err == nil && ct.IPv4() != "" {
			last = ct.IPAddresses()
			if !requireIPv6 || ct.IPv6() != "" {
				return last
			}
		}
		time.Sleep(time.Second)
	}
	return last
}
