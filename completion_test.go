package main

import (
	"strings"
	"testing"
)

func TestCompletionScripts(t *testing.T) {
	for _, shell := range completionShells {
		script, err := completionScript(shell)
		if err != nil {
			t.Fatalf("completionScript(%q): %v", shell, err)
		}
		for _, token := range []string{"bocker", "__complete containers", "completion"} {
			if !strings.Contains(script, token) {
				t.Errorf("%s completion does not contain %q", shell, token)
			}
		}
		if _, err := completionInstallPath(shell); err != nil {
			t.Errorf("completionInstallPath(%q): %v", shell, err)
		}
	}
	if _, err := completionScript("powershell"); err == nil {
		t.Fatal("unsupported shell should fail")
	}
	if !strings.Contains(bashCompletion, "_bocker_completion_refresh_containers") || !strings.Contains(bashCompletion, "_bocker_completion_refresh_images") {
		t.Fatal("bash completion must use cached, on-demand dynamic candidates")
	}
	if !strings.Contains(zshCompletion, "_bocker_refresh_containers") || !strings.Contains(fishCompletion, "__bocker_containers_cache") {
		t.Fatal("zsh/fish completion must use cached dynamic candidates")
	}
}

func TestRuntimeCommand(t *testing.T) {
	if got := runtimeCommand(&Incusfile{Cmd: []string{"/bin/app", "--port", "8080"}}); !strings.Contains(strings.Join(got, " "), "/bin/app --port 8080") {
		t.Fatalf("CMD-only runtime command = %#v", got)
	}
	if !hasRuntimeCommand(&Incusfile{Entrypoint: []string{"/bin/app"}}) {
		t.Fatal("ENTRYPOINT should enable runtime init")
	}
}
