package bocker

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxIncusfileBytes bounds the amount of YAML accepted from an Incusfile.
// Incusfiles are configuration documents, not payload containers; 8 MiB is
// generous for embedded scripts while preventing regular files and streams
// from causing unbounded daemon memory growth.
const maxIncusfileBytes = 8 << 20

// Incusfile v2 deliberately describes a stage by intent rather than by a
// long list of mutually-exclusive step objects. The normalized BuildStep IR
// remains ordered so the executor does not need to know about YAML layout.
type v2BuildFile struct {
	Version int               `yaml:"version"`
	Args    map[string]string `yaml:"args"`
	Mirror  string            `yaml:"mirror"`
	Name    string            `yaml:"name"`
	Network string            `yaml:"network"`
	Base    string            `yaml:"base"`
	Stages  []v2Stage         `yaml:"stages"`
}

type v2Stage struct {
	Name      string                       `yaml:"name"`
	From      string                       `yaml:"from"`
	Workdir   string                       `yaml:"workdir"`
	Env       map[string]string            `yaml:"env"`
	Packages  []string                     `yaml:"packages"`
	Tools     map[string]string            `yaml:"tools"`
	Files     map[string][]string          `yaml:"files"`
	Artifacts map[string]map[string]string `yaml:"artifacts"`
	Fetch     []v2Fetch                    `yaml:"fetch"`
	Commands  []v2Command                  `yaml:"commands"`
	Runtime   *v2Runtime                   `yaml:"runtime"`
}

type v2Fetch struct {
	URL     string  `yaml:"url"`
	Output  string  `yaml:"output"`
	Extract string  `yaml:"extract"`
	Format  string  `yaml:"format"`
	SHA256  string  `yaml:"sha256"`
	Timeout int     `yaml:"timeout"`
	Tries   int     `yaml:"tries"`
	Move    *v2Move `yaml:"move"`
}

type v2Move struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// v2Command accepts the compact argv form and an explicit mapping when a
// command needs a capture name or shell semantics:
//   - [go, build, -o, /out/app]
//   - shell: |
//     set -eu
//     ...
//   - run: [openssl, rand, -hex, "24"]
//     capture: PASSWORD
type v2Command struct {
	Run     []string
	Shell   string
	Capture string
}

func (c *v2Command) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		if err := node.Decode(&c.Run); err != nil {
			return err
		}
		return validateCommandArgv(c.Run)
	case yaml.MappingNode:
		allowed := map[string]bool{"run": true, "shell": true, "capture": true}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if !allowed[key] {
				return fmt.Errorf("commands 项包含未知字段 %q", key)
			}
		}
		var raw struct {
			Run     []string `yaml:"run"`
			Shell   string   `yaml:"shell"`
			Capture string   `yaml:"capture"`
		}
		if err := node.Decode(&raw); err != nil {
			return err
		}
		c.Run, c.Shell, c.Capture = raw.Run, raw.Shell, raw.Capture
		if len(c.Run) > 0 && strings.TrimSpace(c.Shell) != "" {
			return fmt.Errorf("commands 项只能包含 run 或 shell 之一")
		}
		if len(c.Run) == 0 && strings.TrimSpace(c.Shell) == "" {
			return fmt.Errorf("commands 项需要 run 或 shell")
		}
		if len(c.Run) > 0 {
			return validateCommandArgv(c.Run)
		}
		if c.Capture != "" {
			return fmt.Errorf("shell 命令不支持 capture")
		}
		return nil
	default:
		return fmt.Errorf("commands 项必须是 argv 列表或 run/shell 映射")
	}
}

func validateCommandArgv(argv []string) error {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return fmt.Errorf("命令不能为空")
	}
	for i, arg := range argv {
		if strings.IndexByte(arg, 0) >= 0 {
			return fmt.Errorf("命令参数 %d 不能包含 NUL 字符", i)
		}
	}
	return nil
}

