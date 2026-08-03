package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Incusfile 是 Dockerfile 风格的 Incus 镜像构建描述文件，支持多阶段构建。
// bocker build 读取该文件，按顺序执行各构建阶段，最后将最终阶段容器发布为镜像。
//
// 支持的指令：
//
//	FROM <image> [AS <name>]        基础镜像，开始新构建阶段 (多阶段)
//	NAME <name>                     镜像别名 + 容器名 (全局，作用于最终镜像)
//	NETWORK bridge|nat              网络模式 (全局，默认 bridge)
//	WORKDIR <path>                  设置后续 RUN/COPY 的工作目录
//	RUN <command>                   在容器内执行 shell 命令 (通过 /bin/sh -c)
//	COPY [--from=<stage>] <src> <dst>  从宿主机或指定阶段复制文件/目录
//	    --from 省略时从宿主机复制；指定阶段名或数字索引时从该阶段容器复制
//	ENV <KEY>=<VALUE>               设置环境变量 (写入 /etc/environment + profile.d)
//	EXPOSE <port>[/<proto>] ...     声明端口映射 (运行时自动创建)
//	DOMAIN <domain>                 域名映射 (运行时写入 /etc/hosts)
//	AUTOSTART on|off                开机自启动
//	TEMP <name> ... END             临时构建块: 块内步骤在独立临时容器执行，不进最终镜像
//	    块继承外层 FROM 镜像，用于隔离编译工具链 (golang/nodejs) 污染
//	    块名可用于 COPY --from=<name> 拷回构建产物；仅支持单 FROM (all-in-one 模式)
type Incusfile struct {
	Path   string
	Stages []Stage
	// 以下字段从最终阶段同步，保持向后兼容 (buildImageProperties 等函数直接读取)
	From       string
	Name       string
	Network    string
	Steps      []BuildStep
	Exposes    []PortSpec
	Domain     string
	Autostart  *bool
	Entrypoint []string
	Cmd        []string
}

// Stage 表示一个构建阶段 (FROM ... AS ...)。
// 多阶段构建时，中间阶段的容器在构建完成后清理，最终阶段发布为镜像。
type Stage struct {
	Name       string      // AS 后的名字，用于 COPY --from=<name> 引用
	From       string      // 基础镜像 (已规范化)
	Steps      []BuildStep // RUN/COPY/ENV/WORKDIR 按出现顺序执行
	Exposes    []PortSpec  // EXPOSE (运行时指令，通常在最终阶段)
	Domain     string      // DOMAIN
	Autostart  *bool       // AUTOSTART
	Entrypoint []string    // ENTRYPOINT executable and fixed arguments
	Cmd        []string    // CMD executable/arguments or default ENTRYPOINT arguments
}

// BuildStep 是一个有序的构建步骤 (RUN/COPY/ENV/WORKDIR)。
type BuildStep struct {
	Kind    string // "RUN", "COPY", "ENV", "WORKDIR"
	Run     string
	Copy    CopySpec
	Env     EnvSpec
	Workdir string
}

type CopySpec struct {
	Src  string
	Dst  string
	From string // --from=stage_name 或 stage_index, 空表示从宿主机复制
}

type EnvSpec struct {
	Key   string
	Value string
}

type PortSpec struct {
	Port     int
	Protocol string
}

