//go:build linux

package bocker

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const privilegedChildEnv = "BOCKER_PRIVILEGED_CHILD"

const controlMaxInput = 1 << 20

type bockerControlRequest struct {
	Arguments        []string          `json:"arguments"`
	Environment      map[string]string `json:"environment,omitempty"`
	WorkingDirectory string            `json:"workingDirectory,omitempty"`
}

type bockerControlResponse struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
}

type bockerControlServer struct {
	listener net.Listener
	close    sync.Once
}

func startBockerControl(paths embeddedPaths) (*bockerControlServer, error) {
	if err := os.Remove(paths.control); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("清理旧 Bocker 控制 socket 失败: %w", err)
	}
	listener, err := net.Listen("unix", paths.control)
	if err != nil {
		return nil, fmt.Errorf("创建 Bocker 控制 socket 失败: %w", err)
	}
	if err := authorizeBockerSocket(paths.control); err != nil {
		_ = listener.Close()
		_ = os.Remove(paths.control)
		return nil, err
	}
	server := &bockerControlServer{listener: listener}
	go server.serve()
	return server, nil
}

func authorizeBockerSocket(path string) error {
	if err := os.Chmod(path, 0o666); err != nil {
		return fmt.Errorf("设置 Bocker 控制 socket 权限失败: %w", err)
	}
	return nil
}

func (s *bockerControlServer) serve() {
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go handleBockerControlConnection(connection)
	}
}

func (s *bockerControlServer) Close() {
	if s == nil {
		return
	}
	s.close.Do(func() {
		_ = s.listener.Close()
		_ = os.Remove(s.listener.Addr().String())
	})
}

func handleBockerControlConnection(connection net.Conn) {
	defer connection.Close()
	reader := bufio.NewReaderSize(connection, controlMaxInput)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		writeBockerControlResponse(connection, bockerControlResponse{Output: "读取 Bocker 控制请求失败: " + err.Error(), ExitCode: 1})
		return
	}
	var request bockerControlRequest
	if err := json.Unmarshal(line, &request); err != nil {
		writeBockerControlResponse(connection, bockerControlResponse{Output: "Bocker 控制请求格式无效: " + err.Error(), ExitCode: 1})
		return
	}
	if err := validatePrivilegedArguments(request.Arguments); err != nil {
		writeBockerControlResponse(connection, bockerControlResponse{Output: err.Error(), ExitCode: 1})
		return
	}
	if request.WorkingDirectory != "" {
		if !filepath.IsAbs(request.WorkingDirectory) || strings.ContainsRune(request.WorkingDirectory, 0) {
			writeBockerControlResponse(connection, bockerControlResponse{Output: "Bocker 控制工作目录无效", ExitCode: 1})
			return
		}
	}
	binary, err := os.Executable()
	if err != nil {
		writeBockerControlResponse(connection, bockerControlResponse{Output: "读取 Bocker 守护进程路径失败: " + err.Error(), ExitCode: 1})
		return
	}
	command := exec.Command(binary, request.Arguments...)
	command.Dir = request.WorkingDirectory
	command.Env = append(os.Environ(), privilegedChildEnv+"=1")
	for key, value := range request.Environment {
		if !allowedBrokerEnvironment(key) {
			continue
		}
		command.Env = setControlEnvironmentValue(command.Env, key, value)
	}
	var output strings.Builder
	command.Stdout = &output
	command.Stderr = &output
	runErr := command.Run()
	response := bockerControlResponse{Output: output.String()}
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		response.ExitCode = exitErr.ExitCode()
	} else if runErr != nil {
		response.ExitCode = 1
		if response.Output == "" {
			response.Output = runErr.Error()
		}
	}
	writeBockerControlResponse(connection, response)
}

func validatePrivilegedArguments(args []string) error {
	if len(args) == 0 || len(args) > 128 {
		return fmt.Errorf("Bocker 控制命令参数数量无效")
	}
	for _, arg := range args {
		if len(arg) > 16*1024 || strings.ContainsRune(arg, 0) {
			return fmt.Errorf("Bocker 控制命令包含无效参数")
		}
	}
	switch args[0] {
	case "template", "image", "container":
		return nil
	default:
		return fmt.Errorf("Bocker 控制不支持命令: %s", args[0])
	}
}

