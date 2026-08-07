//go:build linux

package bocker

import (
	"os/exec"
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
