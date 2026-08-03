package main

import (
	"archive/zip"
	"strings"
	"testing"
)

func TestEmbeddedRuntimeIsContainerOnly(t *testing.T) {
	zr, err := zip.OpenReader("runtime/incus-runtime.zip")
	if err != nil {
		t.Fatalf("open embedded runtime: %v", err)
	}
	defer zr.Close()

	entries := make(map[string]bool, len(zr.File))
	for _, entry := range zr.File {
		entries[strings.ReplaceAll(entry.Name, "\\", "/")] = true
	}
	for _, required := range []string{
		"bin/incusd",
		"bin/lxcfs",
		"lib/liblxc.so.1",
		"lib/libcowsql.so.0",
		"lib/libfuse3.so.3",
		"share/lxcfs/lxc.mount.hook",
	} {
		if !entries[required] {
			t.Errorf("embedded runtime is missing %s", required)
		}
	}
	for _, forbidden := range []string{
		"bin/incus",
		"bin/qemu-system-x86_64",
		"bin/incus-migrate",
		"bin/incus-benchmark",
		"lib/libnvidia-container.so",
		"lib/libnvidia-container.so.1",
		"lib/libnvidia-container.so.1.19.1",
		"lib/libnvidia-container-go.so",
		"lib/libnvidia-container-go.so.1",
		"lib/libnvidia-container-go.so.1.19.1",
		"lib/libswtpm_libtpms.so",
		"lib/libswtpm_libtpms.so.0",
		"lib/libswtpm_libtpms.so.0.0.0",
		"lib/libtpms.so",
		"lib/libtpms.so.0",
		"lib/libtpms.so.0.10.2",
		"share/lxc/hooks/nvidia",
	} {
		if entries[forbidden] {
			t.Errorf("embedded runtime unexpectedly contains %s", forbidden)
		}
	}
}
