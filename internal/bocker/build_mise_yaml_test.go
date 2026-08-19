package bocker

import (
	"strings"
	"testing"
)

func TestMiseNormalizationAndPinnedInstall(t *testing.T) {
	spec, err := normalizeMiseSpec(MiseSpec{Tool: "postgresql", Version: "18.6"})
	if err != nil || spec != (MiseSpec{Tool: "postgres", Version: "18.6"}) {
		t.Fatalf("normalized mise = %#v, err=%v", spec, err)
	}
	for _, invalid := range []MiseSpec{
		{Tool: "bad/tool", Version: "1.0.0"},
		{Tool: "go", Version: "latest"},
		{Tool: "go", Version: "system"},
		{Tool: "go", Version: "1.0.0;id"},
	} {
		if _, err := normalizeMiseSpec(invalid); err == nil {
			t.Fatalf("normalizeMiseSpec(%#v) unexpectedly succeeded", invalid)
		}
	}
	command := miseInstallCommand(MiseSpec{Tool: "go", Version: "1.26.6"})
	for _, required := range []string{
		miseDownloadURL,
		miseDownloadProxy,
		miseSHA256,
		"mise use --global --pin --yes 'go@1.26.6'",
		"mise reshim --yes",
		"mise exec -- 'go' version",
		"mise where 'go@1.26.6'",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("install command missing %q:\n%s", required, command)
		}
	}
}

func TestMiseBuildEnvironmentPreservesOverrides(t *testing.T) {
	env := map[string]string{
		"GOPROXY":                "https://proxy.example,direct",
		miseDownloadProxyEnvName: "https://download-proxy.example",
	}
	configureMiseBuildEnvironment(MiseSpec{Tool: "go", Version: "1.26.6"}, env)
	if env["GOPROXY"] != "https://proxy.example,direct" || env[miseDownloadProxyEnvName] != "https://download-proxy.example" {
		t.Fatalf("user MISE environment was overridden: %#v", env)
	}
	if env["GOSUMDB"] == "" || !strings.HasPrefix(env["PATH"], miseShimsDir+":") {
		t.Fatalf("Go MISE environment defaults missing: %#v", env)
	}
	configureMiseBuildEnvironment(MiseSpec{Tool: "rust", Version: "1.89.0"}, env)
	if env["RUSTUP_DIST_SERVER"] != "https://rsproxy.cn" || env["CARGO_HOME"] != miseDataDir+"/cargo" || !strings.Contains(env["PATH"], miseDataDir+"/cargo/bin") {
		t.Fatalf("Rust MISE environment defaults missing: %#v", env)
	}
}
