package bocker

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// IncusClient is a thin application-specific wrapper around the official
// Incus REST client. It connects to the local Unix socket by default.
type IncusClient struct {
	server    incus.InstanceServer
	initErr   error
	imageServ incus.ImageServer
	imageErr  error
}

// NewIncusClient connects only to Bocker's embedded local daemon.
func NewIncusClient() *IncusClient {
	c := &IncusClient{}
	args := connectionArgsFromEnv()
	if err := ensureEmbeddedDaemon(); err != nil {
		c.initErr = err
		return c
	}
	c.server, c.initErr = incus.ConnectIncusUnix("", args)
	return c
}

func connectionArgsFromEnv() *incus.ConnectionArgs {
	return &incus.ConnectionArgs{SkipGetEvents: true}
}

func (c *IncusClient) ready() error {
	if c == nil {
		return fmt.Errorf("Incus 客户端为空")
	}
	if c.initErr != nil {
		return fmt.Errorf("连接 Incus REST API 失败: %w", c.initErr)
	}
	return nil
}

func (c *IncusClient) imageServer() (incus.ImageServer, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	if c.imageServ != nil || c.imageErr != nil {
		return c.imageServ, c.imageErr
	}
	c.imageServ, c.imageErr = incus.ConnectSimpleStreams(MirrorURL, nil)
	if c.imageErr != nil {
		return nil, fmt.Errorf("连接镜像 SimpleStreams 服务失败: %w", c.imageErr)
	}
	return c.imageServ, nil
}

func archName() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}

const (
	defaultNICName      = "eth0"
	defaultBridgeParent = "ens18"
	bridgeParentEnv     = "BOCKER_BRIDGE_PARENT"
)

func detectBridgeParent() (string, error) {
	parent := strings.TrimSpace(os.Getenv(bridgeParentEnv))
	if parent != "" {
		if !linkExists(parent) {
			return "", fmt.Errorf("%s=%q 指定的网卡不存在或不可用", bridgeParentEnv, parent)
		}
		return parent, nil
	}
	if parent, err := defaultIPv4RouteParent(); err == nil && parent != "" {
		if !linkExists(parent) {
			return "", fmt.Errorf("默认出口网卡 %q 不存在或不可用", parent)
		}
		return parent, nil
	}
	if linkExists(defaultBridgeParent) {
		return defaultBridgeParent, nil
	}
	return "", fmt.Errorf("无法自动识别默认出口网卡，请设置 %s=网卡名", bridgeParentEnv)
}

func defaultIPv4RouteParent() (string, error) {
	out, err := exec.Command("ip", "-4", "route", "show", "default").Output()
	if err == nil {
		if dev := firstRouteDev(string(out)); dev != "" {
			return dev, nil
		}
	}
	showErr := err
	out, err = exec.Command("ip", "-4", "route", "get", "1.1.1.1").Output()
	if err == nil {
		if dev := firstRouteDev(string(out)); dev != "" {
			return dev, nil
		}
	}
	if showErr != nil {
		return "", fmt.Errorf("读取默认 IPv4 路由失败: %w", showErr)
	}
	if err != nil {
		return "", fmt.Errorf("探测默认 IPv4 出口失败: %w", err)
	}
	return "", fmt.Errorf("默认 IPv4 路由中没有 dev 字段")
}

func firstRouteDev(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "dev" || i+1 >= len(fields) {
				continue
			}
			dev := strings.TrimSpace(fields[i+1])
			if dev != "" && dev != "lo" && dev != defaultHostBridgeName {
				return dev
			}
		}
	}
	return ""
}

func linkExists(name string) bool {
	return exec.Command("ip", "link", "show", name).Run() == nil
}

// Container is the subset of api.InstanceFull used by the application.
type Container struct {
	Name            string
	Status          string
	StatusCode      int
	Type            string
	Config          map[string]string
	Devices         map[string]map[string]string
	ExpandedDevices map[string]map[string]string
	State           *ContainerState
}

type ContainerState struct {
	Network map[string]NICState
	Pid     int64
}

type NICState struct {
	Addresses []NICAddr
	HwAddr    string
	State     string
	Type      string
}

type NICAddr struct {
	Family  string
	Address string
	Scope   string
}

func convertContainer(full *api.InstanceFull) *Container {
	if full == nil {
		return nil
	}
	c := &Container{
		Name:            full.Name,
		Status:          full.Status,
		StatusCode:      int(full.StatusCode),
		Type:            full.Type,
		Config:          cloneConfig(full.Config),
		Devices:         cloneDevices(full.Devices),
		ExpandedDevices: cloneDevices(full.ExpandedDevices),
	}
	if full.State == nil {
		return c
	}
	c.State = &ContainerState{Network: make(map[string]NICState), Pid: full.State.Pid}
	for name, nic := range full.State.Network {
		state := NICState{HwAddr: nic.Hwaddr, State: nic.State, Type: nic.Type}
		for _, addr := range nic.Addresses {
			state.Addresses = append(state.Addresses, NICAddr{Family: addr.Family, Address: addr.Address, Scope: addr.Scope})
		}
		c.State.Network[name] = state
	}
	return c
}

func cloneConfig(in api.ConfigMap) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneDevices(in api.DevicesMap) map[string]map[string]string {
	out := make(map[string]map[string]string, len(in))
	for name, device := range in {
		copyDevice := make(map[string]string, len(device))
		for k, v := range device {
			copyDevice[k] = v
		}
		out[name] = copyDevice
	}
	return out
}

