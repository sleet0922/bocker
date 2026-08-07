//go:build linux

package bocker

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
	"golang.org/x/sys/unix"
)

const (
	embeddedIncusVersion  = "7.2-container"
	defaultBockerState    = "/var/lib/bocker"
	defaultSocketGroup    = "lxd"
	embeddedSocketWait    = 90 * time.Second
	hostDependencyTimeout = 5 * time.Minute
	daemonGracefulStop    = 20 * time.Second
	daemonTermStop        = 5 * time.Second
	daemonKillWait        = 2 * time.Second
)

// incusRuntimeArchive contains the container-only Incus daemon, liblxc and
// their private shared libraries. It deliberately contains no Incus CLI.
//
//go:embed runtime/incus-runtime.zip
var incusRuntimeArchive []byte

var (
	embeddedDaemonOnce sync.Once
	embeddedDaemonErr  error
)

type embeddedPaths struct {
	stateDir   string
	incusDir   string
	runtimeDir string
	socket     string
	control    string
	logDir     string
	lxcfsDir   string
}

// ensureEmbeddedDaemon makes the Bocker-owned daemon available before the
// application client connects to it.
func ensureEmbeddedDaemon() error {
	embeddedDaemonOnce.Do(func() {
		embeddedDaemonErr = ensureEmbeddedDaemonOnce()
	})
	return embeddedDaemonErr
}

func ensureEmbeddedDaemonOnce() error {
	if runtime.GOARCH != "amd64" {
		return fmt.Errorf("the embedded Incus runtime currently supports linux/amd64, got linux/%s", runtime.GOARCH)
	}

	paths, err := embeddedRuntimePaths()
	if err != nil {
		return err
	}
	setEmbeddedEnvironment(paths)
	// The daemon owns the host-level privileges.  Normal users connect to its
	// group-authorized Unix socket and never need to start a privileged process.
	if os.Geteuid() != 0 {
		server, connectErr := connectEmbeddedServer(paths.socket)
		if connectErr != nil {
			return fmt.Errorf("无法连接 Bocker 后台服务（普通用户无需 sudo，但需要已启动 bocker.service 且当前用户在 lxd 组）: %w", connectErr)
		}
		return ensureDefaultIncusConfig(server)
	}
	if err := ensureHostSetfattr(); err != nil {
		return err
	}

	managed, err := startSystemdSupervisor(paths)
	if err != nil {
		return err
	}

	server, err := connectEmbeddedServer(paths.socket)
	if err != nil {
		if !managed {
			if err := spawnEmbeddedSupervisor(paths); err != nil {
				return err
			}
		}
		server, err = waitForEmbeddedServer(paths, embeddedSocketWait)
		if err != nil {
			return err
		}
	}

	if err := ensureDefaultIncusConfig(server); err != nil {
		return fmt.Errorf("initialize Bocker container runtime: %w", err)
	}
	return nil
}

// ensureHostSetfattr installs the host utility required by Incus when it is
// missing. Bocker has already verified root, so apt-get is invoked directly;
// sudo would be both redundant and unable to work in the systemd supervisor.
func ensureHostSetfattr() error {
	if _, err := exec.LookPath("setfattr"); err == nil {
		return nil
	}
	aptGet, err := exec.LookPath("apt-get")
	if err != nil {
		return errors.New("缺少宿主机依赖 setfattr，且系统没有 apt-get；请安装 attr 软件包后重试")
	}

	fmt.Println("首次启动：缺少 setfattr，正在安装 attr 软件包 ...")
	ctx, cancel := context.WithTimeout(context.Background(), hostDependencyTimeout)
	defer cancel()
	update := exec.CommandContext(ctx, aptGet, "update")
	update.Stdout = os.Stdout
	update.Stderr = os.Stderr
	if err := update.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("更新 apt 软件包索引超时: %w", ctx.Err())
		}
		return fmt.Errorf("更新 apt 软件包索引失败: %w", err)
	}

	install := exec.CommandContext(ctx, aptGet, "install", "-y", "attr")
	install.Stdout = os.Stdout
	install.Stderr = os.Stderr
	if err := install.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("安装 attr 软件包超时: %w", ctx.Err())
		}
		return fmt.Errorf("安装 attr 软件包失败: %w", err)
	}
	if _, err := exec.LookPath("setfattr"); err != nil {
		return errors.New("attr 软件包安装完成，但仍找不到 setfattr")
	}
	return nil
}

