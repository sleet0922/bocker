package bocker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

func TestMountJSONUsesStableScriptFieldNames(t *testing.T) {
	data, err := json.Marshal([]Mount{{
		Name:      "mount-demo",
		Source:    "/host/source:with -> marker",
		Target:    "/container/target:with -> marker",
		Readonly:  true,
		Inherited: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var got []Mount
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("mount JSON cannot be decoded: %v", err)
	}
	if len(got) != 1 || got[0].Name != "mount-demo" ||
		got[0].Source != "/host/source:with -> marker" ||
		got[0].Target != "/container/target:with -> marker" ||
		!got[0].Readonly || !got[0].Inherited {
		t.Fatalf("mount JSON decoded as %#v", got)
	}
}

func TestMountDeviceNameIsStableBoundedAndDistinct(t *testing.T) {
	source := "/var/lib/bocker/source with spaces"
	target := "/opt/project/this-is-a-very-long-target-name-that-must-be-truncated"
	first := mountDeviceName(source, target)
	second := mountDeviceName(source, target)
	if first != second {
		t.Fatalf("mountDeviceName is not stable: %q != %q", first, second)
	}
	if len(first) > mountDeviceNameMaxLen {
		t.Fatalf("mount device name length = %d, want <= %d (%q)", len(first), mountDeviceNameMaxLen, first)
	}
	if !strings.HasPrefix(first, mountDevicePrefix) {
		t.Fatalf("mount device name %q does not have prefix %q", first, mountDevicePrefix)
	}
	if strings.ContainsAny(first, " /\\") {
		t.Fatalf("mount device name contains unsafe separators: %q", first)
	}
	if other := mountDeviceName("/var/lib/bocker/other", target); other == first {
		t.Fatalf("different source produced the same device name: %q", first)
	}
}

func TestValidateMountPathsRequiresRegularFileOrDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(file, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		source string
		target string
		want   string
	}{
		{name: "regular file", source: file, target: "/data/source.txt", want: file},
		{name: "directory", source: dir, target: "/data", want: dir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, target, err := validateMountPaths(tc.source, tc.target)
			if err != nil {
				t.Fatalf("validateMountPaths() error = %v", err)
			}
			if source != tc.want || target != filepath.Clean(tc.target) {
				t.Fatalf("validateMountPaths() = %q, %q; want %q, %q", source, target, tc.want, filepath.Clean(tc.target))
			}
		})
	}

	if _, _, err := validateMountPaths("relative", "/data"); err == nil {
		t.Fatal("relative source unexpectedly accepted")
	}
	if _, _, err := validateMountPaths(file, "/"); err == nil {
		t.Fatal("root target unexpectedly accepted")
	}
	if _, _, err := validateMountPaths(filepath.Join(dir, "missing"), "/data"); err == nil {
		t.Fatal("missing source unexpectedly accepted")
	}
}

func TestValidateMountPathsRejectsSourcesNotOwnedByBrokerCaller(t *testing.T) {
	dir := t.TempDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("file ownership metadata unavailable")
	}
	t.Setenv(callerUIDEnv, strconv.FormatUint(uint64(stat.Uid)+1, 10))
	if _, _, err := validateMountPaths(dir, "/data"); err == nil || !strings.Contains(err.Error(), "归当前调用用户所有") {
		t.Fatalf("foreign-owned mount source error = %v", err)
	}
	t.Setenv(callerUIDEnv, strconv.FormatUint(uint64(stat.Uid), 10))
	if _, _, err := validateMountPaths(dir, "/data"); err != nil {
		t.Fatalf("caller-owned mount source rejected: %v", err)
	}
}

func TestNormalizedMountPathCanonicalizesWhitespaceAndTrailingSeparators(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: " /mnt/data/ ", want: "/mnt/data"},
		{input: "/mnt/./data", want: "/mnt/data"},
		{input: "", want: ""},
	} {
		if got := normalizedMountPath(tc.input); got != tc.want {
			t.Errorf("normalizedMountPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestMountCreateTypeAndReadonly(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "source")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := mountCreateType(info); got != "file" {
		t.Fatalf("mountCreateType(file) = %q, want file", got)
	}
	info, err = os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := mountCreateType(info); got != "dir" {
		t.Fatalf("mountCreateType(dir) = %q, want dir", got)
	}
	for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
		if !mountReadonly(map[string]string{"readonly": value}) {
			t.Errorf("mountReadonly(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "0", "false", "no", "off"} {
		if mountReadonly(map[string]string{"readonly": value}) {
			t.Errorf("mountReadonly(%q) = true, want false", value)
		}
	}
}

func TestMountDeviceMapsIncludesProfileDevices(t *testing.T) {
	full := &api.InstanceFull{
		Instance: api.Instance{
			InstancePut: api.InstancePut{Devices: api.DevicesMap{
				"mount-local": {"type": "disk", "source": "/host/local", "path": "/local"},
			}},
			ExpandedDevices: api.DevicesMap{
				"mount-local":   {"type": "disk", "source": "/host/local", "path": "/local"},
				"mount-profile": {"type": "disk", "source": "/host/profile", "path": "/profile"},
			},
		},
	}
	devices, expanded := mountDeviceMaps(full)
	if !expanded || len(devices) != 2 {
		t.Fatalf("mountDeviceMaps() = expanded=%v devices=%#v", expanded, devices)
	}
	if mountDeviceInherited(full, "mount-local", expanded) {
		t.Fatal("local device was marked inherited")
	}
	if !mountDeviceInherited(full, "mount-profile", expanded) {
		t.Fatal("profile device was not marked inherited")
	}

	full.ExpandedDevices = nil
	devices, expanded = mountDeviceMaps(full)
	if expanded || len(devices) != 1 {
		t.Fatalf("mountDeviceMaps() fallback = expanded=%v devices=%#v", expanded, devices)
	}
}

func TestRequireMountStoppedRejectsEveryNonStoppedState(t *testing.T) {
	for _, status := range []api.StatusCode{api.Running, api.Starting, api.Stopping, api.Frozen, api.Error} {
		full := &api.InstanceFull{Instance: api.Instance{Status: status.String(), StatusCode: status}}
		if err := requireMountStopped("demo", full, "添加"); err == nil {
			t.Errorf("status %s was unexpectedly accepted", status)
		}
	}
	full := &api.InstanceFull{Instance: api.Instance{Status: "Stopped", StatusCode: api.Stopped}}
	if err := requireMountStopped("demo", full, "添加"); err != nil {
		t.Fatalf("stopped status rejected: %v", err)
	}
}
