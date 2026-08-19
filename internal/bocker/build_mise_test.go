package bocker

import (
	"reflect"
	"strings"
	"testing"
)

func TestMiseDirectiveInTemp(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", `ARG GO_VERSION=1.26.5
FROM debian/13
TEMP builder
  MISE go ${GO_VERSION}
  MISE node 24.19.0
  MISE redis 7.2.5
  RUN go version && node --version
END
COPY --from=builder /tmp/app /usr/local/bin/app
`)
	f, err := parseIncusfileWithBuildArgs(p, map[string]string{"GO_VERSION": "1.26.6"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Stages) != 2 || len(f.Stages[0].Steps) != 4 {
		t.Fatalf("stages = %#v", f.Stages)
	}
	got := []MiseSpec{f.Stages[0].Steps[0].Mise, f.Stages[0].Steps[1].Mise, f.Stages[0].Steps[2].Mise}
	want := []MiseSpec{{Tool: "go", Version: "1.26.6"}, {Tool: "node", Version: "24.19.0"}, {Tool: "redis", Version: "7.2.5"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MISE specs = %#v, want %#v", got, want)
	}
	if len(f.Env) != 0 {
		t.Fatalf("TEMP MISE environment leaked into final image: %#v", f.Env)
	}
}

func TestMiseDirectiveValidation(t *testing.T) {
	invalid := []string{
		"FROM debian/13\nMISE go 1.26.6\n",
		"FROM debian/13\nTEMP build\nMISE go\nEND\n",
		"FROM debian/13\nTEMP build\nMISE go latest\nEND\n",
		"FROM debian/13\nTEMP build\nMISE go system\nEND\n",
		"FROM debian/13\nTEMP build\nMISE bad/tool 1.0.0\nEND\n",
		"FROM debian/13\nTEMP build\nMISE go '1.26.6; id'\nEND\n",
	}
	for _, content := range invalid {
		p := writeIncusfile(t, "Incusfile", content)
		if _, err := parseIncusfile(p); err == nil {
			t.Fatalf("invalid MISE unexpectedly parsed:\n%s", content)
		}
	}

	p := writeIncusfile(t, "Incusfile", "FROM debian/13\nTEMP build\nMISE postgresql 18\nEND\n")
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Stages[0].Steps[0].Mise; got != (MiseSpec{Tool: "postgres", Version: "18"}) {
		t.Fatalf("postgres alias = %#v", got)
	}
}

func TestMiseBuildEnvironmentDefaultsAndOverrides(t *testing.T) {
	env := map[string]string{
		"GOPROXY":                "https://proxy.example,direct",
		miseDownloadProxyEnvName: "https://download-proxy.example",
	}
	configureMiseBuildEnvironment(MiseSpec{Tool: "go", Version: "1.26.6"}, env)
	if env["MISE_DATA_DIR"] != miseDataDir || env["MISE_GLOBAL_CONFIG_FILE"] != miseConfigFile || !strings.HasPrefix(env["PATH"], miseShimsDir+":") {
		t.Fatalf("MISE environment = %#v", env)
	}
	if env["GOPROXY"] != "https://proxy.example,direct" || env["GOSUMDB"] != "sum.golang.google.cn" || env["MISE_GO_DOWNLOAD_MIRROR"] == "" {
		t.Fatalf("Go defaults missing or overrode user environment: %#v", env)
	}
	if env[miseDownloadProxyEnvName] != "https://download-proxy.example" {
		t.Fatalf("MISE download proxy default overrode user environment: %#v", env)
	}
	configureMiseBuildEnvironment(MiseSpec{Tool: "node", Version: "24.19.0"}, env)
	if strings.Count(env["PATH"], miseShimsDir) != 1 || env["MISE_NODE_MIRROR_URL"] == "" || env["NPM_CONFIG_REGISTRY"] == "" {
		t.Fatalf("Node.js domestic mirror missing: %#v", env)
	}
	configureMiseBuildEnvironment(MiseSpec{Tool: "rust", Version: "1.89.0"}, env)
	if env["RUSTUP_DIST_SERVER"] != "https://rsproxy.cn" || env["CARGO_REGISTRIES_CRATES_IO_INDEX"] != "sparse+https://rsproxy.cn/index/" {
		t.Fatalf("Rust domestic mirrors missing: %#v", env)
	}
	if env["RUSTUP_HOME"] != miseDataDir+"/rustup" || env["CARGO_HOME"] != miseDataDir+"/cargo" || !strings.Contains(env["PATH"], miseDataDir+"/cargo/bin") {
		t.Fatalf("Rust temporary homes missing: %#v", env)
	}
	configureMiseBuildEnvironment(MiseSpec{Tool: "python", Version: "3.13.7"}, env)
	if env["PIP_INDEX_URL"] != "https://pypi.tuna.tsinghua.edu.cn/simple" || env["PIP_DISABLE_PIP_VERSION_CHECK"] != "1" {
		t.Fatalf("Python domestic mirror missing: %#v", env)
	}
}

func TestMiseInstallCommandIsPinned(t *testing.T) {
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

func TestMiseServiceProbeUsesInstalledExecutable(t *testing.T) {
	for _, test := range []struct {
		spec  MiseSpec
		probe string
	}{
		{MiseSpec{Tool: "rust", Version: "1.89.0"}, "'rustc'"},
		{MiseSpec{Tool: "redis", Version: "7.2.5"}, "'redis-server'"},
		{MiseSpec{Tool: "postgres", Version: "18.6"}, "'postgres'"},
	} {
		if command := miseInstallCommand(test.spec); !strings.Contains(command, "mise exec -- "+test.probe+" --version") {
			t.Fatalf("%s probe missing:\n%s", test.spec.Tool, command)
		}
	}
}

func TestMiseBootstrapCommandUsesPinnedArchive(t *testing.T) {
	command := miseBootstrapCommand()
	for _, required := range []string{
		"sha256sum -c -",
		"tar -xzf",
		"install -m 0755",
		"mise version | grep -F '2026.8.8'",
		"curl -fL --connect-timeout 8 --max-time 25",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("bootstrap command missing %q:\n%s", required, command)
		}
	}
}
