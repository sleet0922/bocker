package bocker

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Incusfile 是 YAML 构建文件归一化后的内部 AST，支持多阶段构建。
//
// YAML parser 将 exec、shell、pkg、copy、env、workdir、mise、download 和 write
// 步骤归一化到 BuildStep。
type Incusfile struct {
	Path   string
	Stages []Stage
	Args   []ArgSpec
	Mirror string
	// 以下字段从最终阶段同步，供镜像属性和运行时恢复逻辑读取。
	From       string
	Name       string
	Network    string
	Steps      []BuildStep
	Exposes    []PortSpec
	Domain     string
	Autostart  *bool
	Entrypoint []string
	Cmd        []string
	Env        []EnvSpec
}

// Stage 表示一个构建阶段 (FROM ... AS ...)。
// 多阶段构建时，中间阶段的容器在构建完成后清理，最终阶段发布为镜像。
type Stage struct {
	Name            string      // AS 后的名字，用于 COPY --from=<name> 引用
	From            string      // 基础镜像 (已规范化)
	BaseFingerprint string      // 可选的固定基础镜像 fingerprint
	Steps           []BuildStep // YAML 步骤按出现顺序执行
	Exposes         []PortSpec  // EXPOSE (运行时指令，通常在最终阶段)
	Domain          string      // DOMAIN
	Autostart       *bool       // AUTOSTART
	Entrypoint      []string    // ENTRYPOINT executable and fixed arguments
	Cmd             []string    // CMD executable/arguments or default ENTRYPOINT arguments
}

// BuildStep 是一个有序的 YAML 构建步骤。
type BuildStep struct {
	Kind        string // "EXEC", "SHELL", "PKG", "COPY", "ENV", "WORKDIR", "MISE", "DOWNLOAD", "WRITE"
	Run         string
	ExecCommand string
	ExecArgs    []string
	ExecCapture string
	Packages    []string
	Copy        CopySpec
	Env         EnvSpec
	Workdir     string
	Mise        MiseSpec
	Download    DownloadSpec
	Write       WriteSpec
	Service     ServiceSpec
}

// MiseSpec requests one exact tool version in a disposable build stage.
type MiseSpec struct {
	Tool    string
	Version string
}

type CopySpec struct {
	// Sources contains one or more context/stage paths. Src is retained as a
	// compatibility alias for callers that only handle the single-source form.
	Sources []string
	Src     string
	Dst     string
	From    string // --from=stage_name 或 stage_index, 空表示从宿主机复制
}

type EnvSpec struct {
	Key   string
	Value string
}

type PortSpec struct {
	Port     int
	Protocol string
}

// DownloadSpec describes one verified archive download with ordered fallbacks.
type DownloadSpec struct {
	Output   string
	Extract  string
	Verify   *FileVerifySpec
	Attempts []DownloadAttempt
}

type DownloadAttempt struct {
	URL     string
	SHA256  string
	Format  string
	Timeout int
	Tries   int
	Move    *MoveSpec
}

type MoveSpec struct {
	From string
	To   string
}

type FileVerifySpec struct {
	Path    string
	Pattern string
	Value   string
}

type WriteSpec struct {
	Path    string
	Content string
	Mode    string
}

type ServiceSpec struct {
	Start  []string
	Stop   []string
	Enable []string
}

// parseIncusfile 从指定路径解析 Incusfile。path 为空时默认 ./Incusfile。
func parseIncusfile(path string) (*Incusfile, error) {
	return parseIncusfileWithBuildArgs(path, nil)
}

func parseIncusfileWithBuildArgs(path string, overrides map[string]string) (*Incusfile, error) {
	return parseYAMLBuildFile(path, overrides)
}

func validateStageName(name string) error {
	if name == "" {
		return fmt.Errorf("阶段名不能为空")
	}
	allDigits := true
	for i := 0; i < len(name); i++ {
		c := name[i]
		isAlphaNum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if i == 0 && !isAlphaNum {
			return fmt.Errorf("阶段名 %q 必须以字母或数字开头", name)
		}
		if !isAlphaNum && c != '_' && c != '.' && c != '-' {
			return fmt.Errorf("阶段名 %q 含有非法字符", name)
		}
		if c < '0' || c > '9' {
			allDigits = false
		}
	}
	if allDigits {
		return fmt.Errorf("阶段名 %q 不能是纯数字，以免与阶段索引混淆", name)
	}
	return nil
}

// normalizeImageRef 兼容 Docker 风格 "debian:12" 与 Incus 风格 "debian/12"。
// 形如 "debian:12" -> "debian/12"；"debian/12" 不变。
func normalizeImageRef(ref string) string {
	ref = strings.TrimSpace(ref)
	remote, image := splitImageRemote(ref)
	if strings.Contains(image, ":") && !strings.Contains(image, "/") {
		image = strings.Replace(image, ":", "/", 1)
	}
	if remote != "" {
		return remote + ":" + image
	}
	return image
}

