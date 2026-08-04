package main

import (
	"fmt"
	"strings"
)

// CmdInstall 两级菜单：先选发行版，再选具体版本，最后安装。
// 若传入参数：bocker install [镜像名/引用] [容器名] 则跳过菜单直接安装。
func CmdInstall(args []string) error {
	permissionOverride := hasPermissionOverride(args)
	permissionMode, args, err := permissionModeFromArgs(args)
	if err != nil {
		return err
	}
	networkOverride := hasNetworkOverride(args)
	mode, args, err := networkModeFromArgs(args)
	if err != nil {
		return err
	}
	client := NewIncusClient()
	if len(args) > 2 {
		return fmt.Errorf("install 只接受镜像引用和可选容器名")
	}

	if len(args) >= 1 {
		imageRef := args[0]
		// 规范化镜像引用：debian:12 -> debian/12，统一内部处理
		imageRef = normalizeImageRef(imageRef)
		name := ""
		if len(args) >= 2 {
			name = args[1]
		} else {
			name = defaultNameFromImage(args[0])
		}
		if err := validateBockerName(name); err != nil {
			return fmt.Errorf("容器名称 %q 无效: %w", name, err)
		}
		fmt.Printf("正在安装 %s (名称: %s, 网络: %s, 权限: %s) ...\n", imageRef, name, mode, permissionMode)
		if err := client.LaunchWithNetworkAndPermission(imageRef, name, mode, permissionMode); err != nil {
			return err
		}
		ip := waitForIP(client, name, 15)
		if ip != "" {
			warnAutoHostBridge(AutoConfigureHostBridge(client))
		}
		fmt.Printf("✔ 容器 %s 已安装并启动!\n", name)
		return nil
	}

	if !networkOverride {
		selectedMode, ok := selectNetworkMode(mode)
		if !ok {
			return nil
		}
		mode = selectedMode
	}
	if len(args) == 0 && !permissionOverride {
		selectedPermission, ok := selectPermissionMode(permissionMode)
		if !ok {
			return nil
		}
		permissionMode = selectedPermission
	}

	fmt.Println("正在从镜像源获取可用发行版列表 ...")
	groups, err := client.ListImages()
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return fmt.Errorf("未找到可用镜像")
	}

	// 一级菜单：发行版
	distroNames := make([]string, len(groups))
	for i, g := range groups {
		distroNames[i] = fmt.Sprintf("%s (%d)", g.Distro, len(g.Versions))
	}
	dChoice := selectMenu(distroNames, "选择发行版 (↑↓ 选择, Enter 确认, q 退出)")
	if dChoice < 0 {
		return nil
	}
	group := groups[dChoice]

	// 二级菜单：具体版本
	relNames := make([]string, len(group.Versions))
	for i, v := range group.Versions {
		relNames[i] = v.Release
	}
	vChoice := selectMenu(relNames, fmt.Sprintf("%s - 选择版本 (↑↓ 选择, Enter 确认, q 退出)", group.Distro))
	if vChoice < 0 {
		return nil
	}
	version := group.Versions[vChoice]

	// 容器名
	defaultName := defaultNameFromImage(version.Image)
	name := prompt(fmt.Sprintf("容器名称 (回车默认 %s): ", defaultName))
	if name == "" {
		name = defaultName
	}
	if err := validateBockerName(name); err != nil {
		return fmt.Errorf("容器名称 %q 无效: %w", name, err)
	}

	// version.Image 已是 alias 形式（如 debian/12），Launch 内部会处理
	imageRef := version.Image
	fmt.Printf("\n正在安装 %s %s (%s, 网络: %s) ...\n", group.Distro, version.Release, imageRef, mode)
	if err := client.LaunchWithNetworkAndPermission(imageRef, name, mode, permissionMode); err != nil {
		return err
	}

	ip := waitForIP(client, name, 15)
	if ip != "" {
		warnAutoHostBridge(AutoConfigureHostBridge(client))
	}

	fmt.Printf("✔ 容器 %s 已安装并启动!\n", name)
	return nil
}

// defaultNameFromImage 由镜像引用生成合法容器名。
// debian/bookworm -> debian-bookworm
// debian:12 -> debian-12
func defaultNameFromImage(image string) string {
	var out strings.Builder
	lastHyphen := false
	for _, c := range strings.ToLower(image) {
		valid := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if valid {
			out.WriteRune(c)
			lastHyphen = false
		} else if !lastHyphen && out.Len() > 0 {
			out.WriteByte('-')
			lastHyphen = true
		}
	}
	name := strings.Trim(out.String(), "-")
	if name == "" {
		name = "container"
	}
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	allDigits := true
	for _, c := range name {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		name = "container-" + name
	}
	return name
}