type v2Runtime struct {
	Env        map[string]string `yaml:"env"`
	Entrypoint []string          `yaml:"entrypoint"`
	Cmd        []string          `yaml:"cmd"`
	Expose     []string          `yaml:"expose"`
	Domain     string            `yaml:"domain"`
	Autostart  *bool             `yaml:"autostart"`
	Mounts     []v2Mount         `yaml:"mounts"`
}

// v2Mount accepts the explicit runtime mount form:
//
//   - source: ./data
//     target: /var/lib/app
//     mode: ro
//
// `readonly: true|false` is accepted as a spelling of mode for callers that
// prefer a boolean. Supplying both fields is rejected to keep the resulting
// configuration unambiguous.
type v2Mount struct {
	Source   string
	Target   string
	Mode     string
	Readonly *bool
}

func (m *v2Mount) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("runtime.mounts 项必须是 source/target 映射")
	}
	allowed := map[string]bool{"source": true, "target": true, "mode": true, "readonly": true}
	seen := make(map[string]bool, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if !allowed[key] {
			return fmt.Errorf("runtime.mounts 项包含未知字段 %q", key)
		}
		if seen[key] {
			return fmt.Errorf("runtime.mounts 项字段 %q 重复", key)
		}
		seen[key] = true
	}
	var raw struct {
		Source   string `yaml:"source"`
		Target   string `yaml:"target"`
		Mode     string `yaml:"mode"`
		Readonly *bool  `yaml:"readonly"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	if seen["mode"] && seen["readonly"] {
		return fmt.Errorf("runtime.mounts 项不能同时设置 mode 和 readonly")
	}
	m.Source, m.Target, m.Mode, m.Readonly = raw.Source, raw.Target, raw.Mode, raw.Readonly
	return nil
}

func parseV2BuildFile(filePath string, overrides map[string]string) (*Incusfile, error) {
	if filePath == "" {
		filePath = "Incusfile"
	}
	data, err := readBuildFile(filePath)
	if err != nil {
		return nil, err
	}
	if err := validateYAMLDocument(data, filePath); err != nil {
		return nil, err
	}
	var cfg v2BuildFile
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("解析 %s schema 失败: %w", filePath, err)
	}
	if cfg.Version != 2 {
		return nil, fmt.Errorf("%s: version 必须是 2", filePath)
	}
	if len(cfg.Stages) == 0 {
		return nil, fmt.Errorf("%s: stages 至少需要一个阶段", filePath)
	}

	buildValues, args, err := resolveV2Args(cfg.Args, overrides, filePath)
	if err != nil {
		return nil, err
	}
	f := &Incusfile{Path: filePath, Args: args, Name: cfg.Name}
	if err := validateGlobalYAMLConfig(f, cfg.Mirror, cfg.Network, filePath, buildValues); err != nil {
		return nil, err
	}

	seenNames := make(map[string]int)
	for index, source := range cfg.Stages {
		baseRef := source.From
		if strings.TrimSpace(baseRef) == "" {
			baseRef = cfg.Base
		}
		if strings.TrimSpace(baseRef) == "" {
			return nil, fmt.Errorf("%s: stages[%d].from 不能为空 (或声明顶层 base)", filePath, index)
		}
		from, err := expandBuildArgReferences(baseRef, buildValues, true)
		if err != nil {
			return nil, fmt.Errorf("%s: stages[%d].from: %w", filePath, index, err)
		}
		image, fingerprint, err := splitPinnedImageRef(from)
		if err != nil {
			return nil, fmt.Errorf("%s: stages[%d].from: %w", filePath, index, err)
		}
		stage := Stage{Name: source.Name, From: normalizeImageRef(image), BaseFingerprint: fingerprint}
		if stage.Name != "" {
			if err := validateStageName(stage.Name); err != nil {
				return nil, fmt.Errorf("%s: stages[%d].name: %w", filePath, index, err)
			}
			key := strings.ToLower(stage.Name)
			if previous, ok := seenNames[key]; ok {
				return nil, fmt.Errorf("%s: stages[%d].name %q 重复 (首次位于 stages[%d])", filePath, index, stage.Name, previous)
			}
			seenNames[key] = index
		}
		if index == len(cfg.Stages)-1 && len(source.Tools) > 0 {
			return nil, fmt.Errorf("%s: stages[%d].tools 只能用于非最终构建阶段", filePath, index)
		}
		if err := appendV2Stage(&stage, source, buildValues, filePath, index); err != nil {
			return nil, err
		}
		if source.Runtime != nil {
			if index != len(cfg.Stages)-1 {
				return nil, fmt.Errorf("%s: stages[%d].runtime 只允许出现在最终阶段", filePath, index)
			}
			if err := applyV2Runtime(&stage, source.Runtime, buildValues, filePath, index); err != nil {
				return nil, err
			}
		}
		f.Stages = append(f.Stages, stage)
	}
	if err := validateYAMLStageReferences(f.Stages, filePath); err != nil {
		return nil, err
	}
	last := &f.Stages[len(f.Stages)-1]
	f.From, f.Steps, f.Exposes, f.Domain, f.Autostart = last.From, last.Steps, last.Exposes, last.Domain, last.Autostart
	f.Entrypoint, f.Cmd = append([]string(nil), last.Entrypoint...), append([]string(nil), last.Cmd...)
	f.Mounts = append([]RuntimeMount(nil), last.Mounts...)
	if err := validateRuntimeMounts(f.Mounts); err != nil {
		return nil, fmt.Errorf("%s: stages[%d].runtime: %w", filePath, len(f.Stages)-1, err)
	}
	for _, step := range last.Steps {
		if step.Kind == "ENV" {
			f.Env = append(f.Env, step.Env)
		}
	}
	if err := validateFinalStageRuntimeDirectives(f.Stages); err != nil {
		return nil, err
	}
	return f, nil
}

func readBuildFile(filePath string) ([]byte, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", filePath, err)
	}
	defer f.Close()
	if info, statErr := f.Stat(); statErr == nil && info.Mode().IsRegular() && info.Size() > maxIncusfileBytes {
		return nil, fmt.Errorf("读取 %s 失败: 文件大小 %d 字节超过上限 %d 字节", filePath, info.Size(), maxIncusfileBytes)
	}
	return readBuildFileStream(f, filePath)
}

func readBuildFileStream(r io.Reader, filePath string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxIncusfileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", filePath, err)
	}
	if len(data) > maxIncusfileBytes {
		return nil, fmt.Errorf("读取 %s 失败: 内容超过上限 %d 字节", filePath, maxIncusfileBytes)
	}
	return data, nil
}

func validateYAMLDocument(data []byte, filePath string) error {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil {
		return fmt.Errorf("解析 %s YAML 失败: %w", filePath, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil && extra.Kind != 0 {
		return fmt.Errorf("%s 必须只包含一个 YAML 文档", filePath)
	} else if err != nil && err != io.EOF {
		return fmt.Errorf("解析 %s YAML 文档失败: %w", filePath, err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 {
		return fmt.Errorf("%s 必须包含一个 YAML 文档", filePath)
	}
	return validateYAMLNode(root.Content[0], filePath)
}

func resolveV2Args(declared, overrides map[string]string, filePath string) (map[string]string, []ArgSpec, error) {
	values := make(map[string]string, len(declared))
	keys := make([]string, 0, len(declared))
	for key := range declared {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pending := make(map[string]string, len(declared))
	for key, value := range declared {
		if override, ok := overrides[key]; ok {
			value = override
		}
		pending[key] = value
	}
	for len(pending) > 0 {
		progress := false
		for _, key := range keys {
			value, ok := pending[key]
			if !ok {
				continue
			}
			expanded, expandErr := expandBuildArgReferences(value, values, true)
			if expandErr != nil && strings.Contains(expandErr.Error(), "未声明") {
				continue
			}
			if expandErr != nil {
				return nil, nil, fmt.Errorf("%s: args.%s: %w", filePath, key, expandErr)
			}
			values[key] = expanded
			delete(pending, key)
			progress = true
		}
		if !progress {
			for _, key := range keys {
				if value, ok := pending[key]; ok {
					_, expandErr := expandBuildArgReferences(value, values, true)
					return nil, nil, fmt.Errorf("%s: args.%s: %w", filePath, key, expandErr)
				}
			}
		}
	}
	for key := range overrides {
		if _, ok := declared[key]; !ok {
			return nil, nil, fmt.Errorf("--build-arg %s 未在 YAML args 中声明", key)
		}
	}
	args := make([]ArgSpec, 0, len(keys))
	for _, key := range keys {
		if err := validateArgKey(key); err != nil {
			return nil, nil, fmt.Errorf("%s: args.%s: %w", filePath, key, err)
		}
		args = append(args, ArgSpec{Key: key, Value: values[key]})
	}
	return values, args, nil
}

func appendV2Stage(stage *Stage, source v2Stage, values map[string]string, filePath string, stageIndex int) error {
	prefix := fmt.Sprintf("%s: stages[%d]", filePath, stageIndex)
	if source.Workdir != "" {
		workdir, err := expandBuildArgReferences(source.Workdir, values, true)
		if err != nil {
			return fmt.Errorf("%s.workdir: %w", prefix, err)
		}
		stage.Steps = append(stage.Steps, BuildStep{Kind: "WORKDIR", Workdir: workdir})
	}
	if err := appendV2Env(stage, source.Env, values, prefix); err != nil {
		return err
	}
	packages, err := validateYAMLPackages(source.Packages, values)
	if err != nil && len(source.Packages) > 0 {
		return fmt.Errorf("%s.packages: %w", prefix, err)
	}
	if len(packages) > 0 {
		stage.Steps = append(stage.Steps, BuildStep{Kind: "PKG", Packages: packages})
	}
	keys := make([]string, 0, len(source.Tools))
	for tool := range source.Tools {
		keys = append(keys, tool)
	}
	sort.Strings(keys)
	for _, tool := range keys {
		version, err := expandBuildArgReferences(source.Tools[tool], values, true)
		if err != nil {
			return fmt.Errorf("%s.tools.%s: %w", prefix, tool, err)
		}
		spec, err := normalizeMiseSpec(MiseSpec{Tool: tool, Version: version})
		if err != nil {
			return fmt.Errorf("%s.tools.%s: %w", prefix, tool, err)
		}
		stage.Steps = append(stage.Steps, BuildStep{Kind: "MISE", Mise: spec})
	}
	if err := appendV2Files(stage, source.Files, values, prefix); err != nil {
		return err
	}
	if err := appendV2Artifacts(stage, source.Artifacts, values, prefix); err != nil {
		return err
	}
	for index, fetch := range source.Fetch {
		spec, err := normalizeV2Fetch(fetch, values, prefix, index)
		if err != nil {
			return err
		}
		stage.Steps = append(stage.Steps, BuildStep{Kind: "DOWNLOAD", Download: spec})
	}
	for index, command := range source.Commands {
		if err := appendV2Command(stage, command, values, prefix, index); err != nil {
			return err
		}
	}
	return nil
}

func appendV2Env(stage *Stage, env map[string]string, values map[string]string, prefix string) error {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := validateEnvKey(key); err != nil {
			return fmt.Errorf("%s.env.%s: %w", prefix, key, err)
		}
		value, err := expandBuildArgReferences(env[key], values, false)
		if err != nil {
			return fmt.Errorf("%s.env.%s: %w", prefix, key, err)
		}
		stage.Steps = append(stage.Steps, BuildStep{Kind: "ENV", Env: EnvSpec{Key: key, Value: value}})
	}
	return nil
}

func appendV2Files(stage *Stage, files map[string][]string, values map[string]string, prefix string) error {
	destinations := make([]string, 0, len(files))
	for destination := range files {
		destinations = append(destinations, destination)
	}
	sort.Strings(destinations)
	for _, destination := range destinations {
		sources := files[destination]
		if len(sources) == 0 || strings.TrimSpace(destination) == "" {
			return fmt.Errorf("%s.files[%q] 需要 destination 和至少一个 source", prefix, destination)
		}
		expandedSources := make([]string, len(sources))
		for index, source := range sources {
			value, err := expandBuildArgReferences(source, values, true)
			if err != nil {
				return fmt.Errorf("%s.files[%s][%d]: %w", prefix, destination, index, err)
			}
			expandedSources[index] = value
		}
		expandedDestination, err := expandBuildArgReferences(destination, values, true)
		if err != nil {
			return fmt.Errorf("%s.files.%s: %w", prefix, destination, err)
		}
		if len(expandedSources) > 1 && !strings.HasSuffix(expandedDestination, "/") && expandedDestination != "." {
			return fmt.Errorf("%s.files.%s 多个 source 时目标必须是目录", prefix, destination)
		}
		stage.Steps = append(stage.Steps, BuildStep{Kind: "COPY", Copy: CopySpec{Sources: expandedSources, Src: expandedSources[0], Dst: expandedDestination}})
	}
	return nil
}

func appendV2Artifacts(stage *Stage, artifacts map[string]map[string]string, values map[string]string, prefix string) error {
	fromNames := make([]string, 0, len(artifacts))
	for from := range artifacts {
		fromNames = append(fromNames, from)
	}
	sort.Strings(fromNames)
	for _, from := range fromNames {
		sources := artifacts[from]
		paths := make([]string, 0, len(sources))
		for source := range sources {
			paths = append(paths, source)
		}
		sort.Strings(paths)
		for _, source := range paths {
			expandedFrom, err := expandBuildArgReferences(from, values, true)
			if err != nil {
				return fmt.Errorf("%s.artifacts.%s: %w", prefix, from, err)
			}
			expandedSource, err := expandBuildArgReferences(source, values, true)
			if err != nil {
				return fmt.Errorf("%s.artifacts.%s.%s: %w", prefix, from, source, err)
			}
			expandedDestination, err := expandBuildArgReferences(sources[source], values, true)
			if err != nil {
				return fmt.Errorf("%s.artifacts.%s.%s: %w", prefix, from, source, err)
			}
			stage.Steps = append(stage.Steps, BuildStep{Kind: "COPY", Copy: CopySpec{Sources: []string{expandedSource}, Src: expandedSource, Dst: expandedDestination, From: expandedFrom}})
		}
	}
	return nil
}

func normalizeV2Fetch(source v2Fetch, values map[string]string, prefix string, index int) (DownloadSpec, error) {
	url, err := expandBuildArgReferences(source.URL, values, true)
	if err != nil || strings.TrimSpace(url) == "" {
		if err == nil {
			err = fmt.Errorf("url 不能为空")
		}
		return DownloadSpec{}, fmt.Errorf("%s.fetch[%d].url: %w", prefix, index, err)
	}
	extract, err := expandBuildArgReferences(source.Extract, values, true)
	if err != nil || strings.TrimSpace(extract) == "" {
		if err == nil {
			err = fmt.Errorf("extract 不能为空")
		}
		return DownloadSpec{}, fmt.Errorf("%s.fetch[%d].extract: %w", prefix, index, err)
	}
	format := strings.ToLower(strings.TrimSpace(source.Format))
	if format != "tar.gz" && format != "tgz" && format != "zip" {
		return DownloadSpec{}, fmt.Errorf("%s.fetch[%d].format 无效", prefix, index)
	}
	output := source.Output
	if output == "" {
		output = fmt.Sprintf("/tmp/bocker-fetch-%d.archive", index)
	}
	output, err = expandBuildArgReferences(output, values, true)
	if err != nil {
		return DownloadSpec{}, fmt.Errorf("%s.fetch[%d].output: %w", prefix, index, err)
	}
	sha, err := expandBuildArgReferences(source.SHA256, values, true)
	if err != nil {
		return DownloadSpec{}, fmt.Errorf("%s.fetch[%d].sha256: %w", prefix, index, err)
	}
	if sha != "" && !validSHA256(sha) {
		return DownloadSpec{}, fmt.Errorf("%s.fetch[%d].sha256 必须是 64 位十六进制值", prefix, index)
	}
	timeout, tries := source.Timeout, source.Tries
	if timeout == 0 {
		timeout = 30
	}
	if tries == 0 {
		tries = 1
	}
	if timeout < 1 || tries < 1 {
		return DownloadSpec{}, fmt.Errorf("%s.fetch[%d] timeout 和 tries 必须为正数", prefix, index)
	}
	var move *MoveSpec
	if source.Move != nil {
		from, e := expandBuildArgReferences(source.Move.From, values, true)
		if e != nil {
			return DownloadSpec{}, fmt.Errorf("%s.fetch[%d].move.from: %w", prefix, index, e)
		}
		to, e := expandBuildArgReferences(source.Move.To, values, true)
		if e != nil {
			return DownloadSpec{}, fmt.Errorf("%s.fetch[%d].move.to: %w", prefix, index, e)
		}
		move = &MoveSpec{From: from, To: to}
	}
	return DownloadSpec{Output: output, Extract: extract, Attempts: []DownloadAttempt{{URL: url, SHA256: sha, Format: format, Timeout: timeout, Tries: tries, Move: move}}}, nil
}

func appendV2Command(stage *Stage, source v2Command, values map[string]string, prefix string, index int) error {
	if len(source.Run) > 0 {
		args := make([]string, len(source.Run))
		for i, arg := range source.Run {
			expanded, err := expandBuildArgReferences(arg, values, true)
			if err != nil {
				return fmt.Errorf("%s.commands[%d][%d]: %w", prefix, index, i, err)
			}
			args[i] = expanded
		}
		if source.Capture != "" {
			if err := validateEnvKey(source.Capture); err != nil {
				return fmt.Errorf("%s.commands[%d].capture: %w", prefix, index, err)
			}
		}
		stage.Steps = append(stage.Steps, BuildStep{Kind: "EXEC", ExecCommand: args[0], ExecArgs: args[1:], ExecCapture: source.Capture})
		return nil
	}
	shell, err := expandBuildArgReferences(source.Shell, values, true)
	if err != nil {
		return fmt.Errorf("%s.commands[%d].shell: %w", prefix, index, err)
	}
	stage.Steps = append(stage.Steps, BuildStep{Kind: "SHELL", Run: shell})
	return nil
}

func applyV2Runtime(stage *Stage, runtime *v2Runtime, values map[string]string, filePath string, index int) error {
	prefix := fmt.Sprintf("%s: stages[%d].runtime", filePath, index)
	if runtime.Autostart != nil {
		value := *runtime.Autostart
		stage.Autostart = &value
	}
	var err error
	stage.Domain, err = expandBuildArgReferences(runtime.Domain, values, false)
	if err != nil {
		return fmt.Errorf("%s.domain: %w", prefix, err)
	}
	if stage.Domain != "" {
		if err := validateDomainName(stage.Domain); err != nil {
			return fmt.Errorf("%s.domain: %w", prefix, err)
		}
	}
	for _, item := range runtime.Expose {
		port, protocol, err := parseV2Expose(item)
		if err != nil {
			return fmt.Errorf("%s.expose: %w", prefix, err)
		}
		stage.Exposes = append(stage.Exposes, PortSpec{Port: port, Protocol: protocol})
	}
	stage.Entrypoint, err = expandCommand(runtime.Entrypoint, values)
	if err != nil {
		return fmt.Errorf("%s.entrypoint: %w", prefix, err)
	}
	stage.Cmd, err = expandCommand(runtime.Cmd, values)
	if err != nil {
		return fmt.Errorf("%s.cmd: %w", prefix, err)
	}
	if err := appendV2Env(stage, runtime.Env, values, prefix); err != nil {
		return err
	}
	if err := appendV2RuntimeMounts(stage, runtime.Mounts, values, filePath, prefix); err != nil {
		return err
	}
	return nil
}

func appendV2RuntimeMounts(stage *Stage, mounts []v2Mount, values map[string]string, filePath, prefix string) error {
	if len(mounts) == 0 {
		return nil
	}
	seenTargets := make(map[string]int, len(mounts))
	for index, source := range mounts {
		mount, err := normalizeV2RuntimeMount(source, values, filePath, prefix, index)
		if err != nil {
			return err
		}
		if previous, ok := seenTargets[mount.Target]; ok {
			return fmt.Errorf("%s.mounts[%d].target %q 与 mounts[%d] 重复", prefix, index, mount.Target, previous)
		}
		seenTargets[mount.Target] = index
		stage.Mounts = append(stage.Mounts, mount)
	}
	return nil
}

func normalizeV2RuntimeMount(source v2Mount, values map[string]string, filePath, prefix string, index int) (RuntimeMount, error) {
	label := fmt.Sprintf("%s.mounts[%d]", prefix, index)
	hostSource, err := expandBuildArgReferences(strings.TrimSpace(source.Source), values, true)
	if err != nil {
		return RuntimeMount{}, fmt.Errorf("%s.source: %w", label, err)
	}
	if hostSource == "" {
		return RuntimeMount{}, fmt.Errorf("%s.source 不能为空", label)
	}
	// Relative sources are resolved against the Incusfile directory so builds
	// remain independent of the caller's current working directory.
	if !filepath.IsAbs(hostSource) {
		hostSource = filepath.Join(filepath.Dir(filePath), hostSource)
	}
	hostSource, err = filepath.Abs(filepath.Clean(hostSource))
	if err != nil {
		return RuntimeMount{}, fmt.Errorf("%s.source 路径解析失败: %w", label, err)
	}

	target, err := expandBuildArgReferences(strings.TrimSpace(source.Target), values, true)
	if err != nil {
		return RuntimeMount{}, fmt.Errorf("%s.target: %w", label, err)
	}
	if target == "" || !path.IsAbs(target) {
		return RuntimeMount{}, fmt.Errorf("%s.target 必须是绝对容器路径", label)
	}
	target = path.Clean(target)
	if target == "/" {
		return RuntimeMount{}, fmt.Errorf("%s.target 不能是容器根路径 / (会与 root 设备冲突)", label)
	}

	mode, err := expandBuildArgReferences(strings.TrimSpace(source.Mode), values, true)
	if err != nil {
		return RuntimeMount{}, fmt.Errorf("%s.mode: %w", label, err)
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if source.Readonly != nil {
		if *source.Readonly {
			mode = "ro"
		} else {
			mode = "rw"
		}
	}
	if mode == "" {
		mode = "rw"
	}
	if mode != "ro" && mode != "rw" {
		return RuntimeMount{}, fmt.Errorf("%s.mode 必须是 ro 或 rw", label)
	}
	return RuntimeMount{Source: hostSource, Target: target, Mode: mode}, nil
}

func parseV2Expose(value string) (int, string, error) {
	parts := strings.Split(value, "/")
	if len(parts) > 2 || len(parts) == 0 {
		return 0, "", fmt.Errorf("端口 %q 必须是 PORT 或 PORT/PROTOCOL", value)
	}
	port, err := strconv.Atoi(parts[0])
	if err != nil || port < 1 || port > 65535 {
		return 0, "", fmt.Errorf("端口 %q 无效", value)
	}
	protocol := "tcp"
	if len(parts) == 2 {
		protocol = strings.ToLower(parts[1])
	}
	if protocol != "tcp" && protocol != "udp" {
		return 0, "", fmt.Errorf("端口协议 %q 无效", protocol)
	}
	return port, protocol, nil
}
