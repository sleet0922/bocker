package bocker

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	guiHelperChildEnv = "BOCKER_GUI_HELPER_CHILD"
	guiHelperMaxInput = 1 << 20
	guiHelperProtocol = 4
)

type guiHelperRequest struct {
	Arguments   []string `json:"arguments"`
	Interactive bool     `json:"interactive,omitempty"`
	Width       int      `json:"width,omitempty"`
	Height      int      `json:"height,omitempty"`
	Term        string   `json:"term,omitempty"`
}

type guiHelperResponse struct {
	OK       bool   `json:"ok"`
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
	Protocol int    `json:"protocol"`
}

// runGUIHelper starts the GUI's short bootstrap process or the detached root
// helper. The Unix socket is owned by the desktop user, so only the user who
// authorized pkexec can use the helper.
func runGUIHelper(args []string) error {
	socketPath, err := guiHelperSocketPath(args)
	if err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("必须通过 pkexec 启动 GUI 权限代理")
	}
	if os.Getenv(guiHelperChildEnv) != "1" {
		return startGUIHelperChild()
	}
	return serveGUIHelper(socketPath)
}

func guiHelperSocketPath(args []string) (string, error) {
	if len(args) != 2 || args[0] != "--socket" {
		return "", fmt.Errorf("用法: __gui_helper --socket <path>")
	}
	path := strings.TrimSpace(args[1])
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("socket 路径必须是绝对路径")
	}
	return filepath.Clean(path), nil
}

func startGUIHelperChild() error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("读取当前 bocker 路径失败: %w", err)
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("打开 %s 失败: %w", os.DevNull, err)
	}
	defer null.Close()
	env := append([]string{}, os.Environ()...)
	env = append(env, guiHelperChildEnv+"=1")
	childArgs := append([]string{binary}, os.Args[1:]...)
	process, err := os.StartProcess(binary, childArgs, &os.ProcAttr{
		Env:   env,
		Files: []*os.File{null, null, null},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	})
	if err != nil {
		return fmt.Errorf("启动 GUI 权限代理失败: %w", err)
	}
	return process.Release()
}

func serveGUIHelper(socketPath string) error {
	uid, gid, err := guiSocketOwner(socketPath)
	if err != nil {
		return err
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理旧 socket 失败: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("创建 socket 失败: %w", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()
	if err := os.Chown(socketPath, uid, gid); err != nil {
		return fmt.Errorf("设置 socket 所有者失败: %w", err)
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		return fmt.Errorf("设置 socket 权限失败: %w", err)
	}
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("读取当前 bocker 路径失败: %w", err)
	}
	for {
		connection, err := listener.Accept()
		if err != nil {
			return err
		}
		go handleGUIHelperConnection(connection, binary)
	}
}

func guiSocketOwner(socketPath string) (int, int, error) {
	dir := filepath.Dir(socketPath)
	info, err := os.Stat(dir)
	if err != nil {
		return 0, 0, fmt.Errorf("读取 socket 目录失败: %w", err)
	}
	if !info.IsDir() {
		return 0, 0, fmt.Errorf("socket 父路径不是目录: %s", dir)
	}
	if info.Mode().Perm()&0077 != 0 {
		return 0, 0, fmt.Errorf("socket 目录必须仅对当前用户可访问: %s", dir)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("无法读取 socket 目录所有者")
	}
	return int(stat.Uid), int(stat.Gid), nil
}

func handleGUIHelperConnection(connection net.Conn, binary string) {
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReaderSize(connection, guiHelperMaxInput)
	line, err := reader.ReadSlice('\n')
	if err != nil {
		writeGUIHelperResponse(connection, guiHelperResponse{Output: "读取 GUI 请求失败: " + err.Error(), ExitCode: 1})
		return
	}
	var request guiHelperRequest
	if err := json.Unmarshal(line, &request); err != nil {
		writeGUIHelperResponse(connection, guiHelperResponse{Output: "GUI 请求格式无效: " + err.Error(), ExitCode: 1})
		return
	}
	_ = connection.SetReadDeadline(time.Time{})
	if request.Interactive {
		handleGUIInteractiveConnection(connection, reader, binary, request)
		return
	}
	if err := validateGUIHelperArguments(request.Arguments); err != nil {
		writeGUIHelperResponse(connection, guiHelperResponse{Output: err.Error(), ExitCode: 1})
		return
	}
	command := exec.Command(binary, request.Arguments...)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err = command.Run()
	response := guiHelperResponse{OK: err == nil, Output: strings.TrimSpace(output.String())}
	if exitErr, ok := err.(*exec.ExitError); ok {
		response.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		response.ExitCode = 1
		if response.Output == "" {
			response.Output = err.Error()
		}
	}
	writeGUIHelperResponse(connection, response)
}

func handleGUIInteractiveConnection(connection net.Conn, reader io.Reader, binary string, request guiHelperRequest) {
	args := request.Arguments
	if len(args) != 3 || args[0] != "container" || args[1] != "shell" {
		_, _ = fmt.Fprintln(connection, "GUI 交互终端请求无效")
		return
	}
	if err := validateBockerName(args[2]); err != nil {
		_, _ = fmt.Fprintf(connection, "容器名称无效: %v\n", err)
		return
	}
	command := exec.Command(binary, args...)
	command.Env = os.Environ()
	if request.Width > 0 && request.Height > 0 {
		command.Env = setEnvironmentValue(command.Env, "BOCKER_TERM_WIDTH", strconv.Itoa(request.Width))
		command.Env = setEnvironmentValue(command.Env, "BOCKER_TERM_HEIGHT", strconv.Itoa(request.Height))
	}
	if strings.TrimSpace(request.Term) != "" {
		command.Env = setEnvironmentValue(command.Env, "TERM", strings.TrimSpace(request.Term))
	}
	command.Stdin = reader
	command.Stdout = connection
	command.Stderr = connection
	if err := command.Run(); err != nil {
		_, _ = fmt.Fprintf(connection, "\r\n容器终端已断开: %v\r\n", err)
	}
}

func setEnvironmentValue(environment []string, key, value string) []string {
	prefix := key + "="
	filtered := environment[:0]
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, prefix+value)
}

