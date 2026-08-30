package bocker

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lxc/incus/v7/shared/api"
)

const (
	mountDevicePrefix      = "mount-"
	mountDeviceNameMaxLen  = 64
	mountDeviceHashByteLen = 6 // 48 bits keeps names short while making collisions negligible.
)

type Mount struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Readonly  bool   `json:"readonly"`
	Inherited bool   `json:"inherited"`
}

func mountDeviceName(source, target string) string {
	// Incus device names are limited to 64 characters. Keep a readable target
	// basename, then add a deterministic source/target hash so names remain
	// stable across repeated apply operations and cannot collide after truncation.
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	base := filepath.Base(target)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "root"
	}
	base = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, base)
	if base == "" {
		base = "mount"
	}
	digest := sha256.Sum256([]byte(source + "\x00" + target))
	hash := fmt.Sprintf("%x", digest[:mountDeviceHashByteLen])
	maxBaseLen := mountDeviceNameMaxLen - len(mountDevicePrefix) - 1 - len(hash)
	if len(base) > maxBaseLen {
		base = base[:maxBaseLen]
	}
	return fmt.Sprintf("%s%s-%s", mountDevicePrefix, base, hash)
}

func validateMountPaths(source, target string) (string, string, error) {
	source = strings.TrimSpace(source)
	target = strings.TrimSpace(target)
	if source == "" || target == "" {
		return "", "", fmt.Errorf("挂载源路径和容器目标路径不能为空")
	}
	if strings.ContainsRune(source, '\x00') || strings.ContainsRune(target, '\x00') {
		return "", "", fmt.Errorf("挂载路径不能包含 NUL 字符")
	}
	if !filepath.IsAbs(source) || !filepath.IsAbs(target) {
		return "", "", fmt.Errorf("挂载路径必须是绝对路径")
	}
	var err error
	source, err = filepath.Abs(filepath.Clean(source))
	if err != nil {
		return "", "", fmt.Errorf("解析挂载源路径失败: %w", err)
	}
	target = filepath.Clean(target)
	if target == string(filepath.Separator) {
		return "", "", fmt.Errorf("容器目标路径不能是根目录")
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", "", fmt.Errorf("挂载源不存在或不可访问: %s: %w", source, err)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("挂载源必须是普通文件或目录: %s", source)
	}
	return source, target, nil
}

func normalizedMountPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func mountCreateType(info os.FileInfo) string {
	if info.IsDir() {
		return "dir"
	}
	return "file"
}

