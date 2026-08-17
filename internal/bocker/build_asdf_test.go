package bocker

import (
	"reflect"
	"strings"
	"testing"
)

func TestAsdfDirectiveInTemp(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", `ARG GO_VERSION=1.26.5
FROM debian/13
TEMP builder
  ASDF go ${GO_VERSION}
  ASDF node 24.19.0
  RUN go version && node --version
END
COPY --from=builder /tmp/app /usr/local/bin/app
`)
	f, err := parseIncusfileWithBuildArgs(p, map[string]string{"GO_VERSION": "1.26.6"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Stages) != 2 || len(f.Stages[0].Steps) != 3 {
		t.Fatalf("stages = %#v", f.Stages)
	}
	got := []AsdfSpec{f.Stages[0].Steps[0].Asdf, f.Stages[0].Steps[1].Asdf}
	want := []AsdfSpec{{Tool: "golang", Version: "1.26.6"}, {Tool: "nodejs", Version: "24.19.0"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ASDF specs = %#v, want %#v", got, want)
	}
	if len(f.Env) != 0 {
		t.Fatalf("TEMP ASDF environment leaked into final image: %#v", f.Env)
	}
}

func TestAsdfDirectiveValidation(t *testing.T) {
	invalid := []string{
		"FROM debian/13\nASDF go 1.26.6\n",
		"FROM debian/13\nTEMP build\nASDF go\nEND\n",
		"FROM debian/13\nTEMP build\nASDF go latest\nEND\n",
		"FROM debian/13\nTEMP build\nASDF go system\nEND\n",
		"FROM debian/13\nTEMP build\nASDF bad/tool 1.0.0\nEND\n",
		"FROM debian/13\nTEMP build\nASDF go '1.26.6; id'\nEND\n",
	}
	for _, content := range invalid {
		p := writeIncusfile(t, "Incusfile", content)
		if _, err := parseIncusfile(p); err == nil {
			t.Fatalf("invalid ASDF unexpectedly parsed:\n%s", content)
		}
	}

	p := writeIncusfile(t, "Incusfile", "FROM debian/13\nTEMP build\nASDF python 3.13.7\nEND\n")
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Stages[0].Steps[0].Asdf; got != (AsdfSpec{Tool: "python", Version: "3.13.7"}) {
		t.Fatalf("generic plugin = %#v", got)
	}
}

func TestAsdfBuildEnvironmentDefaultsAndOverrides(t *testing.T) {
	env := map[string]string{
		"GOPROXY":              "https://proxy.example,direct",
		asdfPluginProxyEnvName: "https://plugin-proxy.example",
	}
	configureAsdfBuildEnvironment(AsdfSpec{Tool: "golang", Version: "1.26.6"}, env)
	if env["ASDF_DATA_DIR"] != asdfDataDir || !strings.HasPrefix(env["PATH"], asdfShimsDir+":") {
		t.Fatalf("ASDF environment = %#v", env)
	}
	if env["GOPROXY"] != "https://proxy.example,direct" || env["GOSUMDB"] != "sum.golang.google.cn" {
		t.Fatalf("Go defaults overrode user environment: %#v", env)
	}
	if env[asdfDownloadProxyEnvName] != asdfDownloadProxy || env[asdfPluginProxyEnvName] != "https://plugin-proxy.example" {
		t.Fatalf("ASDF proxy defaults overrode user environment: %#v", env)
	}
	configureAsdfBuildEnvironment(AsdfSpec{Tool: "nodejs", Version: "24.19.0"}, env)
	if strings.Count(env["PATH"], asdfShimsDir) != 1 {
		t.Fatalf("ASDF shims duplicated in PATH: %q", env["PATH"])
	}
	if env["NODEJS_ORG_MIRROR"] == "" || env["NPM_CONFIG_REGISTRY"] == "" || !strings.Contains(env["ASDF_NODEJS_NODEBUILD_REPOSITORY"], "nodenv/node-build.git") || env["ASDF_NODEJS_SKIP_NODEBUILD_UPDATE"] != "1" {
		t.Fatalf("Node.js domestic mirrors missing: %#v", env)
	}
	configureAsdfBuildEnvironment(AsdfSpec{Tool: "rust", Version: "1.89.0"}, env)
	if env["RUSTUP_DIST_SERVER"] != "https://rsproxy.cn" || env["RUSTUP_UPDATE_ROOT"] == "" || env["CARGO_REGISTRIES_CRATES_IO_INDEX"] != "sparse+https://rsproxy.cn/index/" || env["CARGO_REGISTRIES_CRATES_IO_PROTOCOL"] != "sparse" {
		t.Fatalf("Rust domestic mirrors missing: %#v", env)
	}
	configureAsdfBuildEnvironment(AsdfSpec{Tool: "python", Version: "3.13.7"}, env)
	if !strings.Contains(env["ASDF_PYTHON_PYENV_REPOSITORY"], "pyenv/pyenv.git") || env["PIP_INDEX_URL"] != "https://pypi.tuna.tsinghua.edu.cn/simple" || env["PIP_DISABLE_PIP_VERSION_CHECK"] != "1" {
		t.Fatalf("Python domestic repository proxy missing: %#v", env)
	}
}

func TestAsdfInstallCommandIsPinned(t *testing.T) {
	command := asdfInstallCommand(AsdfSpec{Tool: "golang", Version: "1.26.6"})
	for _, required := range []string{
		asdfDownloadURL,
		asdfDownloadProxy,
		asdfPluginProxy,
		asdfSHA256,
		"https://github.com/asdf-community/asdf-golang.git",
		"--connect-timeout 8 --max-time 25",
		"timeout 45",
		"install 'golang' '1.26.6'",
		"set --home 'golang' '1.26.6'",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("install command missing %q:\n%s", required, command)
		}
	}
}

func TestKnownAsdfPluginsUseProxyCapableURLs(t *testing.T) {
	for _, tool := range []string{"golang", "nodejs", "python", "rust"} {
		command := asdfInstallCommand(AsdfSpec{Tool: tool, Version: "1.2.3"})
		if !strings.Contains(command, asdfKnownTools[tool].pluginURL) || !strings.Contains(command, asdfPluginProxy) {
			t.Fatalf("%s plugin command has no proxy fallback:\n%s", tool, command)
		}
	}
}

func TestAsdfNodeBuildUsesBoundedShallowClone(t *testing.T) {
	command := asdfInstallCommand(AsdfSpec{Tool: "nodejs", Version: "24.19.0"})
	for _, required := range []string{
		"ASDF_NODEJS_NODEBUILD_REPOSITORY",
		"https://github.com/nodenv/node-build.git",
		"timeout 120 git clone --depth 1 --single-branch --branch main",
		"test -x \"$nodebuild_dir/bin/node-build\"",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("Node.js install command missing %q:\n%s", required, command)
		}
	}
}

func TestAsdfPythonBuildUsesBoundedShallowClone(t *testing.T) {
	command := asdfInstallCommand(AsdfSpec{Tool: "python", Version: "3.13.7"})
	for _, required := range []string{
		"ASDF_PYTHON_PYENV_REPOSITORY",
		"https://github.com/pyenv/pyenv.git",
		"timeout 120 git clone --depth 1 --single-branch --branch master",
		"plugins/python-build/bin/python-build",
		"pyenv_last_update",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("Python install command missing %q:\n%s", required, command)
		}
	}
}
