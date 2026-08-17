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
			"NODEJS_ORG_MIRROR":   "https://registry.npmmirror.com/-/binary/node/",
			"NPM_CONFIG_REGISTRY": "https://registry.npmmirror.com",
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
	return strings.Join([]string{
		"set -eu",
		"if [ ! -x " + asdfBinary + " ]; then " + asdfBootstrapCommand() + "; fi",
		"mkdir -p " + shellQuote(asdfDataDir),
		"if ! " + asdfBinary + " plugin list 2>/dev/null | grep -Fxq " + shellQuote(spec.Tool) + "; then " + pluginAdd + "; fi",
		asdfBinary + " install " + shellQuote(spec.Tool) + " " + shellQuote(spec.Version),
		asdfBinary + " set --home " + shellQuote(spec.Tool) + " " + shellQuote(spec.Version),
		asdfBinary + " reshim " + shellQuote(spec.Tool) + " " + shellQuote(spec.Version),
		asdfBinary + " where " + shellQuote(spec.Tool) + " " + shellQuote(spec.Version),
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
