package bocker

import (
	"fmt"
	"strings"
)

const (
	asdfVersion              = "0.20.0"
	asdfDownloadURL          = "https://github.com/asdf-vm/asdf/releases/download/v0.20.0/asdf-v0.20.0-linux-amd64.tar.gz"
	asdfDownloadProxy        = "https://gh.nxnow.top"
	asdfPluginProxy          = "https://gh-proxy.com"
	asdfSHA256               = "9c25e1af7cc4c9d59ff3736eba14fd000480c32929258f80d8c5a8b290ebee14"
	asdfBinary               = "/usr/local/bin/asdf"
	asdfDataDir              = "/opt/bocker-asdf"
	asdfShimsDir             = asdfDataDir + "/shims"
	asdfDownloadProxyEnvName = "ASDF_DOWNLOAD_PROXY"
	asdfPluginProxyEnvName   = "ASDF_PLUGIN_PROXY"
	defaultBuildPath         = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

type asdfToolDefaults struct {
	pluginURL string
	env       map[string]string
}

var asdfKnownTools = map[string]asdfToolDefaults{
	"golang": {
		pluginURL: "https://github.com/asdf-community/asdf-golang.git",
		env: map[string]string{
			"GOPROXY": "https://goproxy.cn,direct",
			"GOSUMDB": "sum.golang.google.cn",
		},
	},
	"nodejs": {
		pluginURL: "https://github.com/asdf-vm/asdf-nodejs.git",
		env: map[string]string{
			"NODEJS_ORG_MIRROR":                 "https://registry.npmmirror.com/-/binary/node/",
			"NPM_CONFIG_REGISTRY":               "https://registry.npmmirror.com",
			"ASDF_NODEJS_NODEBUILD_REPOSITORY":  asdfPluginProxy + "/https://github.com/nodenv/node-build.git",
			"ASDF_NODEJS_SKIP_NODEBUILD_UPDATE": "1",
		},
	},
	"python": {
		pluginURL: "https://github.com/asdf-community/asdf-python.git",
		env: map[string]string{
			"ASDF_PYTHON_PYENV_REPOSITORY":  asdfPluginProxy + "/https://github.com/pyenv/pyenv.git",
			"PIP_INDEX_URL":                 "https://pypi.tuna.tsinghua.edu.cn/simple",
			"PIP_DISABLE_PIP_VERSION_CHECK": "1",
		},
	},
	"rust": {
		pluginURL: "https://github.com/code-lever/asdf-rust.git",
		env: map[string]string{
			"RUSTUP_DIST_SERVER":                  "https://rsproxy.cn",
			"RUSTUP_UPDATE_ROOT":                  "https://rsproxy.cn/rustup",
			"CARGO_REGISTRIES_CRATES_IO_INDEX":    "sparse+https://rsproxy.cn/index/",
			"CARGO_REGISTRIES_CRATES_IO_PROTOCOL": "sparse",
		},
	},
}

func parseAsdfPayload(payload string) (AsdfSpec, error) {
	parts, err := shellSplit(payload)
	if err != nil || len(parts) != 2 {
		return AsdfSpec{}, fmt.Errorf("ASDF 用法: ASDF <tool> <exact-version>，如 ASDF go 1.26.6")
	}
	return AsdfSpec{Tool: parts[0], Version: parts[1]}, nil
}

func normalizeAsdfSpec(spec AsdfSpec) (AsdfSpec, error) {
	spec.Tool = strings.ToLower(strings.TrimSpace(spec.Tool))
	switch spec.Tool {
	case "go":
		spec.Tool = "golang"
	case "node":
		spec.Tool = "nodejs"
	}
	if !validAsdfToken(spec.Tool) {
		return AsdfSpec{}, fmt.Errorf("ASDF 工具名 %q 无效", spec.Tool)
	}
	spec.Version = strings.TrimSpace(spec.Version)
	if !validAsdfToken(spec.Version) || spec.Version == "latest" || spec.Version == "system" || strings.HasPrefix(spec.Version, "latest:") {
		return AsdfSpec{}, fmt.Errorf("ASDF 版本 %q 无效；必须指定精确版本", spec.Version)
	}
	return spec, nil
}

func validAsdfToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		if i > 0 && (c == '.' || c == '_' || c == '+' || c == '-') {
			continue
		}
		return false
	}
	return true
}

func configureAsdfBuildEnvironment(spec AsdfSpec, runEnv map[string]string) {
	runEnv["ASDF_DATA_DIR"] = asdfDataDir
	if _, configured := runEnv[asdfDownloadProxyEnvName]; !configured {
		runEnv[asdfDownloadProxyEnvName] = asdfDownloadProxy
	}
	if _, configured := runEnv[asdfPluginProxyEnvName]; !configured {
		runEnv[asdfPluginProxyEnvName] = asdfPluginProxy
	}
	currentPath := runEnv["PATH"]
	if currentPath == "" {
		currentPath = defaultBuildPath
	}
	if !strings.Contains(":"+currentPath+":", ":"+asdfShimsDir+":") {
		runEnv["PATH"] = asdfShimsDir + ":" + currentPath
	}
	for key, value := range asdfKnownTools[spec.Tool].env {
		if _, configured := runEnv[key]; !configured {
			runEnv[key] = value
		}
	}
}

