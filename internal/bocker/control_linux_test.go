//go:build linux

package bocker

import "testing"

func TestShouldUsePrivilegedBroker(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{[]string{"container", "start", "demo"}, true},
		{[]string{"container", "set", "demo", "domain", "demo.test"}, true},
		{[]string{"container", "set", "demo", "port", "18080:80"}, false},
		{[]string{"container", "shell", "demo"}, false},
		{[]string{"container", "exec", "demo", "id"}, false},
		{[]string{"container", "export", "demo"}, false},
		{[]string{"image", "build", "Incusfile"}, false},
		{[]string{"image", "run", "demo"}, true},
	} {
		if got := brokerCommandClassification(test.args); got != test.want {
			t.Errorf("shouldUsePrivilegedBroker(%v)=%v, want %v", test.args, got, test.want)
		}
	}
}

func TestAllowedBrokerEnvironment(t *testing.T) {
	for _, key := range []string{networkModeEnv, bridgeParentEnv, natNetworkCIDREnv, natNetworkIPv6CIDREnv, hostShimCIDREnv} {
		if !allowedBrokerEnvironment(key) {
			t.Errorf("%s should be forwarded to the daemon", key)
		}
	}
	if allowedBrokerEnvironment("BOCKER_STATE_DIR") {
		t.Fatal("state directory must not be overridden through the privileged broker")
	}
}