// runGUIShellClient is the unprivileged half of an interactive GUI terminal.
// It forwards the local terminal to the already-authorized root helper.
func runGUIShellClient(args []string) error {
	if len(args) != 3 || args[0] != "--socket" {
		return fmt.Errorf("用法: __gui_shell --socket <path> <container>")
	}
	socketPath := filepath.Clean(strings.TrimSpace(args[1]))
	if !filepath.IsAbs(socketPath) {
		return fmt.Errorf("socket 路径必须是绝对路径")
	}
	name := strings.TrimSpace(args[2])
	if err := validateBockerName(name); err != nil {
		return fmt.Errorf("容器名称无效: %w", err)
	}
	connection, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("连接 GUI 权限代理失败: %w", err)
	}
	defer connection.Close()
	fd := int(os.Stdin.Fd())
	width, height := 120, 40
	if w, h, sizeErr := term.GetSize(fd); sizeErr == nil && w > 0 && h > 0 {
		width, height = w, h
	}
	request := guiHelperRequest{
		Arguments:   []string{"container", "shell", name},
		Interactive: true,
		Width:       width,
		Height:      height,
		Term:        os.Getenv("TERM"),
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return fmt.Errorf("发送终端请求失败: %w", err)
	}

	if old, err := makeRaw(fd); err == nil {
		defer restoreTerm(fd, old)
	}
	go func() {
		_, _ = io.Copy(connection, os.Stdin)
	}()
	if _, err := io.Copy(os.Stdout, connection); err != nil {
		return fmt.Errorf("终端连接中断: %w", err)
	}
	return nil
}

func validateGUIHelperArguments(args []string) error {
	if len(args) == 0 || len(args) > 128 {
		return fmt.Errorf("GUI 命令参数数量无效")
	}
	for _, arg := range args {
		if len(arg) > 16*1024 || strings.ContainsRune(arg, 0) {
			return fmt.Errorf("GUI 命令包含无效参数")
		}
	}
	switch args[0] {
	case "template", "image", "container":
		return nil
	default:
		return fmt.Errorf("GUI 不支持命令: %s", args[0])
	}
}

func writeGUIHelperResponse(connection net.Conn, response guiHelperResponse) {
	response.Protocol = guiHelperProtocol
	_ = json.NewEncoder(connection).Encode(response)
}
