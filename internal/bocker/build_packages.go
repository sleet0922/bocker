package bocker

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

const chinaPackageMirror = defaultAptMirrorURL

func normalizePackageMirror(value string) (string, error) {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "china", "tuna":
		return chinaPackageMirror, nil
	}
	if strings.ContainsAny(value, " \t\r\n'\"`$|;&<>\\") {
		return "", fmt.Errorf("MIRROR 地址包含不安全字符")
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("MIRROR 需要 china、tuna 或 http/https 镜像站根地址")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("MIRROR 地址不能包含认证信息、查询参数或片段")
	}
	return strings.TrimRight(value, "/"), nil
}

func parsePackagePayload(payload string) ([]string, error) {
	packages, err := shellSplit(payload)
	if err != nil || len(packages) == 0 {
		return nil, fmt.Errorf("PKG 至少需要一个软件包名称")
	}
	for _, pkg := range packages {
		if pkg == "" || len(pkg) > 256 || strings.HasPrefix(pkg, "-") || strings.IndexFunc(pkg, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r)
		}) >= 0 {
			return nil, fmt.Errorf("PKG 软件包名称 %q 无效", pkg)
		}
	}
	return packages, nil
}

func packageMirrorCommand(mirror string) string {
	aptScript := aptMirrorSedScript(mirror)
	return fmt.Sprintf(`set -eu
if command -v apt-get >/dev/null 2>&1; then
%s
elif command -v apk >/dev/null 2>&1; then
	mirror=%s
	test -f /etc/apk/repositories
	sed -i -E "s|https?://[^/]+/alpine|${mirror}/alpine|g" /etc/apk/repositories
	grep -F "${mirror}/alpine" /etc/apk/repositories >/dev/null
	echo "✔ Alpine 镜像源已切换为 ${mirror}/alpine"
else
	echo 'MIRROR 目前只支持 Debian、Ubuntu 和 Alpine' >&2
	exit 1
fi`, aptScript, shellQuote(mirror))
}

func packageInstallCommand(packages []string) string {
	quoted := make([]string, len(packages))
	for i, pkg := range packages {
		quoted[i] = shellQuote(pkg)
	}
	args := strings.Join(quoted, " ")
	return fmt.Sprintf(`set -eu
if command -v apt-get >/dev/null 2>&1; then
	apt-get update
	DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends %s
	rm -rf /var/lib/apt/lists/*
elif command -v apk >/dev/null 2>&1; then
	apk add --no-cache %s
else
	echo 'PKG 目前只支持 Debian、Ubuntu 和 Alpine' >&2
	exit 1
fi`, args, args)
}