func apiConfig(in map[string]string) api.ConfigMap {
	out := api.ConfigMap{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func apiDevices(in map[string]map[string]string) api.DevicesMap {
	out := api.DevicesMap{}
	for name, device := range in {
		copyDevice := map[string]string{}
		for k, v := range device {
			copyDevice[k] = v
		}
		out[name] = copyDevice
	}
	return out
}

func writableInstance(full *api.InstanceFull) api.InstancePut {
	p := full.Writable()
	p.Config = apiConfig(cloneConfig(full.Config))
	p.Devices = apiDevices(cloneDevices(full.Devices))
	p.Profiles = append([]string(nil), full.Profiles...)
	return p
}

func (c *IncusClient) updateInstance(name string, etag string, put api.InstancePut) error {
	op, err := c.server.UpdateInstance(name, put, etag)
	if err != nil {
		return err
	}
	return op.Wait()
}

func (c *IncusClient) Start(name string) error {
	if err := c.ready(); err != nil {
		return err
	}
	full, _, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	mode, err := networkModeFromContainer(convertContainer(full))
	if err != nil {
		return err
	}
	if err := c.SetContainerNetwork(name, mode, false); err != nil {
		return fmt.Errorf("配置 %s 网络失败: %w", mode, err)
	}
	op, err := c.server.UpdateInstanceState(name, api.InstanceStatePut{Action: "start", Timeout: -1}, "")
	if err != nil {
		return err
	}
	if err := op.Wait(); err != nil {
		return err
	}
	if full.Config[permissionConfigKey] == string(PermissionSuper) {
		if err := c.ExecStreaming(name, superRuntimeCompatibility, nil); err != nil {
			return fmt.Errorf("apply super permission compatibility: %w", err)
		}
	}
	return nil
}

func randomMAC() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[0] = (buf[0] | 0x02) & 0xfe
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5]), nil
}

func (c *IncusClient) Stop(name string) error {
	if err := c.ready(); err != nil {
		return err
	}
	op, err := c.server.UpdateInstanceState(name, api.InstanceStatePut{Action: "stop", Timeout: 30}, "")
	if err != nil {
		return err
	}
	return op.Wait()
}

// StopForce 强制停止容器（Force=true, Timeout=0），用于构建容器的可靠清理。
// 即使容器内进程挂起或 Stop 超时，ForceStop 也能确保容器停止，避免后续 Delete 失败导致资源泄漏。
func (c *IncusClient) StopForce(name string) error {
	if err := c.ready(); err != nil {
		return err
	}
	op, err := c.server.UpdateInstanceState(name, api.InstanceStatePut{Action: "stop", Timeout: 0, Force: true}, "")
	if err != nil {
		return err
	}
	return op.Wait()
}

func (c *IncusClient) Delete(name string) error {
	if err := c.ready(); err != nil {
		return err
	}
	op, err := c.server.DeleteInstance(name)
	if err != nil {
		return err
	}
	return op.Wait()
}

func defaultExecEnv() map[string]string {
	term := os.Getenv("TERM")
	if term == "" {
		term = "xterm-256color"
	}
	// Do not set PATH here. Incus supplies its distribution-aware default and
	// appends /run/current-system/sw/bin for NixOS containers. Supplying a
	// traditional Linux PATH would override that detection and make NixOS
	// commands such as ls and cat unavailable by name.
	return map[string]string{
		"HOME": "/root",
		"TERM": term,
		"USER": "root",
	}
}

func (c *IncusClient) Exec(name string) error {
	if err := c.ready(); err != nil {
		return err
	}
	shell := "/bin/sh"
	if out, _ := c.execQuiet(name, "/bin/sh", "-c", "if test -x /bin/bash; then echo yes; fi"); out == "yes" {
		shell = "/bin/bash"
	}
	fd := int(os.Stdin.Fd())
	if old, err := makeRaw(fd); err == nil {
		defer restoreTerm(fd, old)
	}
	width, height := 120, 40
	if w, h, err := term.GetSize(fd); err == nil && w > 0 && h > 0 {
		width, height = w, h
	}
	if requestedWidth, requestedHeight := requestedTerminalSize(); requestedWidth > 0 && requestedHeight > 0 {
		width, height = requestedWidth, requestedHeight
	}
	req := api.InstanceExecPost{
		Command:     []string{shell},
		Environment: defaultExecEnv(),
		WaitForWS:   true,
		Interactive: true,
		Width:       width,
		Height:      height,
	}
	op, err := c.server.ExecInstance(name, req, &incus.InstanceExecArgs{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		return fmt.Errorf("无法进入容器 %s 的 shell: %w", name, err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("进入容器 %s 的 shell 断开或执行失败: %w", name, err)
	}
	return nil
}

func requestedTerminalSize() (int, int) {
	width, widthErr := strconv.Atoi(strings.TrimSpace(os.Getenv("BOCKER_TERM_WIDTH")))
	height, heightErr := strconv.Atoi(strings.TrimSpace(os.Getenv("BOCKER_TERM_HEIGHT")))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 0, 0
	}
	return width, height
}

// execExitCode 等待 exec 操作完成并返回命令退出码。
// op.Wait() 仅在操作本身失败时返回错误（如连接中断），
// 命令在容器内以非零退出码结束时 op.Wait() 仍返回 nil。
// 退出码存储在操作的 Metadata["return"] 字段中。
func execExitCode(op incus.Operation) (int, error) {
	if err := op.Wait(); err != nil {
		return -1, err
	}
	opAPI := op.Get()
	if opAPI.Metadata == nil {
		return 0, nil
	}
	ret, ok := opAPI.Metadata["return"]
	if !ok {
		return 0, nil
	}
	// JSON 解码后数字类型为 float64
	switch v := ret.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	default:
		return 0, nil
	}
}