func mountReadonly(dev map[string]string) bool {
	switch strings.ToLower(strings.TrimSpace(dev["readonly"])) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func mountDeviceMaps(full *api.InstanceFull) (api.DevicesMap, bool) {
	if full == nil {
		return nil, false
	}
	// ExpandedDevices contains profile devices as well as local overrides. Some
	// older API responses omit it, so fall back to the local map in that case.
	devices := full.ExpandedDevices
	fromExpanded := true
	if devices == nil || (len(devices) == 0 && len(full.Devices) > 0) {
		devices = full.Devices
		fromExpanded = false
	}
	return devices, fromExpanded
}

func mountDeviceInherited(full *api.InstanceFull, devName string, fromExpanded bool) bool {
	if !fromExpanded {
		return false
	}
	_, local := full.Devices[devName]
	return !local
}

func mountStatusLabel(full *api.InstanceFull) string {
	if status := strings.TrimSpace(full.Status); status != "" {
		return status
	}
	if status := full.StatusCode.String(); status != "" {
		return status
	}
	return fmt.Sprintf("状态码 %d", full.StatusCode)
}

func requireMountStopped(name string, full *api.InstanceFull, action string) error {
	if full == nil {
		return fmt.Errorf("容器 %s 状态未知，无法%s挂载", name, action)
	}
	if full.StatusCode == api.Stopped {
		return nil
	}
	return fmt.Errorf("容器 %s 当前状态为 %s，请先停止后再%s挂载", name, mountStatusLabel(full), action)
}

func (c *IncusClient) ListMounts(name string) ([]Mount, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	full, _, err := c.server.GetInstanceFull(name)
	if err != nil {
		return nil, err
	}
	devices, fromExpanded := mountDeviceMaps(full)
	result := make([]Mount, 0)
	for devName, dev := range devices {
		if !strings.HasPrefix(devName, mountDevicePrefix) || dev["type"] != "disk" {
			continue
		}
		result = append(result, Mount{
			Name:      devName,
			Source:    normalizedMountPath(dev["source"]),
			Target:    normalizedMountPath(dev["path"]),
			Readonly:  mountReadonly(dev),
			Inherited: mountDeviceInherited(full, devName, fromExpanded),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Target != result[j].Target {
			return result[i].Target < result[j].Target
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (c *IncusClient) AddMount(name, source, target string, readonly bool) error {
	if err := c.ready(); err != nil {
		return err
	}
	source, target, err := validateMountPaths(source, target)
	if err != nil {
		return err
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	if err := requireMountStopped(name, full, "添加"); err != nil {
		return err
	}
	put := writableInstance(full)
	if put.Devices == nil {
		put.Devices = api.DevicesMap{}
	}
	devices, fromExpanded := mountDeviceMaps(full)
	for devName, dev := range devices {
		if dev["type"] != "disk" || normalizedMountPath(dev["path"]) != target {
			continue
		}
		if normalizedMountPath(dev["source"]) != source || !strings.HasPrefix(devName, mountDevicePrefix) {
			return fmt.Errorf("容器目标路径已被其他磁盘设备占用: %s", target)
		}
		if mountDeviceInherited(full, devName, fromExpanded) {
			if mountReadonly(dev) == readonly {
				return fmt.Errorf("该挂载已存在 (来自 profile: %s)", devName)
			}
			return fmt.Errorf("挂载 %s 来自 profile，不能通过容器修改模式", devName)
		}
		localDev, ok := put.Devices[devName]
		if !ok {
			return fmt.Errorf("挂载设备 %s 不存在于容器本地配置", devName)
		}
		if mountReadonly(localDev) == readonly {
			return fmt.Errorf("该挂载已存在")
		}
		localDev["readonly"] = fmt.Sprintf("%t", readonly)
		put.Devices[devName] = localDev
		return c.updateInstance(name, etag, put)
	}
	put.Devices[mountDeviceName(source, target)] = map[string]string{
		"type":     "disk",
		"source":   source,
		"path":     target,
		"readonly": fmt.Sprintf("%t", readonly),
	}
	return c.updateInstance(name, etag, put)
}

func (c *IncusClient) RemoveMount(name, mountName string) error {
	if err := c.ready(); err != nil {
		return err
	}
	if !strings.HasPrefix(mountName, mountDevicePrefix) {
		return fmt.Errorf("无效挂载名称: %s", mountName)
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	if err := requireMountStopped(name, full, "删除"); err != nil {
		return err
	}
	put := writableInstance(full)
	dev, ok := put.Devices[mountName]
	if !ok {
		if expanded, fromExpanded := mountDeviceMaps(full); expanded != nil {
			if profileDev, found := expanded[mountName]; found && fromExpanded && mountDeviceInherited(full, mountName, fromExpanded) && profileDev["type"] == "disk" {
				return fmt.Errorf("挂载 %s 来自 profile，不能通过容器删除", mountName)
			}
		}
		return fmt.Errorf("挂载不存在: %s", mountName)
	}
	if dev["type"] != "disk" {
		return fmt.Errorf("挂载不存在: %s", mountName)
	}
	delete(put.Devices, mountName)
	return c.updateInstance(name, etag, put)
}

// UpdateMount changes the read-only mode of an existing instance-local mount.
// Profile-provided mounts are intentionally immutable through the container API.
func (c *IncusClient) UpdateMount(name, mountName string, readonly bool) error {
	if err := c.ready(); err != nil {
		return err
	}
	if !strings.HasPrefix(mountName, mountDevicePrefix) {
		return fmt.Errorf("无效挂载名称: %s", mountName)
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	if err := requireMountStopped(name, full, "更新"); err != nil {
		return err
	}
	put := writableInstance(full)
	dev, ok := put.Devices[mountName]
	if !ok {
		if expanded, fromExpanded := mountDeviceMaps(full); expanded != nil {
			if profileDev, found := expanded[mountName]; found && fromExpanded && mountDeviceInherited(full, mountName, fromExpanded) && profileDev["type"] == "disk" {
				return fmt.Errorf("挂载 %s 来自 profile，不能通过容器修改模式", mountName)
			}
		}
		return fmt.Errorf("挂载不存在: %s", mountName)
	}
	if dev["type"] != "disk" {
		return fmt.Errorf("挂载不存在: %s", mountName)
	}
	if mountReadonly(dev) == readonly {
		return nil
	}
	dev["readonly"] = fmt.Sprintf("%t", readonly)
	put.Devices[mountName] = dev
	return c.updateInstance(name, etag, put)
}