// parseIncusfile 从指定路径解析 Incusfile。path 为空时默认 ./Incusfile。
// 支持行尾反斜杠续行 (与 Dockerfile 一致)：行尾 \ 会把下一行拼接到当前行。
func parseIncusfile(path string) (*Incusfile, error) {
	if path == "" {
		path = "Incusfile"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	f := &Incusfile{Path: path}
	// 规范化换行：去掉 \r (兼容 Windows CRLF)，再按 \n 切分
	cleaned := strings.ReplaceAll(string(data), "\r\n", "\n")
	cleaned = strings.ReplaceAll(cleaned, "\r", "\n")
	// 预处理：合并以 \ 结尾的续行，记录每条逻辑行起始的物理行号
	rawLines := strings.Split(cleaned, "\n")
	type logicalLine struct {
		no   int
		text string
	}
	var logical []logicalLine
	for i := 0; i < len(rawLines); i++ {
		startNo := i + 1
		cur := rawLines[i]
		// 注释 / 空行不参与续行
		trimmed := strings.TrimSpace(cur)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			logical = append(logical, logicalLine{no: startNo, text: cur})
			continue
		}
		// 合并后续以 \ 结尾的行 (容忍反斜杠后的尾随空白，跳过续行中的注释/空行)
		for {
			trimmedCur := strings.TrimRight(cur, " \t")
			if !hasLineContinuation(trimmedCur) {
				break
			}
			if i+1 >= len(rawLines) {
				return nil, fmt.Errorf("line %d: 续行反斜杠后缺少内容", startNo)
			}
			cur = strings.TrimSuffix(trimmedCur, "\\")
			i++
			// 跳过续行中的注释行和空行 (与 Dockerfile 行为一致：注释在续行中被移除)
			for i < len(rawLines) {
				next := strings.TrimSpace(rawLines[i])
				if next == "" || strings.HasPrefix(next, "#") {
					i++
					continue
				}
				break
			}
			if i >= len(rawLines) {
				return nil, fmt.Errorf("line %d: 续行反斜杠后缺少内容", startNo)
			}
			cur += " " + strings.TrimLeft(rawLines[i], " \t")
		}
		logical = append(logical, logicalLine{no: startNo, text: cur})
	}

	// 解析逻辑行：遇到 FROM 开始新阶段
	var currentStage *Stage
	stageNameLines := map[string]int{}

	// TEMP 块状态: TEMP <name> ... END 内的步骤在一个独立临时容器执行，
	// 不进入最终镜像。常用于编译型语言 (golang/nodejs) 隔离构建工具链污染。
	// TEMP 块在解析后转换为前置 Stage，复用外层 FROM 镜像，主 FROM 阶段作为最终阶段。
	var tempStages []Stage
	var inTemp bool
	var currentTemp *Stage

	startStage := func(fromImg, stageName string, lineNo int) error {
		if fromImg == "" {
			return fmt.Errorf("line %d: FROM 需要镜像引用", lineNo)
		}
		if currentStage != nil {
			f.Stages = append(f.Stages, *currentStage)
		}
		if stageName != "" {
			if err := validateStageName(stageName); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
			key := strings.ToLower(stageName)
			if previous, exists := stageNameLines[key]; exists {
				return fmt.Errorf("line %d: 阶段名 %q 重复 (首次位于 line %d)", lineNo, stageName, previous)
			}
			stageNameLines[key] = lineNo
		}
		currentStage = &Stage{
			From: normalizeImageRef(fromImg),
			Name: stageName,
		}
		return nil
	}

	// targetStage 返回当前指令应作用的目标阶段: TEMP 块内返回临时阶段，否则返回主阶段。
	targetStage := func() *Stage {
		if inTemp {
			return currentTemp
		}
		return currentStage
	}

	for _, ll := range logical {
		lineNo := ll.no
		raw := ll.text
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		directive := strings.ToUpper(fields[0])
		payload := directivePayload(raw)
		switch directive {
		case "FROM":
			if inTemp {
				return nil, fmt.Errorf("line %d: TEMP 块内不能有 FROM", lineNo)
			}
			fromImg, stageName, err := parseFromPayload(payload)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			if err := startStage(fromImg, stageName, lineNo); err != nil {
				return nil, err
			}
		case "TEMP":
			// TEMP <name> 开始一个临时构建块: 块内步骤在独立临时容器执行，不进最终镜像。
			// 块继承外层 FROM 镜像。需用 END 关闭。块名可用于 COPY --from=<name>。
			if inTemp {
				return nil, fmt.Errorf("line %d: TEMP 不能嵌套", lineNo)
			}
			if currentStage == nil {
				return nil, fmt.Errorf("line %d: TEMP 必须在 FROM 之后", lineNo)
			}
			nameFields := strings.Fields(payload)
			if len(nameFields) == 0 {
				return nil, fmt.Errorf("line %d: TEMP 需要名称 (如 TEMP builder)", lineNo)
			}
			if len(nameFields) != 1 {
				return nil, fmt.Errorf("line %d: TEMP 只接受一个阶段名", lineNo)
			}
			name := nameFields[0]
			if err := validateStageName(name); err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			key := strings.ToLower(name)
			if previous, exists := stageNameLines[key]; exists {
				return nil, fmt.Errorf("line %d: 阶段名 %q 重复 (首次位于 line %d)", lineNo, name, previous)
			}
			stageNameLines[key] = lineNo
			inTemp = true
			currentTemp = &Stage{Name: name, From: currentStage.From}
		case "END":
			// END 关闭最近的 TEMP 块。
			if !inTemp {
				return nil, fmt.Errorf("line %d: END 没有匹配的 TEMP", lineNo)
			}
			tempStages = append(tempStages, *currentTemp)
			inTemp = false
			currentTemp = nil
		case "NAME":
			parts, err := shellSplit(payload)
			if err != nil || len(parts) != 1 {
				return nil, fmt.Errorf("line %d: NAME 只接受一个名称", lineNo)
			}
			if err := validateBockerName(parts[0]); err != nil {
				return nil, fmt.Errorf("line %d: NAME 无效: %w", lineNo, err)
			}
			f.Name = parts[0]
		case "NETWORK":
			if inTemp {
				return nil, fmt.Errorf("line %d: NETWORK 不能位于 TEMP 块内", lineNo)
			}
			if strings.TrimSpace(payload) == "" {
				return nil, fmt.Errorf("line %d: NETWORK 需要 bridge 或 nat", lineNo)
			}
			mode, err := ParseNetworkMode(payload)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			f.Network = string(mode)
		case "WORKDIR":
			if targetStage() == nil {
				return nil, fmt.Errorf("line %d: WORKDIR 必须在 FROM 之后", lineNo)
			}
			parts, err := shellSplit(payload)
			if err != nil || len(parts) != 1 || parts[0] == "" {
				return nil, fmt.Errorf("line %d: WORKDIR 需要一个非空路径", lineNo)
			}
			targetStage().Steps = append(targetStage().Steps, BuildStep{Kind: "WORKDIR", Workdir: parts[0]})
		case "RUN":
			if targetStage() == nil {
				return nil, fmt.Errorf("line %d: RUN 必须在 FROM 之后", lineNo)
			}
			if payload == "" {
				return nil, fmt.Errorf("line %d: RUN 需要命令", lineNo)
			}
			targetStage().Steps = append(targetStage().Steps, BuildStep{Kind: "RUN", Run: payload})
		case "COPY":
			if targetStage() == nil {
				return nil, fmt.Errorf("line %d: COPY 必须在 FROM 之后", lineNo)
			}
			from, src, dst, err := parseCopyPayload(payload)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			targetStage().Steps = append(targetStage().Steps, BuildStep{Kind: "COPY", Copy: CopySpec{Src: src, Dst: dst, From: from}})
		case "ENV":
			if targetStage() == nil {
				return nil, fmt.Errorf("line %d: ENV 必须在 FROM 之后", lineNo)
			}
			kv, err := parseEnvPayload(payload)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			targetStage().Steps = append(targetStage().Steps, BuildStep{Kind: "ENV", Env: kv})
		case "EXPOSE":
			if targetStage() == nil {
				return nil, fmt.Errorf("line %d: EXPOSE 必须在 FROM 之后", lineNo)
			}
			ports, err := parseExposePayload(payload)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			targetStage().Exposes = append(targetStage().Exposes, ports...)
		case "DOMAIN":
			if targetStage() == nil {
				return nil, fmt.Errorf("line %d: DOMAIN 必须在 FROM 之后", lineNo)
			}
			if payload == "" {
				return nil, fmt.Errorf("line %d: DOMAIN 需要域名", lineNo)
			}
			parts, err := shellSplit(payload)
			if err != nil || len(parts) != 1 {
				return nil, fmt.Errorf("line %d: DOMAIN 只接受一个域名", lineNo)
			}
			if err := validateDomainName(parts[0]); err != nil {
				return nil, fmt.Errorf("line %d: DOMAIN 无效: %w", lineNo, err)
			}
			targetStage().Domain = parts[0]
		case "AUTOSTART":
			if targetStage() == nil {
				return nil, fmt.Errorf("line %d: AUTOSTART 必须在 FROM 之后", lineNo)
			}
			on, err := parseBoolPayload(payload)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			targetStage().Autostart = &on
		case "ENTRYPOINT", "CMD":
			if targetStage() == nil {
				return nil, fmt.Errorf("line %d: %s must appear after FROM", lineNo, directive)
			}
			command, err := parseCommandPayload(payload)
			if err != nil {
				return nil, fmt.Errorf("line %d: %s: %w", lineNo, directive, err)
			}
			if directive == "ENTRYPOINT" {
				targetStage().Entrypoint = command
			} else {
				targetStage().Cmd = command
			}
		default:
			return nil, fmt.Errorf("line %d: 未知指令 %s (支持: FROM NAME NETWORK WORKDIR RUN COPY ENV EXPOSE DOMAIN AUTOSTART ENTRYPOINT CMD TEMP END)", lineNo, directive)
		}
	}

	// 保存最后一个阶段
	if currentStage != nil {
		f.Stages = append(f.Stages, *currentStage)
	}
	if len(f.Stages) == 0 {
		return nil, fmt.Errorf("%s 缺少 FROM 指令", path)
	}

	// TEMP 块后处理: 校验闭合 + 校验单 FROM + 转换为前置阶段
	if inTemp {
		return nil, fmt.Errorf("%s: TEMP 块未用 END 关闭", path)
	}
	if len(tempStages) > 0 {
		// TEMP 块仅支持单 FROM (all-in-one 模式)；多 FROM 应直接用多阶段构建
		if len(f.Stages) > 1 {
			return nil, fmt.Errorf("%s: TEMP 块不支持多 FROM (多阶段构建请用 FROM ... AS, 不要混用 TEMP)", path)
		}
		// TEMP 块作为前置阶段，主 FROM 阶段作为最终阶段 (最后发布)
		f.Stages = append(tempStages, f.Stages...)
	}

	// 同步最终阶段到顶层字段 (保持向后兼容，buildImageProperties 等函数直接读取)
	last := &f.Stages[len(f.Stages)-1]
	f.From = last.From
	f.Steps = last.Steps
	f.Exposes = last.Exposes
	f.Domain = last.Domain
	f.Autostart = last.Autostart
	f.Entrypoint = append([]string(nil), last.Entrypoint...)
	f.Cmd = append([]string(nil), last.Cmd...)
	return f, nil
}

