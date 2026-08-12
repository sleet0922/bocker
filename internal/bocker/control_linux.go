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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const privilegedChildEnv = "BOCKER_PRIVILEGED_CHILD"

const (
	callerUIDEnv      = "BOCKER_CALLER_UID"
	callerGIDEnv      = "BOCKER_CALLER_GID"
	terminalWidthEnv  = "BOCKER_TERM_WIDTH"
	terminalHeightEnv = "BOCKER_TERM_HEIGHT"
)

const (
	controlMaxInput       = 1 << 20
	controlMaxConnections = 32
	controlRequestTimeout = 30 * time.Second
	controlWriteTimeout   = 30 * time.Second
)

type bockerControlRequest struct {
	Arguments        []string          `json:"arguments"`
	Environment      map[string]string `json:"environment,omitempty"`
	WorkingDirectory string            `json:"workingDirectory,omitempty"`
	CallerUID        int               `json:"callerUID,omitempty"`
	CallerGID        int               `json:"callerGID,omitempty"`
	Terminal         bool              `json:"terminal,omitempty"`
}

type bockerControlResponse struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
	Done     bool   `json:"done,omitempty"`
}

type bockerControlServer struct {
	listener    net.Listener
	close       sync.Once
	connections chan struct{}
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
	server := &bockerControlServer{listener: listener, connections: make(chan struct{}, controlMaxConnections)}
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
		select {
		case s.connections <- struct{}{}:
			go func() {
				defer func() { <-s.connections }()
				handleBockerControlConnection(connection)
			}()
		default:
			_ = connection.SetWriteDeadline(time.Now().Add(controlWriteTimeout))
			writeBockerControlResponse(connection, bockerControlResponse{Output: "Bocker 控制服务繁忙，请稍后重试", ExitCode: 1})
			_ = connection.Close()
		}
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
	_ = connection.SetReadDeadline(time.Now().Add(controlRequestTimeout))
	reader := bufio.NewReaderSize(connection, 64*1024)
	line, err := readBockerControlRequest(reader)
	if err != nil {
		writeBockerControlResponse(connection, bockerControlResponse{Output: "读取 Bocker 控制请求失败: " + err.Error(), ExitCode: 1})
		return
	}
	_ = connection.SetReadDeadline(time.Time{})
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
	command.Env = setControlEnvironmentValue(command.Env, callerUIDEnv, strconv.Itoa(request.CallerUID))
	command.Env = setControlEnvironmentValue(command.Env, callerGIDEnv, strconv.Itoa(request.CallerGID))
	for key, value := range request.Environment {
		if !allowedBrokerEnvironment(key) {
			continue
		}
		command.Env = setControlEnvironmentValue(command.Env, key, value)
	}
	stream := &bockerControlOutputStream{connection: connection}
	var processState *os.ProcessState
	if request.Terminal {
		processState, err = runBockerTerminalCommand(connection, command, reader, stream, request)
	} else {
		processState, err = runBockerPipeCommand(connection, command, reader, stream)
	}
	response := bockerControlResponse{Done: true}
	if processState != nil {
		response.ExitCode = processState.ExitCode()
	}
	if err != nil && processState == nil {
		response.ExitCode = 1
		response.Output = err.Error()
	}
	writeBockerControlResponse(connection, response)
}

func readBockerControlRequest(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > controlMaxInput {
			return nil, fmt.Errorf("Bocker 控制请求超过 %d 字节限制", controlMaxInput)
		}
		line = append(line, fragment...)
		if err == nil {
			return line, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
	}
}

func runBockerPipeCommand(connection net.Conn, command *exec.Cmd, reader io.Reader, stream io.Writer) (*os.ProcessState, error) {
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 Bocker 控制 stdin 失败: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("创建 Bocker 控制 stdout 失败: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("创建 Bocker 控制 stderr 失败: %w", err)
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("启动 Bocker 控制命令失败: %w", err)
	}
	var outputWG sync.WaitGroup
	outputWG.Add(3)
	go func() {
		defer outputWG.Done()
		_, _ = io.Copy(stdin, reader)
		_ = stdin.Close()
	}()
	go func() {
		defer outputWG.Done()
		_, _ = io.Copy(stream, stdout)
		_ = stdout.Close()
	}()
	go func() {
		defer outputWG.Done()
		_, _ = io.Copy(stream, stderr)
		_ = stderr.Close()
	}()
	processState, waitErr := command.Process.Wait()
	closeBockerControlInput(connection)
	_ = stdin.Close()
	outputWG.Wait()
	return processState, waitErr
}

