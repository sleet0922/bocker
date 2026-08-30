package bocker

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lxc/incus/v7/shared/api"
)

const mountDevicePrefix = "mount-"

type Mount struct {
	Name     string
	Source   string
	Target   string
	Readonly bool
}

func mountDeviceName(source, target string) string {
	// A stable, collision-resistant name keeps repeated add operations idempotent.
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
	return fmt.Sprintf("%s%s-%x", mountDevicePrefix, base, time.Now().UnixNano())
}

func validateMountPaths(source, target string) (string, string, error) {
	source = strings.TrimSpace(source)
	target = strings.TrimSpace(target)
	if source == "" || target == "" {
		return "", "", fmt.Errorf("挂载源路径和容器目标路径不能为空")
	}
	if !filepath.IsAbs(source) || !filepath.IsAbs(target) {
		return "", "", fmt.Errorf("挂载路径必须是绝对路径")
	}
	source, err := filepath.Abs(filepath.Clean(source))
	if err != nil {
		return "", "", fmt.Errorf("解析挂载源路径失败: %w", err)
	}
	if _, err := os.Stat(source); err != nil {
		return "", "", fmt.Errorf("挂载源不存在或不可访问: %s: %w", source, err)
	}
	return source, filepath.Clean(target), nil
}

func (c *IncusClient) ListMounts(name string) ([]Mount, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	full, _, err := c.server.GetInstanceFull(name)
	if err != nil {
		return nil, err
	}
	result := make([]Mount, 0)
	for devName, dev := range full.Devices {
		if !strings.HasPrefix(devName, mountDevicePrefix) || dev["type"] != "disk" {
			continue
		}
		result = append(result, Mount{Name: devName, Source: dev["source"], Target: dev["path"], Readonly: dev["readonly"] == "true"})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
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
	if strings.EqualFold(full.Status, "Running") {
		return fmt.Errorf("容器 %s 正在运行，请先停止后再添加挂载", name)
	}
	put := writableInstance(full)
	if put.Devices == nil {
		put.Devices = api.DevicesMap{}
	}
	for _, dev := range put.Devices {
		if dev["type"] == "disk" && dev["source"] == source && dev["path"] == target {
			return fmt.Errorf("该挂载已存在")
		}
	}
	put.Devices[mountDeviceName(source, target)] = map[string]string{"type": "disk", "source": source, "path": target, "readonly": fmt.Sprintf("%t", readonly)}
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
	if strings.EqualFold(full.Status, "Running") {
		return fmt.Errorf("容器 %s 正在运行，请先停止后再删除挂载", name)
	}
	put := writableInstance(full)
	dev, ok := put.Devices[mountName]
	if !ok || dev["type"] != "disk" {
		return fmt.Errorf("挂载不存在: %s", mountName)
	}
	delete(put.Devices, mountName)
	return c.updateInstance(name, etag, put)
}
