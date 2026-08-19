package bocker

import (
	"fmt"
	"strings"
)

const (
	miseVersion              = "2026.8.8"
	miseDownloadURL          = "https://github.com/jdx/mise/releases/download/v2026.8.8/mise-v2026.8.8-linux-x64.tar.gz"
	miseDownloadProxy        = "https://gh.nxnow.top"
	miseSHA256               = "58edfbdba6d4255b6536a61daeaf3b21f7a059430c789e948c8494ba32d59e1f"
	miseBinary               = "/usr/local/bin/mise"
	miseDataDir              = "/opt/bocker-mise"
	miseConfigFile           = miseDataDir + "/config.toml"
	miseShimsDir             = miseDataDir + "/shims"
	miseDownloadProxyEnvName = "MISE_DOWNLOAD_PROXY"
	defaultBuildPath         = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

// parseMisePayload parses the compact MISE <tool> <version> syntax.
func parseMisePayload(payload string) (MiseSpec, error) {
	parts, err := shellSplit(payload)
	if err != nil || len(parts) != 2 {
		return MiseSpec{}, fmt.Errorf("MISE 用法: MISE <tool> <exact-version>，如 MISE go 1.26.6")
	}
	return MiseSpec{Tool: parts[0], Version: parts[1]}, nil
}

func normalizeMiseSpec(spec MiseSpec) (MiseSpec, error) {
	spec.Tool = strings.ToLower(strings.TrimSpace(spec.Tool))
	switch spec.Tool {
	case "golang":
		spec.Tool = "go"
	case "nodejs":
		spec.Tool = "node"
	case "postgresql":
		spec.Tool = "postgres"
	}
	if !validMiseToken(spec.Tool) {
		return MiseSpec{}, fmt.Errorf("MISE 工具名 %q 无效", spec.Tool)
	}
	spec.Version = strings.TrimSpace(spec.Version)
	if !validMiseToken(spec.Version) || spec.Version == "latest" || spec.Version == "system" || strings.HasPrefix(spec.Version, "latest:") || strings.HasPrefix(spec.Version, "prefix:") {
		return MiseSpec{}, fmt.Errorf("MISE 版本 %q 无效；必须指定精确版本", spec.Version)
	}
	return spec, nil
}

func validMiseToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		if i > 0 && (c == '.' || c == '_' || c == '+' || c == '-' || c == ':') {
			continue
		}
		return false
	}
	return true
}

func configureMiseBuildEnvironment(spec MiseSpec, runEnv map[string]string) {
	runEnv["MISE_DATA_DIR"] = miseDataDir
	runEnv["MISE_GLOBAL_CONFIG_FILE"] = miseConfigFile
	runEnv["MISE_CACHE_DIR"] = miseDataDir + "/cache"
	runEnv["MISE_DISABLE_HINTS"] = "1"
	runEnv["MISE_JOBS"] = "1"
	if _, configured := runEnv[miseDownloadProxyEnvName]; !configured {
		runEnv[miseDownloadProxyEnvName] = miseDownloadProxy
	}
	currentPath := runEnv["PATH"]
	if currentPath == "" {
		currentPath = defaultBuildPath
	}
	if !strings.Contains(":"+currentPath+":", ":"+miseShimsDir+":") {
		runEnv["PATH"] = miseShimsDir + ":" + currentPath
	}
	defaults := map[string]string{}
	switch spec.Tool {
	case "go":
		defaults = map[string]string{
			"GOPROXY": "https://goproxy.cn,direct",
			"GOSUMDB": "sum.golang.google.cn",
			// golang.google.cn serves the archive but redirects its .sha256 URL
			// to an HTML page, which mise correctly rejects. Keep checksum
			// verification on Google's mirror and use goproxy.cn for modules.
			"MISE_GO_DOWNLOAD_MIRROR": "https://dl.google.com/go",
		}
	case "node":
		defaults = map[string]string{
			"MISE_NODE_MIRROR_URL": "https://npmmirror.com/mirrors/node/",
			"NPM_CONFIG_REGISTRY":  "https://registry.npmmirror.com",
		}
	case "python":
		defaults = map[string]string{
			"PIP_INDEX_URL":                 "https://pypi.tuna.tsinghua.edu.cn/simple",
			"PIP_DISABLE_PIP_VERSION_CHECK": "1",
		}
	case "rust":
		defaults = map[string]string{
			"RUSTUP_HOME":                         miseDataDir + "/rustup",
			"CARGO_HOME":                          miseDataDir + "/cargo",
			"RUSTUP_DIST_SERVER":                  "https://rsproxy.cn",
			"RUSTUP_UPDATE_ROOT":                  "https://rsproxy.cn/rustup",
			"CARGO_REGISTRIES_CRATES_IO_INDEX":    "sparse+https://rsproxy.cn/index/",
			"CARGO_REGISTRIES_CRATES_IO_PROTOCOL": "sparse",
		}
	}
	for key, value := range defaults {
		if _, configured := runEnv[key]; !configured {
			runEnv[key] = value
		}
	}
	if spec.Tool == "rust" {
		cargoBin := miseDataDir + "/cargo/bin"
		if !strings.Contains(":"+runEnv["PATH"]+":", ":"+cargoBin+":") {
			runEnv["PATH"] = cargoBin + ":" + runEnv["PATH"]
		}
	}
}

