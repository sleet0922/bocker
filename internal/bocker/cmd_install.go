package bocker

import (
	"fmt"
	"strings"
)

// CmdInstall 两级菜单：先选发行版，再选具体版本，最后安装。
// 若传入模板：bocker template install <template> [--name <name>]，则直接安装。
func CmdInstall(args []string) error {
	nameOverride, args, err := nameOptionFromArgs(args)
	if err != nil {
		return err
	}
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
	if len(args) > 1 {
		return fmt.Errorf("template install 只接受一个模板名；容器名请用 --name 指定")
	}

	if len(args) >= 1 {
		imageRef := args[0]
		// 规范化镜像引用：debian:12 -> debian/12，统一内部处理
		imageRef = normalizeImageRef(imageRef)
		name := nameOverride
		if name == "" {
			name = defaultNameFromImage(args[0])
		}
		if err := validateBockerName(name); err != nil {
			return fmt.Errorf("容器名称 %q 无效: %w", name, err)
		}
		fmt.Printf("正在安装 %s (名称: %s, 网络: %s, 权限: %s) ...\n", imageRef, name, mode, permissionMode)
		if err := client.LaunchWithNetworkAndPermission(imageRef, name, mode, permissionMode); err != nil {
			return err
		}
		if err := finishContainerInstall(client, name, mode); err != nil {
			rollbackFailedInstall(client, name)
			return err
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
	name := nameOverride
	if name == "" {
		name = prompt(fmt.Sprintf("容器名称 (回车默认 %s): ", defaultName))
		if name == "" {
			name = defaultName
		}
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

	if err := finishContainerInstall(client, name, mode); err != nil {
		rollbackFailedInstall(client, name)
		return err
	}

	fmt.Printf("✔ 容器 %s 已安装并启动!\n", name)
	return nil
}

func finishContainerInstall(client *IncusClient, name string, mode NetworkMode) error {
	if ip := waitForIP(client, name, 30); ip == "" {
		return fmt.Errorf("容器 %s 在 30 秒内未获取 IPv4（网络模式: %s）；安装已回滚，请检查 DHCP、DNS 和宿主机网络配置", name, mode)
	}
	if err := AutoConfigureHostBridge(client); err != nil {
		return fmt.Errorf("配置宿主机 bridge 互通失败: %w", err)
	}
	return nil
}

func rollbackFailedInstall(client *IncusClient, name string) {
	if err := client.Stop(name); err != nil {
		_ = client.StopForce(name)
	}
	if err := client.Delete(name); err != nil {
		fmt.Printf("⚠ 回滚失败安装的容器 %s 失败: %v\n", name, err)
	}
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
