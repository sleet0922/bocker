package bocker

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDownloadCommandIsValidShellAndKeepsFallbackOrder(t *testing.T) {
	command := downloadCommand(DownloadSpec{
		Output:  "/tmp/source.archive",
		Extract: "/src",
		Attempts: []DownloadAttempt{
			{URL: "https://primary.invalid/source.tar.gz", SHA256: strings.Repeat("a", 64), Format: "tar.gz", Timeout: 20, Tries: 1},
			{URL: "https://fallback.invalid/source.zip", SHA256: strings.Repeat("b", 64), Format: "zip", Timeout: 30, Tries: 3, Move: &MoveSpec{From: "/src/module", To: "/src/source"}},
		},
		Verify: &FileVerifySpec{Path: "/src/source/version.go", Pattern: `s/^const Version = "\([^\"]*\)"/\1/p`, Value: "1.2.3"},
	})
	if err := exec.Command("sh", "-n", "-c", command).Run(); err != nil {
		t.Fatalf("generated download shell is invalid: %v\n%s", err, command)
	}
	if strings.Index(command, "primary.invalid") > strings.Index(command, "fallback.invalid") {
		t.Fatalf("fallback order reversed: %s", command)
	}
}

func TestServiceActionsAreDeterministic(t *testing.T) {
	actions := serviceActions(ServiceSpec{
		Start:  []string{"postgresql.service"},
		Stop:   []string{"postgresql.service"},
		Enable: []string{"postgresql.service", "api.service"},
	})
	if len(actions) != 3 || actions[0].Name != "start" || actions[1].Name != "stop" || actions[2].Name != "enable" {
		t.Fatalf("service actions = %#v", actions)
	}
}
