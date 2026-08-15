package bocker

import (
	"reflect"
	"strings"
	"testing"
)

func TestNameOptionFromArgs(t *testing.T) {
	name, args, err := nameOptionFromArgs([]string{"debian:12", "--name", "demo", "--network", "nat"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "demo" {
		t.Fatalf("name = %q, want demo", name)
	}
	want := []string{"debian:12", "--network", "nat"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestNameOptionRejectsMissingAndDuplicateValues(t *testing.T) {
	for _, args := range [][]string{
		{"--name"},
		{"--name="},
		{"--name", "one", "--name", "two"},
	} {
		if _, _, err := nameOptionFromArgs(args); err == nil {
			t.Fatalf("nameOptionFromArgs(%#v) unexpectedly succeeded", args)
		}
	}
}

func TestJSONOutputOptionIsStrict(t *testing.T) {
	if enabled, err := parseJSONOutputOption([]string{"--json"}); err != nil || !enabled {
		t.Fatalf("--json = %v, %v; want true, nil", enabled, err)
	}
	if _, err := parseJSONOutputOption([]string{"--format", "json"}); err == nil {
		t.Fatal("unsupported JSON spelling unexpectedly succeeded")
	}
}

func TestRemovedTopLevelCommandsAreRejected(t *testing.T) {
	for _, command := range []string{
		"templates", "install", "build", "images", "run", "list", "ls", "shell",
		"create", "in", "start", "stop", "restart", "exec", "set", "remove",
		"export", "import", "uninstall",
	} {
		err := dispatch(command, nil)
		if err == nil || !strings.Contains(err.Error(), "未知命令") {
			t.Fatalf("dispatch(%q) error = %v, want unknown command", command, err)
		}
	}
}

func TestRemovedResourceActionsAreRejected(t *testing.T) {
	for resource, action := range map[string]string{
		"template":  "show",
		"image":     "images",
		"container": "in",
	} {
		if err := dispatch(resource, []string{action}); err == nil {
			t.Fatalf("%s %s unexpectedly succeeded", resource, action)
		}
	}
}

func TestRemovedBuildFlagsAreRejected(t *testing.T) {
	for _, option := range []string{"--no-run", "--image-only"} {
		if err := CmdBuild([]string{option}); err == nil {
			t.Fatalf("CmdBuild(%q) unexpectedly succeeded", option)
		}
	}
}

func TestRemovedInstallAndRunSyntaxIsRejected(t *testing.T) {
	if err := CmdInstall([]string{"debian:12", "demo"}); err == nil {
		t.Fatal("positional install container name unexpectedly succeeded")
	}
	for _, args := range [][]string{
		{"demo-image", "--name", "demo", "-N", "nat"},
		{"demo-image", "--name", "demo", "-P", "super"},
		{"demo-image", "--name", "demo", "--privilege", "super"},
	} {
		if err := CmdRun(args); err == nil {
			t.Fatalf("CmdRun(%#v) unexpectedly succeeded", args)
		}
	}
}

func TestRemovedPermissionValuesAreRejected(t *testing.T) {
	for _, value := range []string{"root", "privileged"} {
		if _, err := ParsePermissionMode(value); err == nil {
			t.Fatalf("ParsePermissionMode(%q) unexpectedly succeeded", value)
		}
	}
}

func TestRuntimeConfigFromImageProperties(t *testing.T) {
	f, err := runtimeConfigFromImageProperties("web-01", map[string]string{
		"user.bocker.expose":    "80/tcp,53/udp",
		"user.bocker.domain":    "web.test",
		"user.bocker.autostart": "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "web-01" || f.Domain != "web.test" || f.Autostart == nil || !*f.Autostart {
		t.Fatalf("runtime config = %#v", f)
	}
	if got := exposeString(f.Exposes); got != "80/tcp,53/udp" {
		t.Fatalf("exposes = %q", got)
	}
	if _, err := runtimeConfigFromImageProperties("bad", map[string]string{
		"user.bocker.autostart": "sometimes",
	}); err == nil {
		t.Fatal("invalid autostart unexpectedly succeeded")
	}
}

func TestValidateRuntimePortMappings(t *testing.T) {
	if err := validateRuntimePortMappings([]PortSpec{{Port: 80, Protocol: "tcp"}, {Port: 80, Protocol: "tcp"}}, nil); err == nil {
		t.Fatal("duplicate requested ports should fail")
	}
	containers := []Container{{Name: "occupied", Devices: map[string]map[string]string{
		portDeviceName(80, "tcp"): {"type": "proxy", "listen": "tcp:0.0.0.0:80", "connect": "tcp:10.0.0.2:80"},
	}}}
	if err := validateRuntimePortMappings([]PortSpec{{Port: 80, Protocol: "tcp"}}, containers); err == nil {
		t.Fatal("port owned by another container should fail")
	}
	if err := validateRuntimePortMappings([]PortSpec{{Port: 53, Protocol: "udp"}}, containers); err != nil {
		t.Fatalf("unoccupied port should succeed: %v", err)
	}
}

func TestBuildImagePropertiesRecordResolvedBase(t *testing.T) {
	f := &Incusfile{
		From:   "images:alpine/3.24",
		Stages: []Stage{{From: "images:alpine/3.24", BaseFingerprint: strings.Repeat("a", 64)}},
	}
	properties := buildImageProperties(f)
	if properties["user.bocker.base_image"] != "images:alpine/3.24" {
		t.Fatalf("base image property = %q", properties["user.bocker.base_image"])
	}
	if properties["user.bocker.base_fingerprint"] != strings.Repeat("a", 64) {
		t.Fatalf("base fingerprint property = %q", properties["user.bocker.base_fingerprint"])
	}
}