func (c *IncusClient) execQuiet(name string, args ...string) (string, error) {
	if err := c.ready(); err != nil {
		return "", err
	}
	req := api.InstanceExecPost{
		Command:     args,
		Environment: defaultExecEnv(),
		WaitForWS:   true,
		Interactive: false,
	}
	var out bytes.Buffer
	op, err := c.server.ExecInstance(name, req, &incus.InstanceExecArgs{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard,
	})
	if err != nil {
		return "", err
	}
	exitCode, err := execExitCode(op)
	if err != nil {
		return strings.TrimSpace(out.String()), err
	}
	if exitCode != 0 {
		return strings.TrimSpace(out.String()), fmt.Errorf("命令退出码 %d", exitCode)
	}
	return strings.TrimSpace(out.String()), nil
}

func (c *IncusClient) Launch(imageRef, name string) error {
	mode, err := configuredNetworkMode()
	if err != nil {
		return err
	}
	return c.LaunchWithNetwork(imageRef, name, mode)
}

func (c *IncusClient) LaunchWithNetwork(imageRef, name string, mode NetworkMode) error {
	return c.LaunchWithNetworkAndPermission(imageRef, name, mode, PermissionNormal)
}

func (c *IncusClient) LaunchWithNetworkAndPermission(imageRef, name string, mode NetworkMode, permission PermissionMode) error {
	return c.LaunchWithNetworkAndPermissionAndFingerprint(imageRef, "", name, mode, permission)
}

func (c *IncusClient) LaunchWithNetworkAndPermissionAndFingerprint(imageRef, fingerprint, name string, mode NetworkMode, permission PermissionMode) error {
	if err := c.ready(); err != nil {
		return err
	}
	if err := validateBockerName(name); err != nil {
		return fmt.Errorf("容器名称 %q 无效: %w", name, err)
	}
	nic, err := c.newContainerNetworkDevice(mode)
	if err != nil {
		return err
	}
	_, alias := splitImageRemote(imageRef)
	// 规范化镜像引用：debian:12 -> debian/12，与镜像源 alias 一致
	alias = normalizeImageRef(alias)
	permission, err = ParsePermissionMode(string(permission))
	if err != nil {
		return err
	}
	config := map[string]string{
		containerNetworkConfig: string(mode),
	}
	applyPermissionConfig(config, permission)
	source := api.InstanceSource{Type: "image", Server: MirrorURL, Protocol: "simplestreams"}
	if fingerprint != "" {
		source.Fingerprint = fingerprint
	} else {
		source.Alias = alias
	}
	req := api.InstancesPost{
		Name: name,
		Type: api.InstanceTypeContainer,
		InstancePut: api.InstancePut{
			Config:  config,
			Devices: apiDevices(map[string]map[string]string{defaultNICName: nic}),
		},
		Source: source,
		Start:  false,
	}
	op, err := c.server.CreateInstance(req)
	if err != nil {
		return err
	}
	if err := op.Wait(); err != nil {
		return err
	}
	if err := c.Start(name); err != nil {
		return err
	}
	return nil
}

func (c *IncusClient) ListContainers() ([]Container, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	instances, err := c.server.GetInstancesFull(api.InstanceTypeContainer)
	if err != nil {
		return nil, fmt.Errorf("获取容器列表失败: %w", err)
	}
	result := make([]Container, 0, len(instances))
	for i := range instances {
		result = append(result, *convertContainer(&instances[i]))
	}
	return result, nil
}

func (c *IncusClient) GetContainer(name string) (*Container, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	instance, _, err := c.server.GetInstanceFull(name)
	if err != nil {
		return nil, fmt.Errorf("获取容器 %q 失败: %w", name, err)
	}
	return convertContainer(instance), nil
}

// BaseImageFingerprint returns the immutable image fingerprint resolved by
// Incus for a launched instance. Incus records this value even when the
// request used a mutable remote alias.
func (c *IncusClient) BaseImageFingerprint(name string) (string, error) {
	if err := c.ready(); err != nil {
		return "", err
	}
	full, _, err := c.server.GetInstanceFull(name)
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(full.Config["volatile.base_image"])), nil
}

func isInstanceNotFound(err error) bool {
	return api.StatusErrorCheck(err, http.StatusNotFound)
}

