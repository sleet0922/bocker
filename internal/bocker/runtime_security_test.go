package bocker

import (
	"strings"
	"testing"
)

func TestApplyContainerSecurity(t *testing.T) {
	config := map[string]string{
		"security.privileged": "false",
		"raw.lxc":             "lxc.apparmor.profile = generated\nlxc.cap.drop = sys_admin\nlxc.mount.auto = proc\n",
	}
	applyContainerSecurity(config)

	if config["security.privileged"] != "true" {
		t.Fatalf("container must be privileged, got %q", config["security.privileged"])
	}
	if config["security.nesting"] != "true" {
		t.Fatalf("container must allow nesting, got %q", config["security.nesting"])
	}
	wantLines := []string{
		"lxc.mount.auto = proc",
		"lxc.apparmor.profile = unconfined",
		"lxc.cap.drop =",
	}
	for _, line := range wantLines {
		if strings.Count(config["raw.lxc"], line) != 1 {
			t.Fatalf("raw.lxc should contain %q exactly once: %q", line, config["raw.lxc"])
		}
	}

	applyContainerSecurity(config)
	for _, line := range wantLines {
		if strings.Count(config["raw.lxc"], line) != 1 {
			t.Fatalf("second application duplicated %q: %q", line, config["raw.lxc"])
		}
	}
}
