//go:build linux

package bocker

import (
	"bufio"
	"os"
	"strings"
	"testing"
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