func (c *IncusClient) SetBootAutostart(name string, on bool) error {
	if err := c.ready(); err != nil {
		return err
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	put := writableInstance(full)
	if put.Config == nil {
		put.Config = api.ConfigMap{}
	}
	put.Config["boot.autostart"] = strconv.FormatBool(on)
	return c.updateInstance(name, etag, put)
}

func (c *IncusClient) SetDomain(name, domain string) error {
	if err := c.ready(); err != nil {
		return err
	}
	if err := validateDomainName(domain); err != nil {
		return fmt.Errorf("域名无效: %w", err)
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	put := writableInstance(full)
	if put.Config == nil {
		put.Config = api.ConfigMap{}
	}
	put.Config["user.bocker.domain"] = domain
	return c.updateInstance(name, etag, put)
}

func (c *IncusClient) UnsetDomain(name string) error {
	if err := c.ready(); err != nil {
		return err
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	put := writableInstance(full)
	delete(put.Config, "user.bocker.domain")
	return c.updateInstance(name, etag, put)
}

func (c *IncusClient) Export(name, path string) error {
	if err := c.ready(); err != nil {
		return err
	}
	f, err := openExclusiveExportFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(path)
		}
	}()
	backup := api.InstanceBackupsPost{CompressionAlgorithm: "gzip"}
	err = c.server.CreateInstanceBackupStream(name, backup, &incus.BackupFileRequest{BackupFile: f})
	if err == nil {
		completed = true
		return nil
	}
	// Older servers may not expose direct_backup. Fall back to a temporary
	// persisted backup and download it through the standard REST endpoint.
	if _, seekErr := f.Seek(0, io.SeekStart); seekErr == nil {
		_ = f.Truncate(0)
	}
	backup.Name = fmt.Sprintf("bocker-export-%d", time.Now().UnixNano())
	op, createErr := c.server.CreateInstanceBackup(name, backup)
	if createErr != nil {
		return fmt.Errorf("导出备份失败: %w (direct backup: %v)", createErr, err)
	}
	if createErr = op.Wait(); createErr != nil {
		return createErr
	}
	defer func() {
		if deleteOp, e := c.server.DeleteInstanceBackup(name, backup.Name); e == nil {
			_ = deleteOp.Wait()
		}
	}()
	_, downloadErr := c.server.GetInstanceBackupFile(name, backup.Name, &incus.BackupFileRequest{BackupFile: f})
	if downloadErr == nil {
		completed = true
	}
	return downloadErr
}

func openExclusiveExportFile(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("export path must be absolute")
	}
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func (c *IncusClient) Import(path, name string) error {
	if err := c.ready(); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// Backups retain the source instance's NIC MAC. Override it during the
	// import request so the new instance never collides with its source.
	mac, err := randomMAC()
	if err != nil {
		return fmt.Errorf("generate import NIC MAC: %w", err)
	}
	op, err := c.server.CreateInstanceFromBackup(incus.InstanceBackupArgs{
		BackupFile: f,
		Name:       name,
		Config:     []string{"volatile.eth0.hwaddr=" + mac},
		Devices:    []string{"eth0,hwaddr=" + mac},
	})
	if err != nil {
		return err
	}
	if err := op.Wait(); err != nil {
		return err
	}
	return c.EnsurePermission(name, PermissionNormal)
}

// EnsurePrivileged 确保容器以高权限运行 (security.privileged=true)。
// 已是高权限则跳过。用于导入/迁移场景保持策略一致。
func (c *IncusClient) EnsurePrivileged(name string) error {
	if err := c.ready(); err != nil {
		return err
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	if full.Config["security.privileged"] == "true" {
		return nil
	}
	put := writableInstance(full)
	if put.Config == nil {
		put.Config = api.ConfigMap{}
	}
	put.Config["security.privileged"] = "true"
	return c.updateInstance(name, etag, put)
}

func (c *IncusClient) EnsurePermission(name string, mode PermissionMode) error {
	if err := c.ready(); err != nil {
		return err
	}
	mode, err := ParsePermissionMode(string(mode))
	if err != nil {
		return err
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	put := writableInstance(full)
	if put.Config == nil {
		put.Config = api.ConfigMap{}
	}
	applyPermissionConfig(map[string]string(put.Config), mode)
	return c.updateInstance(name, etag, put)
}

func (c *IncusClient) ConfigureImportedNetwork(name string) error {
	ct, err := c.GetContainer(name)
	if err != nil {
		return err
	}
	mode, err := networkModeFromContainer(ct)
	if err != nil {
		return err
	}
	return c.SetContainerNetwork(name, mode, true)
}

func (c *IncusClient) ConfigureImportedNetworkWithMode(name string, mode NetworkMode) error {
	return c.SetContainerNetwork(name, mode, true)
}

func (ct *Container) NICMAC(nic string) string {
	for _, devs := range []map[string]map[string]string{ct.Devices, ct.ExpandedDevices} {
		if dev := devs[nic]; dev != nil && strings.TrimSpace(dev["hwaddr"]) != "" {
			return strings.TrimSpace(dev["hwaddr"])
		}
	}
	if ct.Config != nil {
		if mac := strings.TrimSpace(ct.Config["volatile."+nic+".hwaddr"]); mac != "" {
			return mac
		}
	}
	if ct.State != nil {
		if state, ok := ct.State.Network[nic]; ok {
			return strings.TrimSpace(state.HwAddr)
		}
	}
	return ""
}

func (ct *Container) UsesBridgeNIC(nic string) bool {
	dev := ct.ExpandedDevices[nic]
	if dev == nil {
		dev = ct.Devices[nic]
	}
	return dev != nil && dev["type"] == "nic" && dev["nictype"] == "macvlan"
}

func (ct *Container) NetworkMode() string {
	mode, err := networkModeFromContainer(ct)
	if err != nil {
		return string(defaultNetworkMode)
	}
	return string(mode)
}

func (ct *Container) IPv4() string {
	if ct.State == nil {
		return ""
	}
	for name, nic := range ct.State.Network {
		if name == "lo" || nic.Type == "loopback" {
			continue
		}
		for _, addr := range nic.Addresses {
			if addr.Family == "inet" && addr.Scope == "global" {
				return addr.Address
			}
		}
	}
	return ""
}

// IPv6Addresses returns all global IPv6 addresses assigned to the container.
// Incus uses the global scope for public and ULA addresses; link-local
// addresses are intentionally excluded from Bocker's application view.
func (ct *Container) IPv6Addresses() []string {
	if ct.State == nil {
		return nil
	}
	seen := map[string]bool{}
	addresses := []string{}
	for name, nic := range ct.State.Network {
		if name == "lo" || nic.Type == "loopback" {
			continue
		}
		for _, addr := range nic.Addresses {
			if addr.Family != "inet6" || addr.Scope != "global" || addr.Address == "" || seen[addr.Address] {
				continue
			}
			seen[addr.Address] = true
			addresses = append(addresses, addr.Address)
		}
	}
	sort.Strings(addresses)
	return addresses
}

func (ct *Container) IPv6() string {
	addresses := ct.IPv6Addresses()
	if len(addresses) == 0 {
		return ""
	}
	return addresses[0]
}

func (ct *Container) IPAddresses() []string {
	addresses := []string{}
	if ipv4 := ct.IPv4(); ipv4 != "" {
		addresses = append(addresses, ipv4)
	}
	return append(addresses, ct.IPv6Addresses()...)
}

func (ct *Container) Autostart() string { return ct.Config["boot.autostart"] }
func (ct *Container) Domain() string    { return ct.Config["user.bocker.domain"] }

type Image struct {
	Architecture string
	Type         string
	Aliases      []ImageAlias
	Properties   map[string]string
	Size         int64
}

type ImageAlias struct{ Name string }

type ImageVersion struct {
	Release string
	Image   string
}

type DistroGroup struct {
	Distro   string
	Versions []ImageVersion
}

func (c *IncusClient) ListImages() ([]DistroGroup, error) {
	remote, err := c.imageServer()
	if err != nil {
		return nil, err
	}
	images, err := remote.GetImages()
	if err != nil {
		return nil, fmt.Errorf("获取镜像列表失败: %w", err)
	}
	arch := archName() // 动态获取当前主机架构，支持 amd64/arm64
	grouped := map[string]map[string]string{}
	distroOrder := []string{}
	for _, img := range images {
		if img.Type != "container" || img.Architecture != arch || img.Properties["variant"] == "cloud" {
			continue
		}
		osName, release := img.Properties["os"], img.Properties["release"]
		if osName == "" || release == "" {
			continue
		}
		osKey, relKey := strings.ToLower(osName), strings.ToLower(release)
		shortest := ""
		for _, alias := range img.Aliases {
			if shortest == "" || len(alias.Name) < len(shortest) {
				shortest = alias.Name
			}
		}
		if shortest == "" {
			continue
		}
		if grouped[osKey] == nil {
			grouped[osKey] = map[string]string{}
			distroOrder = append(distroOrder, osKey)
		}
		if current := grouped[osKey][relKey]; current == "" || len(shortest) < len(current) {
			grouped[osKey][relKey] = shortest
		}
	}
	sort.Strings(distroOrder)
	result := make([]DistroGroup, 0, len(distroOrder))
	for _, osKey := range distroOrder {
		rels := grouped[osKey]
		relKeys := make([]string, 0, len(rels))
		for release := range rels {
			relKeys = append(relKeys, release)
		}
		sort.Strings(relKeys)
		versions := make([]ImageVersion, 0, len(relKeys))
		for _, release := range relKeys {
			versions = append(versions, ImageVersion{Release: release, Image: strings.TrimSuffix(rels[release], "/default")})
		}
		result = append(result, DistroGroup{Distro: titleCase(osKey), Versions: versions})
	}
	return result, nil
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// LaunchLocalImage 从本地镜像存储启动容器 (不从远程镜像服务器拉取)。
// 用于 bocker build/run 启动已构建的本地镜像。
func (c *IncusClient) LaunchLocalImage(alias, name string) error {
	mode, err := configuredNetworkMode()
	if err != nil {
		return err
	}
	return c.LaunchLocalImageWithNetwork(alias, name, mode)
}

func (c *IncusClient) LaunchLocalImageWithNetwork(alias, name string, mode NetworkMode) error {
	return c.LaunchLocalImageWithNetworkAndPermission(alias, name, mode, PermissionNormal)
}

func (c *IncusClient) LaunchLocalImageWithNetworkAndPermission(alias, name string, mode NetworkMode, permission PermissionMode) error {
	return c.LaunchLocalImageWithNetworkPermissionAndConfig(alias, name, mode, permission, nil)
}

// LaunchLocalImageWithNetworkPermissionAndConfig starts a local image with
// additional instance configuration applied before its init process starts.
func (c *IncusClient) LaunchLocalImageWithNetworkPermissionAndConfig(alias, name string, mode NetworkMode, permission PermissionMode, extraConfig map[string]string) error {
	if err := c.ready(); err != nil {
		return err
	}
	if err := validateBockerName(name); err != nil {
		return fmt.Errorf("容器名称 %q 无效: %w", name, err)
	}
	if err := validateBockerName(alias); err != nil {
		return fmt.Errorf("镜像别名 %q 无效: %w", alias, err)
	}
	nic, err := c.newContainerNetworkDevice(mode)
	if err != nil {
		return err
	}
	permission, err = ParsePermissionMode(string(permission))
	if err != nil {
		return err
	}
	config := map[string]string{
		containerNetworkConfig: string(mode),
	}
	for key, value := range extraConfig {
		config[key] = value
	}
	applyPermissionConfig(config, permission)
	req := api.InstancesPost{
		Name: name,
		Type: api.InstanceTypeContainer,
		InstancePut: api.InstancePut{
			Config:  config,
			Devices: apiDevices(map[string]map[string]string{defaultNICName: nic}),
		},
		Source: api.InstanceSource{
			Type:  "image",
			Alias: alias,
		},
		Start: false,
	}
	op, err := c.server.CreateInstance(req)
	if err != nil {
		return err
	}
	if err := op.Wait(); err != nil {
		return err
	}
	if err := c.Start(name); err != nil {
		return err
	}
	return nil
}

// PushFile 将字节数据写入容器的指定路径 (覆盖模式)。
// mode 为八进制权限字符串如 "0644"，为空时默认 0644。UID/GID 保持不变。
func (c *IncusClient) PushFile(name, path string, content []byte, mode string) error {
	return c.PushFileReader(name, path, bytes.NewReader(content), mode)
}

func (c *IncusClient) PushFileReader(name, path string, content io.ReadSeeker, mode string) error {
	if err := c.ready(); err != nil {
		return err
	}
	if content == nil {
		return fmt.Errorf("写入容器的内容不能为空")
	}
	if mode == "" {
		mode = "0644"
	}
	modeInt, err := strconv.ParseInt(mode, 8, 64)
	if err != nil {
		return fmt.Errorf("权限 %q 不是合法的八进制数: %w", mode, err)
	}
	args := incus.InstanceFileArgs{
		Content:   content,
		UID:       -1,
		GID:       -1,
		Mode:      int(modeInt),
		Type:      "file",
		WriteMode: "overwrite",
	}
	return c.server.CreateInstanceFile(name, path, args)
}

// ReadFile 读取容器内指定路径的文件内容。
func (c *IncusClient) ReadFile(name, path string) (string, error) {
	if err := c.ready(); err != nil {
		return "", err
	}
	reader, _, err := c.server.GetInstanceFile(name, path)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// CopyBetweenContainers 从源容器复制文件/目录到目标容器。
// 通过 tar 流中转：在源容器内 tar 打包到 stdout，在目标容器内从 stdin 解压。
// 两个容器都必须处于运行状态 (exec 依赖容器运行)。
//
// srcPath: 源容器内绝对路径
// dstPath: 目标容器内绝对路径 (父目录会自动创建)
//
// 行为：把 srcPath 复制为 dstPath (类似 cp -r src dst)。
// 如果 dstPath 的 basename 与 srcPath 的 basename 不同，会在解压后重命名。
func (c *IncusClient) CopyBetweenContainers(srcName, srcPath, dstName, dstPath string) error {
	if err := c.ready(); err != nil {
		return err
	}
	if strings.IndexByte(srcPath, 0) >= 0 || strings.IndexByte(dstPath, 0) >= 0 {
		return fmt.Errorf("COPY 路径不能包含 NUL 字符")
	}

	dstIsDir := strings.HasSuffix(dstPath, "/")
	cleanDst := dstPath
	if !path.IsAbs(cleanDst) {
		cleanDst = "/" + cleanDst
	}
	cleanDst = path.Clean(cleanDst)
	if !dstIsDir {
		_, err := c.execQuiet(dstName, "test", "-d", cleanDst)
		dstIsDir = err == nil
	}
	srcDir, srcBase, target, err := crossContainerCopyPaths(srcPath, cleanDst, dstIsDir)
	if err != nil {
		return err
	}

	// Tar through an io.Pipe so large build artifacts are never buffered in RAM.
	srcCmd := "tar cf - -C " + shellQuote(srcDir) + " " + shellQuote("./"+srcBase)
	parent := path.Dir(target)
	tmpDir := path.Join(parent, fmt.Sprintf(".bocker-copy-%d-%d", os.Getpid(), time.Now().UnixNano()))
	tmpSource := path.Join(tmpDir, srcBase)
	dstCmd := strings.Join([]string{
		"set -eu",
		"mkdir -p " + shellQuote(parent),
		"rm -rf " + shellQuote(tmpDir),
		"mkdir " + shellQuote(tmpDir),
		"tar xf - -C " + shellQuote(tmpDir),
		"if [ -d " + shellQuote(tmpSource) + " ] && [ -d " + shellQuote(target) + " ]; then " +
			"tar cf - -C " + shellQuote(tmpSource) + " . | tar xf - -C " + shellQuote(target) +
			"; else rm -rf " + shellQuote(target) + "; mv " + shellQuote(tmpSource) + " " + shellQuote(target) + "; fi",
		"rm -rf " + shellQuote(tmpDir),
	}, "; ")

	reader, writer := io.Pipe()
	sourceDone := make(chan error, 1)
	go func() {
		var stderr bytes.Buffer
		dataDone := make(chan bool)
		req := api.InstanceExecPost{
			Command:     []string{"/bin/sh", "-c", srcCmd},
			Environment: defaultExecEnv(),
			WaitForWS:   true,
			Interactive: false,
		}
		op, startErr := c.server.ExecInstance(srcName, req, &incus.InstanceExecArgs{
			Stdin: strings.NewReader(""), Stdout: writer, Stderr: &stderr, DataDone: dataDone,
		})
		var sourceErr error
		if startErr != nil {
			sourceErr = fmt.Errorf("启动源容器 tar 失败: %w", startErr)
		} else {
			exitCode, waitErr := execExitCode(op)
			<-dataDone
			if waitErr != nil {
				sourceErr = fmt.Errorf("等待源容器 tar 失败: %w", waitErr)
			} else if exitCode != 0 {
				detail := strings.TrimSpace(stderr.String())
				if detail != "" {
					sourceErr = fmt.Errorf("源容器 tar 退出码 %d: %s", exitCode, detail)
				} else {
					sourceErr = fmt.Errorf("源容器 tar 退出码 %d (路径 %s 不存在?)", exitCode, srcPath)
				}
			}
		}
		_ = writer.CloseWithError(sourceErr)
		sourceDone <- sourceErr
	}()

	dstDataDone := make(chan bool)
	dstReq := api.InstanceExecPost{
		Command:     []string{"/bin/sh", "-c", dstCmd},
		Environment: defaultExecEnv(),
		WaitForWS:   true,
		Interactive: false,
	}
	dstOp, startErr := c.server.ExecInstance(dstName, dstReq, &incus.InstanceExecArgs{
		Stdin: reader, Stdout: io.Discard, Stderr: os.Stderr, DataDone: dstDataDone,
	})
	if startErr != nil {
		_ = reader.CloseWithError(startErr)
		<-sourceDone
		return fmt.Errorf("启动目标容器 tar 失败: %w", startErr)
	}
	dstExitCode, dstWaitErr := execExitCode(dstOp)
	_ = reader.Close()
	<-dstDataDone
	sourceErr := <-sourceDone
	if sourceErr != nil {
		_, _ = c.execQuiet(dstName, "rm", "-rf", tmpDir)
		return fmt.Errorf("从源容器 %s 复制失败: %w", srcName, sourceErr)
	}
	if dstWaitErr != nil {
		return fmt.Errorf("等待目标容器 tar 失败: %w", dstWaitErr)
	}
	if dstExitCode != 0 {
		return fmt.Errorf("目标容器 tar 退出码 %d", dstExitCode)
	}
	return nil
}

func crossContainerCopyPaths(srcPath, dstPath string, dstIsDir bool) (srcDir, srcBase, target string, err error) {
	if !path.IsAbs(srcPath) {
		return "", "", "", fmt.Errorf("源路径必须是绝对路径: %s", srcPath)
	}
	srcPath = path.Clean(srcPath)
	if srcPath == "/" {
		return "", "", "", fmt.Errorf("COPY --from 不支持复制容器根目录")
	}
	if !path.IsAbs(dstPath) {
		dstPath = "/" + dstPath
	}
	dstPath = path.Clean(dstPath)
	srcDir = path.Dir(srcPath)
	srcBase = path.Base(srcPath)
	target = dstPath
	if dstIsDir || dstPath == "/" {
		target = path.Join(dstPath, srcBase)
	}
	return srcDir, srcBase, target, nil
}

// ExecStreaming 在容器内通过 /bin/sh -c 执行命令，stdout/stderr 实时输出到当前进程。
// extraEnv 会与默认环境合并，使 Incusfile 的 ENV 指令对后续 RUN 生效。
func (c *IncusClient) ExecStreaming(name, command string, extraEnv map[string]string) error {
	return c.execStreaming(name, []string{"/bin/sh", "-c", command}, extraEnv)
}

// ExecStreamingArgs executes an argv vector without involving a shell. This
// preserves argument boundaries for public `container exec` calls.
func (c *IncusClient) ExecStreamingArgs(name string, args []string, extraEnv map[string]string) error {
	if len(args) == 0 {
		return fmt.Errorf("执行命令不能为空")
	}
	return c.execStreaming(name, append([]string(nil), args...), extraEnv)
}

func (c *IncusClient) execStreaming(name string, command []string, extraEnv map[string]string) error {
	if err := c.ready(); err != nil {
		return err
	}
	env := defaultExecEnv()
	for k, v := range extraEnv {
		env[k] = v
	}
	req := api.InstanceExecPost{
		Command:     command,
		Environment: env,
		WaitForWS:   true,
		Interactive: false,
	}
	op, err := c.server.ExecInstance(name, req, &incus.InstanceExecArgs{
		Stdin:  strings.NewReader(""),
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		return err
	}
	exitCode, err := execExitCode(op)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("命令退出码 %d", exitCode)
	}
	return nil
}

// PublishImage 将容器发布为本地 Incus 镜像，并设置别名。
// properties 会作为镜像属性存储，供 bocker image run 读取以恢复 EXPOSE/DOMAIN/AUTOSTART。
func (c *IncusClient) PublishImage(containerName, alias string, properties map[string]string) error {
	if err := c.ready(); err != nil {
		return err
	}
	if err := validateBockerName(alias); err != nil {
		return fmt.Errorf("镜像别名 %q 无效: %w", alias, err)
	}
	req := api.ImagesPost{
		Source: &api.ImagesPostSource{
			Type: "container",
			Name: containerName,
		},
	}
	if properties != nil {
		req.Properties = properties
	}
	op, err := c.server.CreateImage(req, nil)
	if err != nil {
		return err
	}
	if err := op.Wait(); err != nil {
		return err
	}
	// 从操作元数据获取 fingerprint
	fingerprint := ""
	opAPI := op.Get()
	if opAPI.Metadata != nil {
		if fp, ok := opAPI.Metadata["fingerprint"].(string); ok {
			fingerprint = fp
		}
	}
	if fingerprint == "" {
		for _, resource := range opAPI.Resources["images"] {
			candidate := path.Base(resource)
			if candidate != "." && candidate != "/" && candidate != "images" {
				fingerprint = candidate
				break
			}
		}
	}
	if fingerprint == "" {
		return fmt.Errorf("镜像已发布，但 Incus 操作未返回 fingerprint")
	}

	// The image exists before the alias changes, so a failed rebuild leaves the
	// previous alias intact. UpdateImageAlias uses an ETag to prevent races.
	oldAlias, etag, aliasErr := c.server.GetImageAlias(alias)
	if aliasErr == nil {
		if oldAlias.Target == fingerprint {
			return nil
		}
		put := api.ImageAliasesEntryPut{Target: fingerprint, Description: oldAlias.Description}
		if err := c.server.UpdateImageAlias(alias, put, etag); err != nil {
			_ = c.deleteImageIfUnaliased(fingerprint)
			return fmt.Errorf("切换镜像别名 %s 失败: %w", alias, err)
		}
		_ = c.deleteImageIfUnaliased(oldAlias.Target)
		return nil
	}
	if !api.StatusErrorCheck(aliasErr, http.StatusNotFound) {
		_ = c.deleteImageIfUnaliased(fingerprint)
		return fmt.Errorf("读取镜像别名 %s 失败: %w", alias, aliasErr)
	}

	aliasReq := api.ImageAliasesPost{ImageAliasesEntry: api.ImageAliasesEntry{
		Name:                 alias,
		ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: fingerprint},
	}}
	if err := c.server.CreateImageAlias(aliasReq); err != nil {
		_ = c.deleteImageIfUnaliased(fingerprint)
		return fmt.Errorf("创建镜像别名 %s 失败: %w", alias, err)
	}
	return nil
}

func (c *IncusClient) deleteImageIfUnaliased(fingerprint string) error {
	aliases, err := c.server.GetImageAliases()
	if err != nil {
		return err
	}
	for _, alias := range aliases {
		if alias.Target == fingerprint {
			return nil
		}
	}
	op, err := c.server.DeleteImage(fingerprint)
	if err != nil {
		return err
	}
	return op.Wait()
}

// ReplaceImageAlias 删除已存在的镜像别名。若旧镜像无其他别名引用，则一并删除孤儿镜像。
// 用于重新构建同名镜像时保持幂等。别名不存在时静默返回 nil。
func (c *IncusClient) ReplaceImageAlias(alias string) error {
	if err := c.ready(); err != nil {
		return err
	}
	entry, _, err := c.server.GetImageAlias(alias)
	if err != nil {
		return nil // 别名不存在，无需处理
	}
	// 删除旧别名
	if err := c.server.DeleteImageAlias(alias); err != nil {
		return err
	}
	_ = c.deleteImageIfUnaliased(entry.Target)
	return nil
}

// ListLocalImageAliases 列出本地所有镜像别名 (含 bocker 构建的镜像)。
// 用于 bocker image run 的交互式选择。
func (c *IncusClient) ListLocalImageAliases() ([]string, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	aliases, err := c.server.GetImageAliases()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(aliases))
	for _, a := range aliases {
		names = append(names, a.Name)
	}
	return names, nil
}

// ImageAliasInfo 是带详情的本地镜像别名信息 (供 bocker images 展示)。
type ImageAliasInfo struct {
	Name      string
	Target    string    // 镜像指纹 (fingerprint)
	Size      int64     // 镜像大小 (字节)
	CreatedAt time.Time // 镜像创建时间
}

// ListLocalImageAliasesWithDetails 列出本地镜像别名及其详情 (大小/时间/指纹)。
// 别名按名称升序排列。读取单个镜像详情失败时跳过该别名而非整体失败 (镜像可能已被手动删除)。
func (c *IncusClient) ListLocalImageAliasesWithDetails() ([]ImageAliasInfo, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	aliases, err := c.server.GetImageAliases()
	if err != nil {
		return nil, err
	}
	images, err := c.server.GetImages()
	if err != nil {
		return nil, err
	}
	byFingerprint := make(map[string]api.Image, len(images))
	for _, image := range images {
		byFingerprint[image.Fingerprint] = image
	}
	infos := make([]ImageAliasInfo, 0, len(aliases))
	for _, a := range aliases {
		info := ImageAliasInfo{Name: a.Name, Target: a.Target}
		if image, ok := byFingerprint[a.Target]; ok {
			info.Size = image.Size
			info.CreatedAt = image.UploadedAt
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// GetImageAliasEntry 查询镜像别名是否存在, 存在返回其指向的 fingerprint。
func (c *IncusClient) GetImageAliasEntry(alias string) (string, error) {
	if err := c.ready(); err != nil {
		return "", err
	}
	entry, _, err := c.server.GetImageAlias(alias)
	if err != nil {
		return "", err
	}
	return entry.Target, nil
}

// DeleteImageByAlias 删除镜像别名及其指向的镜像。
// 若该镜像还有其他别名引用，仅删除别名保留镜像；否则连镜像一起删除。
func (c *IncusClient) DeleteImageByAlias(alias string) error {
	if err := c.ready(); err != nil {
		return err
	}
	entry, _, err := c.server.GetImageAlias(alias)
	if err != nil {
		return fmt.Errorf("镜像别名 %s 不存在", alias)
	}
	// 删除别名
	if err := c.server.DeleteImageAlias(alias); err != nil {
		return fmt.Errorf("删除别名失败: %w", err)
	}
	// 检查镜像是否还有其他别名引用
	aliases, err := c.server.GetImageAliases()
	if err != nil {
		return fmt.Errorf("删除别名后无法确认镜像引用关系；保留底层镜像: %w", err)
	}
	for _, a := range aliases {
		if a.Target == entry.Target {
			return nil // 仍有其他别名引用，保留镜像
		}
	}
	// 无其他引用，删除孤儿镜像
	op, err := c.server.DeleteImage(entry.Target)
	if err != nil {
		return fmt.Errorf("别名已删除，但删除未引用镜像失败: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("别名已删除，但等待镜像删除完成失败: %w", err)
	}
	return nil
}

// GetImageProperties 读取本地镜像 (通过别名) 的属性。
func (c *IncusClient) GetImageProperties(alias string) (map[string]string, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	entry, _, err := c.server.GetImageAlias(alias)
	if err != nil {
		return nil, err
	}
	image, _, err := c.server.GetImage(entry.Target)
	if err != nil {
		return nil, err
	}
	return image.Properties, nil
}