func embeddedRuntimePaths() (embeddedPaths, error) {
	stateDir := strings.TrimSpace(os.Getenv("BOCKER_STATE_DIR"))
	if stateDir == "" {
		stateDir = defaultBockerState
	}
	stateDir, err := filepath.Abs(stateDir)
	if err != nil {
		return embeddedPaths{}, fmt.Errorf("resolve BOCKER_STATE_DIR: %w", err)
	}

	sum := sha256.Sum256(incusRuntimeArchive)
	runtimeID := fmt.Sprintf("%x", sum[:8])
	incusDir := filepath.Join(stateDir, "incus")
	return embeddedPaths{
		stateDir:   stateDir,
		incusDir:   incusDir,
		runtimeDir: filepath.Join(stateDir, "runtime", embeddedIncusVersion+"-"+runtimeID),
		socket:     filepath.Join(incusDir, "unix.socket"),
		control:    filepath.Join(incusDir, "bocker-control.socket"),
		logDir:     filepath.Join(stateDir, "logs"),
		lxcfsDir:   "/var/lib/incus-lxcfs",
	}, nil
}

func setEmbeddedEnvironment(paths embeddedPaths) {
	_ = os.Setenv("INCUS_DIR", paths.incusDir)
	_ = os.Setenv("INCUS_SOCKET", paths.socket)
	_ = os.Setenv("INCUS_LXC_HOOK", filepath.Join(paths.runtimeDir, "share", "lxc", "hooks"))
	_ = os.Setenv("INCUS_LXC_TEMPLATE_CONFIG", filepath.Join(paths.runtimeDir, "share", "lxc", "config"))

	libDir := filepath.Join(paths.runtimeDir, "lib")
	if old := os.Getenv("LD_LIBRARY_PATH"); old != "" {
		libDir += ":" + old
	}
	_ = os.Setenv("LD_LIBRARY_PATH", libDir)

	binDir := filepath.Join(paths.runtimeDir, "bin")
	if old := os.Getenv("PATH"); old != "" {
		binDir += ":" + old
	}
	_ = os.Setenv("PATH", binDir)
	_ = os.Setenv("HOME", paths.incusDir)
}

func connectEmbeddedServer(socket string) (incus.InstanceServer, error) {
	return incus.ConnectIncusUnix(socket, &incus.ConnectionArgs{SkipGetEvents: true})
}

func spawnEmbeddedSupervisor(paths embeddedPaths) error {
	if err := extractEmbeddedRuntime(paths); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate bocker executable: %w", err)
	}
	if err := os.MkdirAll(paths.logDir, 0o700); err != nil {
		return fmt.Errorf("create Bocker log directory: %w", err)
	}
	logPath := filepath.Join(paths.logDir, "supervisor.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open Bocker supervisor log: %w", err)
	}

	cmd := exec.Command(exe, "__daemon")
	cmd.Env = os.Environ()
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start embedded Incus supervisor: %w", err)
	}
	_ = logFile.Close()
	return cmd.Process.Release()
}