// splitPinnedImageRef separates an optional immutable 64-hex image
// fingerprint. Pinning is explicit so aliases remain convenient for
// exploratory builds while release builds can be reproducible.
func splitPinnedImageRef(ref string) (image, fingerprint string, err error) {
	image, fingerprint, pinned := strings.Cut(ref, "@")
	if !pinned {
		return ref, "", nil
	}
	if image == "" || len(fingerprint) != 64 {
		return "", "", fmt.Errorf("镜像指纹必须是 64 位十六进制值")
	}
	for _, c := range fingerprint {
		if !unicode.Is(unicode.ASCII_Hex_Digit, c) {
			return "", "", fmt.Errorf("镜像指纹必须是 64 位十六进制值")
		}
	}
	return image, strings.ToLower(fingerprint), nil
}

// splitImageRemote separates Bocker's optional Incus-style remote prefix from
// the image reference. A second colon belongs to a Docker-style image tag.
func splitImageRemote(ref string) (remote, image string) {
	idx := strings.IndexByte(ref, ':')
	if idx <= 0 {
		return "", ref
	}
	prefix, rest := ref[:idx], ref[idx+1:]
	firstPart := rest
	if slash := strings.IndexByte(firstPart, '/'); slash >= 0 {
		firstPart = firstPart[:slash]
	}
	isPort := firstPart != ""
	for _, c := range firstPart {
		if c < '0' || c > '9' {
			isPort = false
			break
		}
	}
	if !strings.Contains(prefix, "/") && !isPort && rest != "" && (strings.Contains(rest, "/") || strings.Contains(rest, ":")) {
		return prefix, rest
	}
	return "", ref
}

func validateFinalStageRuntimeDirectives(stages []Stage) error {
	for i := 0; i < len(stages)-1; i++ {
		stage := stages[i]
		if len(stage.Exposes) > 0 || stage.Domain != "" || stage.Autostart != nil || len(stage.Entrypoint) > 0 || len(stage.Cmd) > 0 {
			label := stage.Name
			if label == "" {
				label = strconv.Itoa(i)
			}
			return fmt.Errorf("阶段 %q 包含仅允许在最终阶段使用的运行时指令 (EXPOSE DOMAIN AUTOSTART ENTRYPOINT CMD)", label)
		}
	}
	if len(stages) > 0 {
		seen := make(map[string]bool)
		for _, expose := range stages[len(stages)-1].Exposes {
			key := strconv.Itoa(expose.Port) + "/" + expose.Protocol
			if seen[key] {
				return fmt.Errorf("最终阶段重复声明 EXPOSE %s", key)
			}
			seen[key] = true
		}
	}
	return nil
}

// shellSplit 按 shell 风格处理空白、单双引号和反斜杠，但不执行变量展开。
func shellSplit(s string) ([]string, error) {
	var result []string
	var cur strings.Builder
	var quote byte
	escaped := false
	tokenStarted := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			cur.WriteByte(c)
			escaped = false
			tokenStarted = true
			continue
		}
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case quote == '"':
			if c == '"' {
				quote = 0
			} else if c == '\\' {
				nextIsEscapable := false
				if i+1 < len(s) {
					next := s[i+1]
					nextIsEscapable = next == '\\' || next == '"' || next == '$' || next == '`' || next == '\n'
				}
				if nextIsEscapable {
					escaped = true
				} else {
					cur.WriteByte(c)
				}
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			tokenStarted = true
		case c == '\\':
			escaped = true
			tokenStarted = true
		case c == ' ' || c == '\t':
			if tokenStarted {
				result = append(result, cur.String())
				cur.Reset()
				tokenStarted = false
			}
		default:
			cur.WriteByte(c)
			tokenStarted = true
		}
	}
	if escaped {
		return nil, fmt.Errorf("末尾反斜杠缺少转义字符")
	}
	if quote != 0 {
		return nil, fmt.Errorf("未闭合的引号")
	}
	if tokenStarted {
		result = append(result, cur.String())
	}
	return result, nil
}

func validateEnvKey(key string) error {
	if err := validateVariableKey(key); err != nil {
		return fmt.Errorf("ENV 变量名 %q 无效", key)
	}
	return nil
}

func parseBoolPayload(payload string) (bool, error) {
	p := strings.ToLower(strings.TrimSpace(payload))
	switch p {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("期望 on/off, 得到 %q", payload)
}

// exposeString 序列化端口列表为紧凑字符串 (用于镜像属性)。
func exposeString(ports []PortSpec) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
	}
	return strings.Join(parts, ",")
}

// parseExposeString 从镜像属性恢复端口列表。
func parseExposeString(s string) []PortSpec {
	if s == "" {
		return nil
	}
	var result []PortSpec
	for _, part := range strings.Split(s, ",") {
		proto := "tcp"
		portStr := part
		if idx := strings.LastIndex(part, "/"); idx > 0 {
			proto = strings.ToLower(part[idx+1:])
			portStr = part[:idx]
		}
		if proto != "tcp" && proto != "udp" {
			continue
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		result = append(result, PortSpec{Port: port, Protocol: proto})
	}
	return result
}