func writeBockerControlResponse(connection net.Conn, response bockerControlResponse) {
	_ = json.NewEncoder(connection).Encode(response)
}

// shouldUsePrivilegedBroker leaves terminal-oriented commands in the caller so
// stdin/stdout and PTY behavior remain native. Other explicit operations are
// run by the privileged daemon and can therefore update host networking/files.
func shouldUsePrivilegedBroker(args []string) bool {
	return os.Geteuid() != 0 && os.Getenv(privilegedChildEnv) != "1" && brokerCommandClassification(args)
}

func runPrivilegedOperation(args []string) (bool, error) {
	if os.Geteuid() == 0 || os.Getenv(privilegedChildEnv) == "1" || !brokerCommandClassification(args) {
		return false, nil
	}
	exitCode, err := runPrivilegedBrokerCommand(args)
	if err != nil {
		return true, err
	}
	if exitCode != 0 {
		return true, fmt.Errorf("Bocker 后台命令退出码 %d", exitCode)
	}
	return true, nil
}

func brokerCommandClassification(args []string) bool {
	if len(args) < 2 {
		return false
	}
	if args[0] != "template" && args[0] != "image" && args[0] != "container" {
		return false
	}
	if args[0] == "container" {
		switch args[1] {
		case "shell", "exec", "export":
			return false
		case "import":
			return hasPositionalArgument(args, 2)
		case "set":
			return len(args) >= 4 && (args[3] == "domain" || args[3] == "network")
		case "start", "stop", "restart", "remove":
			return hasPositionalArgument(args, 2)
		}
	}
	if args[0] == "image" && args[1] == "run" {
		return hasPositionalArgument(args, 2)
	}
	if args[0] == "template" && args[1] == "install" {
		return hasPositionalArgument(args, 2)
	}
	return args[1] == "build"
}

func hasPositionalArgument(args []string, index int) bool {
	return len(args) > index && !strings.HasPrefix(args[index], "-")
}

func runPrivilegedBrokerCommand(args []string) (int, error) {
	paths, err := embeddedRuntimePaths()
	if err != nil {
		return 1, err
	}
	connection, err := net.Dial("unix", paths.control)
	if err != nil {
		return 1, fmt.Errorf("无法连接 Bocker 后台控制 socket（请确认 bocker.service 已启动）: %w", err)
	}
	defer connection.Close()
	workingDirectory, err := os.Getwd()
	if err != nil {
		return 1, fmt.Errorf("读取当前工作目录失败: %w", err)
	}
	environment := make(map[string]string)
	for _, key := range []string{networkModeEnv, bridgeParentEnv, natNetworkCIDREnv, natNetworkIPv6CIDREnv, hostShimCIDREnv} {
		if value, ok := os.LookupEnv(key); ok {
			environment[key] = value
		}
	}
	if err := json.NewEncoder(connection).Encode(bockerControlRequest{Arguments: args, Environment: environment, WorkingDirectory: workingDirectory}); err != nil {
		return 1, fmt.Errorf("发送 Bocker 控制请求失败: %w", err)
	}
	responseData, err := io.ReadAll(connection)
	if err != nil {
		return 1, fmt.Errorf("读取 Bocker 控制响应失败: %w", err)
	}
	var response bockerControlResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		return 1, fmt.Errorf("Bocker 控制响应格式无效: %w", err)
	}
	if response.Output != "" {
		_, _ = fmt.Fprint(os.Stdout, response.Output)
	}
	return response.ExitCode, nil
}

func allowedBrokerEnvironment(key string) bool {
	switch key {
	case networkModeEnv, bridgeParentEnv, natNetworkCIDREnv, natNetworkIPv6CIDREnv, hostShimCIDREnv:
		return true
	default:
		return false
	}
}

func setControlEnvironmentValue(environment []string, key, value string) []string {
	prefix := key + "="
	filtered := environment[:0]
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, prefix+value)
}