func startSystemdSupervisor(paths embeddedPaths) (bool, error) {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false, nil
	}
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		return false, nil
	}
	exe, binaryChanged, err := installSystemdSupervisorBinary(paths)
	if err != nil {
		return true, err
	}
	if strings.ContainsAny(exe, "\n\r\"%") || strings.ContainsAny(paths.stateDir, "\n\r\"%") {
		return true, errors.New("Bocker executable and state paths cannot contain quotes or newlines")
	}

	unit := embeddedSystemdUnit(exe, paths.stateDir)
	unitPath := "/etc/systemd/system/bocker.service"
	unitChanged, err := writeFileIfChanged(unitPath, []byte(unit), 0o644)
	if err != nil {
		return true, fmt.Errorf("install bocker.service: %w", err)
	}
	if unitChanged {
		cmd := exec.Command(systemctl, "daemon-reload")
		if output, err := cmd.CombinedOutput(); err != nil {
			return true, fmt.Errorf("reload systemd after installing bocker.service: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}

	active := exec.Command(systemctl, "is-active", "--quiet", "bocker.service").Run() == nil
	if active && (binaryChanged || unitChanged) {
		cmd := exec.Command(systemctl, "restart", "bocker.service")
		if output, err := cmd.CombinedOutput(); err != nil {
			return true, fmt.Errorf("restart updated bocker.service: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return true, nil
	}
	if active {
		return true, nil
	}

	_ = exec.Command(systemctl, "reset-failed", "bocker.service").Run()
	cmd := exec.Command(systemctl, "enable", "--now", "bocker.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		return true, fmt.Errorf("start bocker.service: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return true, nil
}

func installSystemdSupervisorBinary(paths embeddedPaths) (string, bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", false, err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", false, err
	}
	content, err := os.ReadFile(exe)
	if err != nil {
		return "", false, fmt.Errorf("read Bocker executable: %w", err)
	}
	target := filepath.Join(paths.stateDir, "bin", "bocker-daemon")
	changed, err := writeFileIfChanged(target, content, 0o755)
	if err != nil {
		return "", false, fmt.Errorf("install Bocker daemon executable: %w", err)
	}
	return target, changed, nil
}

func embeddedSystemdUnit(exe, stateDir string) string {
	return fmt.Sprintf(`[Unit]
Description=Bocker embedded container runtime
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
ExecStart="%s" __daemon
Environment="BOCKER_STATE_DIR=%s"
Restart=on-failure
RestartSec=2
Delegate=yes
KillMode=mixed
TimeoutStopSec=40
SendSIGKILL=yes
LimitNOFILE=1048576
TasksMax=infinity

[Install]
WantedBy=multi-user.target
`, exe, stateDir)
}

// allowSocketGroup makes only the state directory components needed to reach
// the API socket traversable by members of the Incus administration group.
// Container data and logs remain mode 0700.
func allowSocketGroup(paths embeddedPaths) error {
	group, err := user.LookupGroup(defaultSocketGroup)
	if err != nil {
		return nil
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return fmt.Errorf("parse %s group id: %w", defaultSocketGroup, err)
	}
	for _, dir := range []string{paths.stateDir, paths.incusDir} {
		if err := os.Chown(dir, 0, gid); err != nil {
			return fmt.Errorf("set %s group on %s: %w", defaultSocketGroup, dir, err)
		}
		if err := os.Chmod(dir, 0o710); err != nil {
			return fmt.Errorf("set socket traversal permission on %s: %w", dir, err)
		}
	}
	return nil
}

func ensureSocketGroupWhenReady(paths embeddedPaths) {
	group, err := user.LookupGroup(defaultSocketGroup)
	if err != nil {
		return
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return
	}
	deadline := time.Now().Add(embeddedSocketWait)
	for time.Now().Before(deadline) {
		if info, statErr := os.Stat(paths.socket); statErr == nil && info.Mode()&os.ModeSocket != 0 {
			_ = os.Chown(paths.socket, 0, gid)
			_ = os.Chmod(paths.socket, 0o660)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func writeFileIfChanged(path string, content []byte, mode os.FileMode) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + fmt.Sprintf(".tmp-%d", os.Getpid())
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

func waitForEmbeddedServer(paths embeddedPaths, timeout time.Duration) (incus.InstanceServer, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		server, err := connectEmbeddedServer(paths.socket)
		if err == nil {
			return server, nil
		}
		lastErr = err
		time.Sleep(250 * time.Millisecond)
	}

	detail := tailFile(filepath.Join(paths.logDir, "incusd.log"), 4096)
	if detail == "" {
		detail = tailFile(filepath.Join(paths.logDir, "supervisor.log"), 4096)
	}
	if detail != "" {
		return nil, fmt.Errorf("embedded Incus did not become ready: %w\n%s", lastErr, detail)
	}
	return nil, fmt.Errorf("embedded Incus did not become ready: %w", lastErr)
}

func ensureDefaultIncusConfig(server incus.InstanceServer) error {
	pools, err := server.GetStoragePoolNames()
	if err != nil {
		return fmt.Errorf("list storage pools: %w", err)
	}
	if !containsString(pools, "default") {
		if len(pools) != 0 {
			return fmt.Errorf("unexpected storage pools %v; Bocker requires the default dir pool", pools)
		}
		if err := server.CreateStoragePool(api.StoragePoolsPost{Name: "default", Driver: "dir"}); err != nil {
			return fmt.Errorf("create default dir storage pool: %w", err)
		}
	} else {
		pool, _, err := server.GetStoragePool("default")
		if err != nil {
			return fmt.Errorf("get default storage pool: %w", err)
		}
		if pool == nil {
			return fmt.Errorf("default storage pool is missing")
		}
		if pool.Driver != "dir" {
			return fmt.Errorf("default storage pool must use dir, got %q", pool.Driver)
		}
	}

	profile, etag, err := server.GetProfile("default")
	if err != nil {
		return fmt.Errorf("get default profile: %w", err)
	}
	root := profile.Devices["root"]
	if root != nil && root["type"] == "disk" && root["path"] == "/" && root["pool"] == "default" {
		if len(profile.Devices) == 1 {
			return nil
		}
	}
	devices := map[string]map[string]string{
		"root": {"type": "disk", "path": "/", "pool": "default"},
	}
	put := api.ProfilePut{
		Config:      profile.Config,
		Description: profile.Description,
		Devices:     apiDevices(devices),
	}
	if err := server.UpdateProfile("default", put, etag); err != nil {
		return fmt.Errorf("configure default profile: %w", err)
	}
	return nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func extractEmbeddedRuntime(paths embeddedPaths) error {
	marker := filepath.Join(paths.runtimeDir, ".ready")
	if _, err := os.Stat(marker); err == nil {
		return ensureLXCCompatibilityRootfs()
	}

	if err := os.MkdirAll(filepath.Dir(paths.runtimeDir), 0o700); err != nil {
		return fmt.Errorf("create runtime parent directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(paths.runtimeDir), ".extract.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open runtime extraction lock: %w", err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock runtime extraction: %w", err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN) //nolint:errcheck

	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	if err := os.MkdirAll(paths.runtimeDir, 0o700); err != nil {
		return fmt.Errorf("create embedded runtime directory: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(incusRuntimeArchive), int64(len(incusRuntimeArchive)))
	if err != nil {
		return fmt.Errorf("open embedded Incus runtime: %w", err)
	}
	for _, entry := range zr.File {
		archiveName := strings.ReplaceAll(entry.Name, "\\", "/")
		name := filepath.Clean(filepath.FromSlash(archiveName))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe path %q in embedded runtime", entry.Name)
		}
		target := filepath.Join(paths.runtimeDir, name)
		// Some ZIP writers omit the directory mode bits while retaining the
		// conventional trailing slash. Treat either signal as a directory so
		// archive extraction cannot materialize a directory entry as a file.
		if entry.FileInfo().IsDir() || strings.HasSuffix(archiveName, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractRuntimeFile(entry, archiveName, target); err != nil {
			return err
		}
	}
	if err := writeLXCFSConfig(paths); err != nil {
		return err
	}
	if err := ensureLXCCompatibilityRootfs(); err != nil {
		return err
	}

	if err := os.WriteFile(marker, []byte(embeddedIncusVersion+"\n"), 0o600); err != nil {
		return fmt.Errorf("write embedded runtime marker: %w", err)
	}
	return nil
}

// liblxc in the embedded Incus package is built with this mount staging path.
// The distribution package normally creates it during installation; Bocker
// creates the empty compatibility directory itself so no Incus package is
// required on the host.
func ensureLXCCompatibilityRootfs() error {
	const rootfsMount = "/opt/incus/lib/lxc/rootfs"
	if err := os.MkdirAll(rootfsMount, 0o755); err != nil {
		return fmt.Errorf("create liblxc rootfs staging directory: %w", err)
	}
	info, err := os.Stat(rootfsMount)
	if err != nil {
		return fmt.Errorf("check liblxc rootfs staging directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("liblxc rootfs staging path %s is not a directory", rootfsMount)
	}
	return nil
}

func writeLXCFSConfig(paths embeddedPaths) error {
	configPath := filepath.Join(paths.runtimeDir, "share", "lxc", "config", "common.conf.d", "00-lxcfs.conf")
	content := fmt.Sprintf("lxc.hook.mount = %s\nlxc.hook.post-stop = %s\n",
		filepath.Join(paths.runtimeDir, "share", "lxcfs", "lxc.mount.hook"),
		filepath.Join(paths.runtimeDir, "share", "lxcfs", "lxc.reboot.hook"),
	)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("configure embedded lxcfs hooks: %w", err)
	}
	return nil
}

func extractRuntimeFile(entry *zip.File, archiveName, target string) error {
	src, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open embedded file %s: %w", entry.Name, err)
	}
	defer src.Close()

	tmp := target + fmt.Sprintf(".tmp-%d", os.Getpid())
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("extract %s: %w", entry.Name, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", target, closeErr)
	}

	mode := os.FileMode(0o644)
	if strings.HasPrefix(archiveName, "bin/") || strings.Contains(archiveName, "/hooks/") || strings.HasPrefix(archiveName, "share/lxcfs/") {
		mode = 0o755
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod %s: %w", target, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install %s: %w", target, err)
	}
	return nil
}

// runEmbeddedDaemonSupervisor is entered through the private __daemon command.
// It keeps the daemon tied to one long-lived Bocker process so systemd can
// supervise it without installing an Incus service or binary.
func runEmbeddedDaemonSupervisor() error {
	if runtime.GOARCH != "amd64" {
		return fmt.Errorf("embedded runtime does not support linux/%s", runtime.GOARCH)
	}
	if os.Geteuid() != 0 {
		return errors.New("the Bocker daemon must run as root; use the normal bocker CLI as an unprivileged user")
	}
	paths, err := embeddedRuntimePaths()
	if err != nil {
		return err
	}
	setEmbeddedEnvironment(paths)
	if err := extractEmbeddedRuntime(paths); err != nil {
		return err
	}
	for _, dir := range []string{paths.incusDir, paths.logDir, paths.lxcfsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := allowSocketGroup(paths); err != nil {
		return err
	}

	lock, err := os.OpenFile(filepath.Join(paths.stateDir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon lock: %w", err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil
		}
		return fmt.Errorf("lock daemon: %w", err)
	}

	lxcfs := startEmbeddedLXCFS(paths)
	if lxcfs != nil {
		defer stopAndReapProcess(lxcfs.Process)
	}

	logFile, err := os.OpenFile(filepath.Join(paths.logDir, "incusd.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	incusdPath := filepath.Join(paths.runtimeDir, "bin", "incusd")
	cmdArgs := []string{"--logfile", filepath.Join(paths.logDir, "incusd-internal.log")}
	if _, err := user.LookupGroup(defaultSocketGroup); err == nil {
		cmdArgs = append(cmdArgs, "--group", defaultSocketGroup)
	}
	cmd := exec.Command(incusdPath, cmdArgs...)
	cmd.Env = os.Environ()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start embedded incusd: %w", err)
	}
	// Incus creates the socket asynchronously.  The --group flag above is the
	// primary authorization mechanism; this also fixes access when reusing a
	// socket left by an older daemon.
	go ensureSocketGroupWhenReady(paths)
	control, err := startBockerControl(paths)
	if err != nil {
		stopProcess(cmd.Process)
		return err
	}
	defer control.Close()
	if err := os.WriteFile(filepath.Join(paths.stateDir, "daemon.pid"), []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0o600); err != nil {
		stopProcess(cmd.Process)
		return err
	}
	defer os.Remove(filepath.Join(paths.stateDir, "daemon.pid")) //nolint:errcheck

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(sigCh)
	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()

	select {
	case sig := <-sigCh:
		signalValue, ok := sig.(syscall.Signal)
		if !ok {
			signalValue = syscall.SIGTERM
		}
		requestEmbeddedDaemonShutdown(paths.socket)
		select {
		case err := <-exitCh:
			return normalizeDaemonExit(err)
		case <-time.After(daemonGracefulStop):
			signalProcessGroup(cmd.Process, signalValue)
		}
		select {
		case err := <-exitCh:
			return normalizeDaemonExit(err)
		case <-time.After(daemonTermStop):
			signalProcessGroup(cmd.Process, syscall.SIGKILL)
		}
		select {
		case err := <-exitCh:
			return normalizeDaemonExit(err)
		case <-time.After(daemonKillWait):
			return errors.New("embedded incusd did not stop after SIGKILL")
		}
	case err := <-exitCh:
		return normalizeDaemonExit(err)
	}
}

func startEmbeddedLXCFS(paths embeddedPaths) *exec.Cmd {
	if _, err := os.Stat("/dev/fuse"); err != nil {
		return nil
	}
	path := filepath.Join(paths.runtimeDir, "bin", "lxcfs")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	logFile, err := os.OpenFile(filepath.Join(paths.logDir, "lxcfs.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	cmd := exec.Command(path, paths.lxcfsDir)
	cmd.Env = os.Environ()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil
	}
	_ = logFile.Close()
	return cmd
}

func stopProcess(process *os.Process) {
	if process == nil {
		return
	}
	signalProcessGroup(process, syscall.SIGTERM)
	for i := 0; i < 20; i++ {
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	signalProcessGroup(process, syscall.SIGKILL)
}

func requestEmbeddedDaemonShutdown(socket string) {
	server, err := connectEmbeddedServer(socket)
	if err != nil {
		return
	}
	go func() {
		_, _, _ = server.RawQuery("PUT", "/internal/shutdown?force=true", nil, "")
	}()
}

func signalProcessGroup(process *os.Process, signal syscall.Signal) {
	if process == nil || process.Pid <= 0 {
		return
	}
	if err := syscall.Kill(-process.Pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		_ = process.Signal(signal)
	}
}

func stopAndReapProcess(process *os.Process) {
	stopProcess(process)
	if process != nil {
		_, _ = process.Wait()
	}
}

func normalizeDaemonExit(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() &&
			(status.Signal() == syscall.SIGINT || status.Signal() == syscall.SIGTERM || status.Signal() == syscall.SIGQUIT) {
			return nil
		}
	}
	return fmt.Errorf("embedded incusd exited: %w", err)
}

func tailFile(path string, limit int) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	if len(data) > limit {
		data = data[len(data)-limit:]
	}
	return strings.TrimSpace(string(data))
}
