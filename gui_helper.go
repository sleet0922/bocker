package main

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
	"strings"
	"syscall"
	"time"
)

const (
	guiHelperChildEnv = "BOCKER_GUI_HELPER_CHILD"
	guiHelperMaxInput = 1 << 20
)

type guiHelperRequest struct {
	Arguments []string `json:"arguments"`
}

type guiHelperResponse struct {
	OK       bool   `json:"ok"`
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
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
	reader := bufio.NewReader(io.LimitReader(connection, guiHelperMaxInput))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		writeGUIHelperResponse(connection, guiHelperResponse{Output: "读取 GUI 请求失败: " + err.Error(), ExitCode: 1})
		return
	}
	var request guiHelperRequest
	if err := json.Unmarshal(line, &request); err != nil {
		writeGUIHelperResponse(connection, guiHelperResponse{Output: "GUI 请求格式无效: " + err.Error(), ExitCode: 1})
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
	case "list", "ls", "images", "image", "start", "stop", "restart", "exec", "set", "export", "import", "install", "i", "remove", "rm", "build", "create", "run":
		return nil
	default:
		return fmt.Errorf("GUI 不支持命令: %s", args[0])
	}
}

func writeGUIHelperResponse(connection net.Conn, response guiHelperResponse) {
	_ = json.NewEncoder(connection).Encode(response)
}