// parseCommandPayload accepts Docker-compatible JSON arrays and a shell-like
// whitespace form. Both forms resolve to an argv array; shell execution is
// intentionally not implied. Bocker runs the result as an automatic service.
func parseCommandPayload(payload string) ([]string, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, fmt.Errorf("command cannot be empty")
	}
	var parts []string
	if strings.HasPrefix(payload, "[") {
		if err := json.Unmarshal([]byte(payload), &parts); err != nil {
			return nil, fmt.Errorf("invalid JSON argv: %w", err)
		}
	} else {
		var err error
		parts, err = shellSplit(payload)
		if err != nil {
			return nil, err
		}
	}
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return nil, fmt.Errorf("command requires an executable")
	}
	for _, part := range parts {
		if strings.IndexByte(part, 0) >= 0 {
			return nil, fmt.Errorf("command arguments cannot contain NUL")
		}
	}
	return parts, nil
}

// parseFromPayload 解析 FROM 指令的 payload。
// 格式: <image> [AS <name>]
// 支持示例:
//
//	FROM debian/12
//	FROM debian:12 AS builder
//	FROM golang:1.26-alpine AS frontend-build
func parseFromPayload(payload string) (image, stageName string, err error) {
	fields := strings.Fields(payload)
	switch len(fields) {
	case 0:
		return "", "", fmt.Errorf("FROM 需要镜像引用")
	case 1:
		return fields[0], "", nil
	case 3:
		if !strings.EqualFold(fields[1], "AS") {
			return "", "", fmt.Errorf("FROM 用法: FROM <image> [AS <stage>]")
		}
		if err := validateStageName(fields[2]); err != nil {
			return "", "", err
		}
		return fields[0], fields[2], nil
	default:
		return "", "", fmt.Errorf("FROM 用法: FROM <image> [AS <stage>]")
	}
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

// directivePayload 提取指令关键字后的全部内容，保留空格、引号等。
func directivePayload(rawLine string) string {
	trimmed := strings.TrimLeft(rawLine, " \t")
	idx := strings.IndexAny(trimmed, " \t")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[idx+1:])
}

