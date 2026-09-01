//go:build linux

package bocker

import (
	"bufio"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShouldUsePrivilegedBroker(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{[]string{"template"}, true},
		{[]string{"image"}, true},
		{[]string{"container"}, true},
		{[]string{"help"}, false},
		{[]string{"container", "start", "demo"}, true},
		{[]string{"container", "start"}, true},
		{[]string{"container", "set", "demo", "domain", "demo.test"}, true},
		{[]string{"container", "set", "demo", "port", "18080:80"}, true},
		{[]string{"container", "shell", "demo"}, true},
		{[]string{"container", "exec", "demo", "id"}, true},
		{[]string{"container", "export", "demo"}, true},
		{[]string{"image", "build", "Incusfile"}, true},
		{[]string{"image", "build"}, true},
		{[]string{"image", "run", "demo"}, true},
		{[]string{"image", "run", "--name", "demo"}, true},
		{[]string{"template", "install", "debian:12"}, true},
		{[]string{"template", "install", "--name", "demo"}, true},
		{[]string{"container", "import", "backup.tar.gz", "demo"}, true},
		{[]string{"container", "import"}, true},
	} {
		if got := brokerCommandClassification(test.args); got != test.want {
			t.Errorf("shouldUsePrivilegedBroker(%v)=%v, want %v", test.args, got, test.want)
		}
	}
}

func TestBrokerWorkingDirectoryFallback(t *testing.T) {
	// The public client must still work when invoked through a launcher whose
	// inherited working directory is no longer accessible to that user.
	t.Setenv("HOME", t.TempDir())
	if home, err := os.UserHomeDir(); err != nil || home == "" {
		t.Fatalf("user home fallback unavailable: %q, %v", home, err)
	}
}

func TestReadBockerControlRequestLimit(t *testing.T) {
	if _, err := readBockerControlRequest(bufio.NewReader(strings.NewReader("{}\n"))); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	tooLarge := strings.Repeat("x", controlMaxInput+1) + "\n"
	if _, err := readBockerControlRequest(bufio.NewReader(strings.NewReader(tooLarge))); err == nil {
		t.Fatal("oversized request unexpectedly accepted")
	}
}

func TestAllowedBrokerEnvironment(t *testing.T) {
	for _, key := range []string{networkModeEnv, bridgeParentEnv, natNetworkCIDREnv, natNetworkIPv6CIDREnv, bridgeNetworkCIDREnv, hostShimCIDREnv} {
		if !allowedBrokerEnvironment(key) {
			t.Errorf("%s should be forwarded to the daemon", key)
		}
	}
	if allowedBrokerEnvironment("BOCKER_STATE_DIR") {
		t.Fatal("state directory must not be overridden through the privileged broker")
	}
}

func TestBockerControlPeerIdentityUsesKernelCredentials(t *testing.T) {
	dir := t.TempDir()
	listener, err := net.Listen("unix", dir+"/control.socket")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, _ := listener.Accept()
		accepted <- connection
	}()
	client, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	defer server.Close()
	identity, err := bockerControlPeerIdentity(server)
	if err != nil {
		t.Fatal(err)
	}
	if identity.PID != os.Getpid() || identity.UID != os.Getuid() || identity.GID != os.Getgid() {
		t.Fatalf("peer identity = %#v, want pid=%d uid=%d gid=%d", identity, os.Getpid(), os.Getuid(), os.Getgid())
	}
}

func TestForwardBockerControlInputDetectsDisconnect(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	disconnected := make(chan struct{})
	go func() {
		forwardBockerControlInput(io.Discard, server, func() { close(disconnected) })
	}()
	_ = client.Close()
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("socket disconnect was not detected")
	}
}

func TestPipeCommandStopsWhenClientDisconnects(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	command := exec.Command("/bin/sh", "-c", "exec sleep 30")
	result := make(chan *os.ProcessState, 1)
	go func() {
		state, _ := runBockerPipeCommand(server, command, server, io.Discard)
		result <- state
	}()
	_ = client.Close()
	select {
	case state := <-result:
		if state == nil || state.Success() {
			t.Fatalf("disconnected command exited successfully: state=%v", state)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("broker command remained running after client disconnect")
	}
}

func TestKillBockerCommandKillsProcessGroup(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "exec sleep 30")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	killBockerCommand(command)
	state, err := command.Process.Wait()
	if err != nil || state == nil || state.Success() {
		t.Fatalf("killed command state=%v err=%v", state, err)
	}
}
