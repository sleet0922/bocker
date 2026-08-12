package bocker

import "testing"

func TestPermissionModeFromArgs(t *testing.T) {
	mode, args, err := permissionModeFromArgs([]string{"--network", "nat", "--permission", "super", "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if mode != PermissionSuper || len(args) != 3 || args[1] != "nat" {
		t.Fatalf("mode=%q args=%#v", mode, args)
	}
	mode, args, err = permissionModeFromArgs([]string{"demo"})
	if err != nil || mode != PermissionNormal || len(args) != 1 {
		t.Fatalf("default mode=%q args=%#v err=%v", mode, args, err)
	}
}

func TestApplyPermissionConfig(t *testing.T) {
	config := map[string]string{"security.privileged": "true", "raw.lxc": "lxc.apparmor.profile = unconfined\nlxc.cap.drop =\nlxc.mount.auto = proc\n"}
	applyPermissionConfig(config, PermissionNormal)
	if config["security.privileged"] != "false" {
		t.Fatalf("normal permission must be unprivileged, got %q", config["security.privileged"])
	}
	if _, ok := config["security.nesting"]; ok {
		t.Fatal("normal permission should not enable nesting")
	}
	if config["raw.lxc"] != "lxc.mount.auto = proc\n" {
		t.Fatalf("normal permission removed unrelated raw.lxc config: %q", config["raw.lxc"])
	}
	applyPermissionConfig(config, PermissionSuper)
	for _, key := range []string{"security.nesting", "raw.lxc", permissionConfigKey} {
		if config[key] == "" {
			t.Fatalf("super permission missing %s", key)
		}
	}
	if config["security.privileged"] != "true" {
		t.Fatalf("super permission must be privileged, got %q", config["security.privileged"])
	}
}