// normalizeImageRef 兼容 Docker 风格 "debian:12" 与 Incus 风格 "debian/12"。
// 形如 "debian:12" -> "debian/12"；"debian/12" 不变。
func normalizeImageRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.Contains(ref, ":") && !strings.Contains(ref, "/") {
		return strings.Replace(ref, ":", "/", 1)
	}
	return ref
}

// parseCopyPayload 解析 COPY 指令的 payload，支持 --from 标志和引号路径。
// 格式: [--from=<stage>] <src> <dst>
// --from 可以是阶段名 (如 "builder") 或数字索引 (如 "0")
// 示例:
//
//	COPY ./index.html /var/www/html/index.html
//	COPY --from=builder /app/bin/app /usr/local/bin/app
//	COPY --from=0 /src/public /app/public
func parseCopyPayload(payload string) (from, src, dst string, err error) {
	rest := strings.TrimSpace(payload)
	// 检查 --from= 标志
	if strings.HasPrefix(rest, "--from=") {
		spaceIdx := strings.IndexAny(rest, " \t")
		if spaceIdx < 0 {
			return "", "", "", fmt.Errorf("COPY --from 需要源和目标路径")
		}
		from = strings.TrimPrefix(rest[:spaceIdx], "--from=")
		if from == "" {
			return "", "", "", fmt.Errorf("COPY --from 的阶段名不能为空")
		}
		if _, numericErr := strconv.Atoi(from); numericErr != nil {
			if err := validateStageName(from); err != nil {
				return "", "", "", fmt.Errorf("COPY --from 无效: %w", err)
			}
		} else if strings.HasPrefix(from, "-") {
			return "", "", "", fmt.Errorf("COPY --from 的阶段索引不能为负数")
		}
		rest = strings.TrimLeft(rest[spaceIdx:], " \t")
	}
	fields, err := shellSplit(rest)
	if err != nil {
		return "", "", "", fmt.Errorf("COPY 用法: COPY [--from=<stage>] <src> <dst>: %w", err)
	}
	if len(fields) != 2 {
		return "", "", "", fmt.Errorf("COPY 用法: COPY [--from=<stage>] <src> <dst> (得到 %d 个参数)", len(fields))
	}
	if fields[0] == "" || fields[1] == "" {
		return "", "", "", fmt.Errorf("COPY 的源路径和目标路径不能为空")
	}
	return from, fields[0], fields[1], nil
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

func hasLineContinuation(line string) bool {
	count := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		count++
	}
	return count%2 == 1
}