func runBockerTerminalCommand(connection net.Conn, command *exec.Cmd, reader io.Reader, stream io.Writer, request bockerControlRequest) (*os.ProcessState, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, fmt.Errorf("创建 Bocker 控制终端失败: %w", err)
	}
	defer master.Close()
	command.Stdin = slave
	command.Stdout = slave
	command.Stderr = slave
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if width, height := brokerTerminalSize(request.Environment); width > 0 && height > 0 {
		_ = pty.Setsize(master, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	}
	if err := command.Start(); err != nil {
		_ = slave.Close()
		return nil, fmt.Errorf("启动 Bocker 控制终端命令失败: %w", err)
	}
	var outputWG sync.WaitGroup
	outputWG.Add(2)
	go func() {
		defer outputWG.Done()
		_, _ = io.Copy(master, reader)
	}()
	go func() {
		defer outputWG.Done()
		_, _ = io.Copy(stream, master)
	}()
	processState, waitErr := command.Process.Wait()
	closeBockerControlInput(connection)
	_ = slave.Close()
	outputWG.Wait()
	return processState, waitErr
}

func brokerTerminalSize(environment map[string]string) (int, int) {
	width, widthErr := strconv.Atoi(environment[terminalWidthEnv])
	height, heightErr := strconv.Atoi(environment[terminalHeightEnv])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 120, 40
	}
	return width, height
}

func closeBockerControlInput(connection net.Conn) {
	if unixConnection, ok := connection.(*net.UnixConn); ok {
		_ = unixConnection.CloseRead()
	}
}

// bockerControlOutputStream forwards child output as newline-delimited JSON
// messages so long-running builds remain visibly active in the caller.
type bockerControlOutputStream struct {
	connection net.Conn
	mu         sync.Mutex
}

func (s *bockerControlOutputStream) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connection.SetWriteDeadline(time.Now().Add(controlWriteTimeout)); err != nil {
		return 0, err
	}
	err := json.NewEncoder(s.connection).Encode(bockerControlResponse{Output: string(data)})
	_ = s.connection.SetWriteDeadline(time.Time{})
	if err != nil {
		return 0, err
	}
	return len(data), nil
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
	response.Done = true
	_ = connection.SetWriteDeadline(time.Now().Add(controlWriteTimeout))
	_ = json.NewEncoder(connection).Encode(response)
	_ = connection.SetWriteDeadline(time.Time{})
}

// shouldUsePrivilegedBroker routes every public resource command through the
// root-owned backend. The backend also carries stdin/stdout for menus and
// terminal-oriented commands.
func shouldUsePrivilegedBroker(args []string) bool {
	return os.Geteuid() != 0 && os.Getenv(privilegedChildEnv) != "1" && brokerCommandClassification(args)
}

func brokerCommandClassification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "template", "image", "container":
		return true
	default:
		return false
	}
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
		workingDirectory, err = os.UserHomeDir()
		if err != nil {
			return 1, fmt.Errorf("读取当前工作目录和用户主目录均失败: %w", err)
		}
		if !filepath.IsAbs(workingDirectory) {
			return 1, fmt.Errorf("用户主目录不是绝对路径: %q", workingDirectory)
		}
	}
	environment := make(map[string]string)
	for _, key := range []string{networkModeEnv, bridgeParentEnv, natNetworkCIDREnv, natNetworkIPv6CIDREnv, hostShimCIDREnv, terminalWidthEnv, terminalHeightEnv} {
		if value, ok := os.LookupEnv(key); ok {
			environment[key] = value
		}
	}
	terminalFD := int(os.Stdin.Fd())
	terminal := term.IsTerminal(terminalFD) && term.IsTerminal(int(os.Stdout.Fd()))
	if terminal {
		if width, height, sizeErr := term.GetSize(terminalFD); sizeErr == nil && width > 0 && height > 0 {
			environment[terminalWidthEnv] = strconv.Itoa(width)
			environment[terminalHeightEnv] = strconv.Itoa(height)
		}
	}
	var terminalState *term.State
	if terminal {
		terminalState, err = term.MakeRaw(terminalFD)
		if err != nil {
			return 1, fmt.Errorf("设置终端模式失败: %w", err)
		}
		defer term.Restore(terminalFD, terminalState)
	}
	if err := json.NewEncoder(connection).Encode(bockerControlRequest{
		Arguments: args, Environment: environment, WorkingDirectory: workingDirectory,
		CallerUID: os.Getuid(), CallerGID: os.Getgid(), Terminal: terminal,
	}); err != nil {
		return 1, fmt.Errorf("发送 Bocker 控制请求失败: %w", err)
	}
	go func() {
		_, _ = io.Copy(connection, os.Stdin)
	}()
	decoder := json.NewDecoder(connection)
	for {
		var response bockerControlResponse
		if err := decoder.Decode(&response); err != nil {
			if errors.Is(err, io.EOF) {
				return 1, fmt.Errorf("Bocker 后台服务在命令完成前关闭了连接")
			}
			return 1, fmt.Errorf("读取 Bocker 控制响应失败: %w", err)
		}
		if response.Output != "" {
			_, _ = fmt.Fprint(os.Stdout, response.Output)
		}
		if response.Done {
			return response.ExitCode, nil
		}
	}
}

func allowedBrokerEnvironment(key string) bool {
	switch key {
	case networkModeEnv, bridgeParentEnv, natNetworkCIDREnv, natNetworkIPv6CIDREnv, hostShimCIDREnv:
		return true
	case terminalWidthEnv, terminalHeightEnv:
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