func miseInstallCommand(spec MiseSpec) string {
	toolVersion := shellQuote(spec.Tool + "@" + spec.Version)
	probe := spec.Tool
	probeArgs := "--version"
	if spec.Tool == "go" {
		probeArgs = "version"
	}
	if spec.Tool == "rust" {
		probe = "rustc"
	} else if spec.Tool == "redis" {
		probe = "redis-server"
	}
	return strings.Join([]string{
		"set -eu",
		"if [ ! -x " + miseBinary + " ]; then " + miseBootstrapCommand() + "; fi",
		"mkdir -p " + shellQuote(miseDataDir),
		miseBinary + " use --global --pin --yes " + toolVersion,
		miseBinary + " reshim --yes",
		miseBinary + " exec -- " + shellQuote(probe) + " " + probeArgs,
		miseBinary + " where " + shellQuote(spec.Tool+"@"+spec.Version),
	}, "; ")
}

func miseBootstrapCommand() string {
	return strings.Join([]string{
		"if command -v apt-get >/dev/null 2>&1; then apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends bash ca-certificates coreutils curl git tar xz-utils; " +
			"elif command -v apk >/dev/null 2>&1; then apk add --no-cache bash ca-certificates coreutils curl git tar xz; " +
			"else echo 'MISE 目前只支持 Debian/Ubuntu 和 Alpine 构建阶段' >&2; exit 1; fi",
		"test \"$(uname -m)\" = x86_64",
		"archive=$(mktemp /tmp/bocker-mise.XXXXXX.tar.gz)",
		"extract_dir=$(mktemp -d /tmp/bocker-mise.XXXXXX)",
		"trap 'rm -f \"$archive\"; rm -rf \"$extract_dir\"' EXIT",
		"download_proxy=\"${" + miseDownloadProxyEnvName + ":-" + miseDownloadProxy + "}\"",
		"fallback_url=\"${download_proxy%/}/" + miseDownloadURL + "\"",
		"if ! curl -fL --connect-timeout 8 --max-time 25 " + shellQuote(miseDownloadURL) + " -o \"$archive\"; then " +
			"curl -fL --connect-timeout 8 --max-time 120 --retry 2 --retry-all-errors --retry-delay 2 \"$fallback_url\" -o \"$archive\"; fi",
		"printf '%s  %s\\n' " + shellQuote(miseSHA256) + " \"$archive\" | sha256sum -c -",
		"tar -xzf \"$archive\" -C \"$extract_dir\"",
		"install -m 0755 \"$extract_dir/mise/bin/mise\" " + miseBinary,
		miseBinary + " version | grep -F " + shellQuote(miseVersion),
	}, "; ")
}