func parseEnvPayload(payload string) (EnvSpec, error) {
	parts, err := shellSplit(payload)
	if err != nil {
		return EnvSpec{}, fmt.Errorf("ENV 解析失败: %w", err)
	}
	if len(parts) == 0 {
		return EnvSpec{}, fmt.Errorf("ENV 用法: ENV KEY=VALUE 或 ENV KEY VALUE")
	}
	key := parts[0]
	valueParts := parts[1:]
	if idx := strings.IndexByte(parts[0], '='); idx >= 0 {
		key = parts[0][:idx]
		valueParts = append([]string{parts[0][idx+1:]}, valueParts...)
	} else if len(parts) < 2 {
		return EnvSpec{}, fmt.Errorf("ENV 用法: ENV KEY=VALUE 或 ENV KEY VALUE")
	}
	if err := validateEnvKey(key); err != nil {
		return EnvSpec{}, err
	}
	return EnvSpec{Key: key, Value: strings.Join(valueParts, " ")}, nil
}

func validateEnvKey(key string) error {
	if key == "" {
		return fmt.Errorf("ENV 变量名不能为空")
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		valid := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9'
		if !valid {
			return fmt.Errorf("ENV 变量名 %q 无效", key)
		}
	}
	return nil
}

func parseExposePayload(payload string) ([]PortSpec, error) {
	fields := strings.Fields(payload)
	if len(fields) == 0 {
		return nil, fmt.Errorf("EXPOSE 需要至少一个端口")
	}
	var result []PortSpec
	for _, f := range fields {
		proto := "tcp"
		portStr := f
		if idx := strings.LastIndex(f, "/"); idx > 0 {
			proto = strings.ToLower(strings.TrimSpace(f[idx+1:]))
			if proto != "tcp" && proto != "udp" {
				return nil, fmt.Errorf("协议必须是 tcp 或 udp, 得到 %q", proto)
			}
			portStr = f[:idx]
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("端口 %q 无效", portStr)
		}
		result = append(result, PortSpec{Port: port, Protocol: proto})
	}
	return result, nil
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