func asdfInstallCommand(spec AsdfSpec) string {
	pluginURL := asdfKnownTools[spec.Tool].pluginURL
	pluginAdd := asdfBinary + " plugin add " + shellQuote(spec.Tool)
	if pluginURL != "" {
		pluginDir := asdfDataDir + "/plugins/" + spec.Tool
		pluginAdd = strings.Join([]string{
			"plugin_proxy=\"${" + asdfPluginProxyEnvName + ":-" + asdfPluginProxy + "}\"",
			"plugin_url=\"${plugin_proxy%/}/" + pluginURL + "\"",
			"if ! timeout 45 " + asdfBinary + " plugin add " + shellQuote(spec.Tool) + " \"$plugin_url\"; then " +
				"rm -rf " + shellQuote(pluginDir) + "; " +
				"timeout 90 " + asdfBinary + " plugin add " + shellQuote(spec.Tool) + " " + shellQuote(pluginURL) + "; fi",
		}, "; ")
	}
	commands := []string{
		"set -eu",
		"if [ ! -x " + asdfBinary + " ]; then " + asdfBootstrapCommand() + "; fi",
		"mkdir -p " + shellQuote(asdfDataDir),
		"if ! " + asdfBinary + " plugin list 2>/dev/null | grep -Fxq " + shellQuote(spec.Tool) + "; then " + pluginAdd + "; fi",
	}
	if spec.Tool == "nodejs" {
		commands = append(commands, asdfNodeBuildBootstrapCommand())
	} else if spec.Tool == "python" {
		commands = append(commands, asdfPythonBuildBootstrapCommand())
	}
	commands = append(commands,
		asdfBinary+" install "+shellQuote(spec.Tool)+" "+shellQuote(spec.Version),
		asdfBinary+" set --home "+shellQuote(spec.Tool)+" "+shellQuote(spec.Version),
		asdfBinary+" reshim "+shellQuote(spec.Tool)+" "+shellQuote(spec.Version),
		asdfBinary+" where "+shellQuote(spec.Tool)+" "+shellQuote(spec.Version),
	)
	return strings.Join(commands, "; ")
}

func asdfNodeBuildBootstrapCommand() string {
	nodeBuildDir := asdfDataDir + "/plugins/nodejs/.node-build"
	proxyURL := asdfPluginProxy + "/https://github.com/nodenv/node-build.git"
	officialURL := "https://github.com/nodenv/node-build.git"
	return strings.Join([]string{
		"nodebuild_dir=" + shellQuote(nodeBuildDir),
		"nodebuild_repo=\"${ASDF_NODEJS_NODEBUILD_REPOSITORY:-" + proxyURL + "}\"",
		"if [ ! -x \"$nodebuild_dir/bin/node-build\" ]; then " +
			"rm -rf \"$nodebuild_dir\"; " +
			"if ! timeout 120 git clone --depth 1 --single-branch --branch main \"$nodebuild_repo\" \"$nodebuild_dir\"; then " +
			"rm -rf \"$nodebuild_dir\"; " +
			"timeout 120 git clone --depth 1 --single-branch --branch main " + shellQuote(officialURL) + " \"$nodebuild_dir\"; fi; fi",
		"test -x \"$nodebuild_dir/bin/node-build\"",
	}, "; ")
}

func asdfPythonBuildBootstrapCommand() string {
	pyenvDir := asdfDataDir + "/plugins/python/pyenv"
	updateTimestamp := asdfDataDir + "/plugins/python/pyenv_last_update"
	proxyURL := asdfPluginProxy + "/https://github.com/pyenv/pyenv.git"
	officialURL := "https://github.com/pyenv/pyenv.git"
	return strings.Join([]string{
		"pyenv_dir=" + shellQuote(pyenvDir),
		"pyenv_repo=\"${ASDF_PYTHON_PYENV_REPOSITORY:-" + proxyURL + "}\"",
		"if [ ! -x \"$pyenv_dir/plugins/python-build/bin/python-build\" ]; then " +
			"rm -rf \"$pyenv_dir\"; " +
			"if ! timeout 120 git clone --depth 1 --single-branch --branch master \"$pyenv_repo\" \"$pyenv_dir\"; then " +
			"rm -rf \"$pyenv_dir\"; " +
			"timeout 120 git clone --depth 1 --single-branch --branch master " + shellQuote(officialURL) + " \"$pyenv_dir\"; fi; fi",
		"test -x \"$pyenv_dir/plugins/python-build/bin/python-build\"",
		"date +%s > " + shellQuote(updateTimestamp),
	}, "; ")
}

func asdfBootstrapCommand() string {
	return strings.Join([]string{
		"if command -v apt-get >/dev/null 2>&1; then apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends bash ca-certificates coreutils curl git xz-utils; " +
			"elif command -v apk >/dev/null 2>&1; then apk add --no-cache bash ca-certificates coreutils curl git xz; " +
			"else echo 'ASDF 目前只支持 Debian/Ubuntu 和 Alpine 构建阶段' >&2; exit 1; fi",
		"test \"$(uname -m)\" = x86_64",
		"archive=$(mktemp /tmp/bocker-asdf.XXXXXX.tar.gz)",
		"trap 'rm -f \"$archive\"' EXIT",
		"download_proxy=\"${" + asdfDownloadProxyEnvName + ":-" + asdfDownloadProxy + "}\"",
		"fallback_url=\"${download_proxy%/}/" + asdfDownloadURL + "\"",
		"if ! curl -fL --connect-timeout 8 --max-time 25 " + shellQuote(asdfDownloadURL) + " -o \"$archive\"; then " +
			"curl -fL --connect-timeout 8 --max-time 120 --retry 2 --retry-all-errors --retry-delay 2 \"$fallback_url\" -o \"$archive\"; fi",
		"printf '%s  %s\\n' " + shellQuote(asdfSHA256) + " \"$archive\" | sha256sum -c -",
		"tar -C /usr/local/bin -xzf \"$archive\"",
		"chmod 0755 " + asdfBinary,
		asdfBinary + " version | grep -F " + shellQuote("v"+asdfVersion),
	}, "; ")
}
