//go:build linux

package bocker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEmbeddedSystemdUnitBoundsShutdownAndRestarts(t *testing.T) {
	unit := embeddedSystemdUnit("/var/lib/bocker/bin/bocker-daemon", "/var/lib/bocker")
	for _, expected := range []string{
		`ExecStart="/var/lib/bocker/bin/bocker-daemon" __daemon`,
		`Environment="BOCKER_STATE_DIR=/var/lib/bocker"`,
		"StartLimitIntervalSec=60",
		"StartLimitBurst=5",
		"KillMode=mixed",
		"TimeoutStopSec=40",
		"SendSIGKILL=yes",
	} {
		if !strings.Contains(unit, expected) {
			t.Errorf("systemd unit is missing %q", expected)
		}
	}
}

func TestSignalProcessGroupTerminatesChildren(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process group: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	signalProcessGroup(cmd.Process, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		signalProcessGroup(cmd.Process, syscall.SIGKILL)
		t.Fatal("process group did not stop after SIGTERM")
	}
}

func TestRotateDaemonLogInPlaceKeepsTailAndTruncates(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "incusd-internal.log")
	// Write a log slightly larger than the rotation threshold. The marker
	// must land inside the last daemonLogTailKeep bytes so it survives into
	// the preserved backup.
	payload := make([]byte, maxDaemonLogSize)
	for i := range payload {
		payload[i] = 'a'
	}
	payload = append(payload, []byte("tail-marker\n")...)
	if err := os.WriteFile(logPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rotateDaemonLogInPlace(logPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("live log size = %d, want 0 after truncation", len(data))
	}
	backup, err := os.ReadFile(logPath + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(backup), "tail-marker\n") {
		t.Fatalf("backup is missing the newest lines: %q", backup[len(backup)-80:])
	}
	if int64(len(backup)) > daemonLogTailKeep {
		t.Fatalf("backup size %d exceeds tail budget %d", len(backup), daemonLogTailKeep)
	}
}

func TestRotateDaemonLogInPlaceSkipsSmallAndMissingFiles(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "small.log")
	if err := os.WriteFile(logPath, []byte("small"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rotateDaemonLogInPlace(logPath); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(logPath)
	if string(data) != "small" {
		t.Fatalf("small log was modified: %q", data)
	}
	if _, err := os.Stat(logPath + ".1"); !os.IsNotExist(err) {
		t.Fatal("small log must not create a backup")
	}
	if err := rotateDaemonLogInPlace(filepath.Join(t.TempDir(), "missing.log")); err != nil {
		t.Fatalf("missing log must be a no-op: %v", err)
	}
}

func TestAllowRuntimeHookAccessUsesTraverseOnlyPermissions(t *testing.T) {
	root := t.TempDir()
	runtimeParent := filepath.Join(root, "runtime")
	runtimeDir := filepath.Join(runtimeParent, "current")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := allowRuntimeHookAccess(embeddedPaths{runtimeDir: runtimeDir}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{runtimeParent, runtimeDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o711 {
			t.Fatalf("%s permissions = %o, want 711", dir, got)
		}
	}
}
