package bocker

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// YAML build files are intentionally strict. The build engine consumes the
// normalized Incusfile AST below, while this decoder is the only accepted
// source format.
type yamlBuildFile struct {
	Version int               `yaml:"version"`
	Args    map[string]string `yaml:"args"`
	Mirror  string            `yaml:"mirror"`
	Name    string            `yaml:"name"`
	Network string            `yaml:"network"`
	Stages  []yamlStage       `yaml:"stages"`
}

type yamlStage struct {
	Name    string       `yaml:"name"`
	From    string       `yaml:"from"`
	Steps   []yamlStep   `yaml:"steps"`
	Runtime *yamlRuntime `yaml:"runtime"`
}

type yamlStep struct {
	Exec     *yamlExec         `yaml:"exec"`
	Shell    *string           `yaml:"shell"`
	Pkg      *[]string         `yaml:"pkg"`
	Copy     *yamlCopy         `yaml:"copy"`
	Env      map[string]string `yaml:"env"`
	Workdir  *string           `yaml:"workdir"`
	Mise     *yamlMise         `yaml:"mise"`
	Download *yamlDownload     `yaml:"download"`
	Write    *yamlWrite        `yaml:"write"`
	Service  *yamlService      `yaml:"service"`
}

type yamlExec struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Capture string   `yaml:"capture"`
}

type yamlCopy struct {
	From        string   `yaml:"from"`
	Sources     []string `yaml:"sources"`
	Destination string   `yaml:"destination"`
}

type yamlMise struct {
	Tool    string `yaml:"tool"`
	Version string `yaml:"version"`
}

type yamlDownload struct {
	Output   string            `yaml:"output"`
	Extract  string            `yaml:"extract"`
	Verify   *yamlFileVerify   `yaml:"verify"`
	Attempts []yamlDownloadTry `yaml:"attempts"`
}

type yamlDownloadTry struct {
	URL     string    `yaml:"url"`
	SHA256  string    `yaml:"sha256"`
	Format  string    `yaml:"format"`
	Timeout int       `yaml:"timeout"`
	Tries   int       `yaml:"tries"`
	Move    *yamlMove `yaml:"move"`
}

type yamlMove struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

type yamlFileVerify struct {
	Path    string `yaml:"path"`
	Pattern string `yaml:"pattern"`
	Value   string `yaml:"value"`
}

type yamlWrite struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
	Mode    string `yaml:"mode"`
}

type yamlService struct {
	Start  []string `yaml:"start"`
	Stop   []string `yaml:"stop"`
	Enable []string `yaml:"enable"`
}

type yamlRuntime struct {
	Env        map[string]string `yaml:"env"`
	Entrypoint []string          `yaml:"entrypoint"`
	Cmd        []string          `yaml:"cmd"`
	Expose     []yamlExpose      `yaml:"expose"`
	Domain     string            `yaml:"domain"`
	Autostart  *bool             `yaml:"autostart"`
}

type yamlExpose struct {
	Port     int    `yaml:"port"`
	Protocol string `yaml:"protocol"`
}

func parseYAMLBuildFile(path string, overrides map[string]string) (*Incusfile, error) {
	if path == "" {
		path = "Incusfile"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	var root yaml.Node
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("解析 %s YAML 失败: %w", path, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil && extra.Kind != 0 {
		return nil, fmt.Errorf("%s 必须只包含一个 YAML 文档", path)
	} else if err != nil && err != io.EOF {
		return nil, fmt.Errorf("解析 %s YAML 文档失败: %w", path, err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 {
		return nil, fmt.Errorf("%s 必须包含一个 YAML 文档", path)
	}
	if err := validateYAMLNode(root.Content[0], path); err != nil {
		return nil, err
	}
	var cfg yamlBuildFile
	strict := yaml.NewDecoder(strings.NewReader(string(data)))
	strict.KnownFields(true)
	if err := strict.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("解析 %s schema 失败: %w", path, err)
	}
	if cfg.Version != 1 {
		return nil, fmt.Errorf("%s: version 必须是 1", path)
	}
	if len(cfg.Stages) == 0 {
		return nil, fmt.Errorf("%s: stages 至少需要一个阶段", path)
	}

	buildValues := make(map[string]string, len(cfg.Args))
	args := make([]ArgSpec, 0, len(cfg.Args))
	argKeys := make([]string, 0, len(cfg.Args))
	for key := range cfg.Args {
		argKeys = append(argKeys, key)
	}
	sort.Strings(argKeys)
	pending := make(map[string]string, len(cfg.Args))
	for key, value := range cfg.Args {
		if override, ok := overrides[key]; ok {
			value = override
		}
		pending[key] = value
	}
	for len(pending) > 0 {
		progress := false
		for _, key := range argKeys {
			value, ok := pending[key]
			if !ok {
				continue
			}
			expanded, expandErr := expandBuildArgReferences(value, buildValues, true)
			if expandErr != nil && strings.Contains(expandErr.Error(), "未声明") {
				continue
			}
			if expandErr != nil {
				return nil, fmt.Errorf("%s: args.%s: %w", path, key, expandErr)
			}
			buildValues[key] = expanded
			args = append(args, ArgSpec{Key: key, Value: expanded})
			delete(pending, key)
			progress = true
		}
		if !progress {
			for _, key := range argKeys {
				if value, ok := pending[key]; ok {
					_, expandErr := expandBuildArgReferences(value, buildValues, true)
					return nil, fmt.Errorf("%s: args.%s: %w", path, key, expandErr)
				}
			}
		}
	}
	for _, key := range argKeys {
		if err := validateArgKey(key); err != nil {
			return nil, fmt.Errorf("%s: args.%s: %w", path, key, err)
		}
	}
	for key := range overrides {
		if _, ok := cfg.Args[key]; !ok {
			return nil, fmt.Errorf("--build-arg %s 未在 YAML args 中声明", key)
		}
	}

	f := &Incusfile{Path: path, Args: args, Name: cfg.Name}
	if err := validateGlobalYAMLConfig(f, cfg.Mirror, cfg.Network, path, buildValues); err != nil {
		return nil, err
	}
	seenNames := make(map[string]int)
	for index, sourceStage := range cfg.Stages {
		if strings.TrimSpace(sourceStage.From) == "" {
			return nil, fmt.Errorf("%s: stages[%d].from 不能为空", path, index)
		}
		from, err := expandBuildArgReferences(sourceStage.From, buildValues, true)
		if err != nil {
			return nil, fmt.Errorf("%s: stages[%d].from: %w", path, index, err)
		}
		image, fingerprint, err := splitPinnedImageRef(from)
		if err != nil {
			return nil, fmt.Errorf("%s: stages[%d].from: %w", path, index, err)
		}
		stage := Stage{Name: sourceStage.Name, From: normalizeImageRef(image), BaseFingerprint: fingerprint}
		if stage.Name != "" {
			if err := validateStageName(stage.Name); err != nil {
				return nil, fmt.Errorf("%s: stages[%d].name: %w", path, index, err)
			}
			key := strings.ToLower(stage.Name)
			if previous, ok := seenNames[key]; ok {
				return nil, fmt.Errorf("%s: stages[%d].name %q 重复 (首次位于 stages[%d])", path, index, stage.Name, previous)
			}
			seenNames[key] = index
		}
		isFinal := index == len(cfg.Stages)-1
		if err := appendYAMLSteps(&stage, sourceStage.Steps, buildValues, path, index, isFinal); err != nil {
			return nil, err
		}
		if sourceStage.Runtime != nil {
			if index != len(cfg.Stages)-1 {
				return nil, fmt.Errorf("%s: stages[%d].runtime 只允许出现在最终阶段", path, index)
			}
			if err := applyYAMLRuntime(&stage, sourceStage.Runtime, buildValues, path, index); err != nil {
				return nil, err
			}
		}
		f.Stages = append(f.Stages, stage)
	}
	if err := validateYAMLStageReferences(f.Stages, path); err != nil {
		return nil, err
	}
	last := &f.Stages[len(f.Stages)-1]
	f.From, f.Steps, f.Exposes, f.Domain, f.Autostart = last.From, last.Steps, last.Exposes, last.Domain, last.Autostart
	f.Entrypoint, f.Cmd = append([]string(nil), last.Entrypoint...), append([]string(nil), last.Cmd...)
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

func validateYAMLNode(node *yaml.Node, path string) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("%s:%d: YAML anchors/aliases 不被支持", path, node.Line)
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || seen[key.Value] {
				return fmt.Errorf("%s:%d: YAML mapping key 重复或无效", path, key.Line)
			}
			seen[key.Value] = true
			if err := validateYAMLNode(node.Content[i+1], path); err != nil {
				return err
			}
		}
	} else if node.Kind == yaml.SequenceNode {
		for _, child := range node.Content {
			if err := validateYAMLNode(child, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateGlobalYAMLConfig(f *Incusfile, mirror, network, path string, values map[string]string) error {
	var err error
	if mirror != "" {
		mirror, err = expandBuildArgReferences(mirror, values, true)
		if err != nil {
			return fmt.Errorf("%s: mirror: %w", path, err)
		}
		f.Mirror, err = normalizePackageMirror(mirror)
		if err != nil {
			return fmt.Errorf("%s: mirror: %w", path, err)
		}
	}
	if network != "" {
		network, err = expandBuildArgReferences(network, values, true)
		if err != nil {
			return fmt.Errorf("%s: network: %w", path, err)
		}
		mode, modeErr := ParseNetworkMode(network)
		if modeErr != nil {
			return fmt.Errorf("%s: network: %w", path, modeErr)
		}
		f.Network = string(mode)
	}
	if f.Name != "" {
		f.Name, err = expandBuildArgReferences(f.Name, values, true)
		if err != nil {
			return fmt.Errorf("%s: name: %w", path, err)
		}
		if err := validateBockerName(f.Name); err != nil {
			return fmt.Errorf("%s: name: %w", path, err)
		}
	}
	return nil
}

func appendYAMLSteps(stage *Stage, steps []yamlStep, values map[string]string, path string, stageIndex int, isFinal bool) error {
	stepValues := make(map[string]string, len(values))
	for key, value := range values {
		stepValues[key] = value
	}
	for stepIndex, source := range steps {
		prefix := fmt.Sprintf("%s: stages[%d].steps[%d]", path, stageIndex, stepIndex)
		count := 0
		if source.Exec != nil {
			count++
		}
		if source.Shell != nil {
			count++
		}
		if source.Pkg != nil {
			count++
		}
		if source.Copy != nil {
			count++
		}
		if source.Env != nil {
			count++
		}
		if source.Workdir != nil {
			count++
		}
		if source.Mise != nil {
			if isFinal {
				return fmt.Errorf("%s.mise 只允许出现在非最终构建阶段", prefix)
			}
			count++
		}
		if source.Download != nil {
			count++
		}
		if source.Write != nil {
			count++
		}
		if source.Service != nil {
			count++
		}
		if count != 1 {
			return fmt.Errorf("%s 必须且只能包含一个步骤类型", prefix)
		}
		if source.Exec != nil {
			command, err := expandBuildArgReferences(source.Exec.Command, values, true)
			if err != nil {
				return fmt.Errorf("%s.exec.command: %w", prefix, err)
			}
			if strings.TrimSpace(command) == "" {
				return fmt.Errorf("%s.exec.command 不能为空", prefix)
			}
			args := make([]string, len(source.Exec.Args))
			for i, arg := range source.Exec.Args {
				args[i], err = expandBuildArgReferences(arg, stepValues, true)
				if err != nil {
					return fmt.Errorf("%s.exec.args[%d]: %w", prefix, i, err)
				}
			}
			if source.Exec.Capture != "" {
				if err := validateEnvKey(source.Exec.Capture); err != nil {
					return fmt.Errorf("%s.exec.capture: %w", prefix, err)
				}
				if _, exists := stepValues[source.Exec.Capture]; exists {
					return fmt.Errorf("%s.exec.capture 变量 %q 已存在", prefix, source.Exec.Capture)
				}
				stepValues[source.Exec.Capture] = "${" + source.Exec.Capture + "}"
			}
			stage.Steps = append(stage.Steps, BuildStep{Kind: "EXEC", ExecCommand: command, ExecArgs: args, ExecCapture: source.Exec.Capture})
			continue
		}
		if source.Shell != nil {
			if strings.TrimSpace(*source.Shell) == "" {
				return fmt.Errorf("%s.shell 不能为空", prefix)
			}
			stage.Steps = append(stage.Steps, BuildStep{Kind: "SHELL", Run: *source.Shell})
			continue
		}
		if source.Pkg != nil {
			packages, err := validateYAMLPackages(*source.Pkg, values)
			if err != nil {
				return fmt.Errorf("%s.pkg: %w", prefix, err)
			}
			stage.Steps = append(stage.Steps, BuildStep{Kind: "PKG", Packages: packages})
			continue
		}
		if source.Workdir != nil {
			value, err := expandBuildArgReferences(*source.Workdir, values, true)
			if err != nil {
				return fmt.Errorf("%s.workdir: %w", prefix, err)
			}
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s.workdir 不能为空", prefix)
			}
			stage.Steps = append(stage.Steps, BuildStep{Kind: "WORKDIR", Workdir: value})
			continue
		}
		if source.Env != nil {
			keys := make([]string, 0, len(source.Env))
			for key := range source.Env {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if len(keys) == 0 {
				return fmt.Errorf("%s.env 不能为空", prefix)
			}
			for _, key := range keys {
				if err := validateEnvKey(key); err != nil {
					return fmt.Errorf("%s.env.%s: %w", prefix, key, err)
				}
				value, err := expandBuildArgReferences(source.Env[key], values, false)
				if err != nil {
					return fmt.Errorf("%s.env.%s: %w", prefix, key, err)
				}
				stage.Steps = append(stage.Steps, BuildStep{Kind: "ENV", Env: EnvSpec{Key: key, Value: value}})
				stepValues[key] = value
			}
			continue
		}
		if source.Copy != nil {
			cp := source.Copy
			if len(cp.Sources) == 0 || strings.TrimSpace(cp.Destination) == "" {
				return fmt.Errorf("%s.copy 需要 sources 和 destination", prefix)
			}
			sources := make([]string, len(cp.Sources))
			for i, item := range cp.Sources {
				expanded, err := expandBuildArgReferences(item, values, true)
				if err != nil {
					return fmt.Errorf("%s.copy.sources[%d]: %w", prefix, i, err)
				}
				sources[i] = expanded
				if strings.TrimSpace(sources[i]) == "" {
					return fmt.Errorf("%s.copy.sources[%d] 不能为空", prefix, i)
				}
			}
			destination, err := expandBuildArgReferences(cp.Destination, values, true)
			if err != nil {
				return fmt.Errorf("%s.copy.destination: %w", prefix, err)
			}
			if strings.TrimSpace(destination) == "" {
				return fmt.Errorf("%s.copy.destination 不能为空", prefix)
			}
			if len(sources) > 1 && !strings.HasSuffix(destination, "/") && destination != "." {
				return fmt.Errorf("%s.copy 多个 sources 时 destination 必须是目录", prefix)
			}
			from, err := expandBuildArgReferences(cp.From, values, true)
			if err != nil {
				return fmt.Errorf("%s.copy.from: %w", prefix, err)
			}
			stage.Steps = append(stage.Steps, BuildStep{Kind: "COPY", Copy: CopySpec{Sources: sources, Src: sources[0], Dst: destination, From: from}})
			continue
		}
		if source.Mise != nil {
			spec := MiseSpec{Tool: source.Mise.Tool, Version: source.Mise.Version}
			var err error
			spec.Tool, err = expandBuildArgReferences(spec.Tool, values, true)
			if err != nil {
				return fmt.Errorf("%s.mise.tool: %w", prefix, err)
			}
			spec.Version, err = expandBuildArgReferences(spec.Version, values, true)
			if err != nil {
				return fmt.Errorf("%s.mise.version: %w", prefix, err)
			}
			spec, err = normalizeMiseSpec(spec)
			if err != nil {
				return fmt.Errorf("%s.mise: %w", prefix, err)
			}
			stage.Steps = append(stage.Steps, BuildStep{Kind: "MISE", Mise: spec})
			continue
		}
		if source.Download != nil {
			spec, err := normalizeYAMLDownload(*source.Download, values, prefix)
			if err != nil {
				return err
			}
			stage.Steps = append(stage.Steps, BuildStep{Kind: "DOWNLOAD", Download: spec})
			continue
		}
		if source.Write != nil {
			spec, err := normalizeYAMLWrite(*source.Write, stepValues, prefix)
			if err != nil {
				return err
			}
			stage.Steps = append(stage.Steps, BuildStep{Kind: "WRITE", Write: spec})
			continue
		}
		if source.Service != nil {
			spec, err := normalizeYAMLService(*source.Service, values, prefix)
			if err != nil {
				return err
			}
			stage.Steps = append(stage.Steps, BuildStep{Kind: "SERVICE", Service: spec})
		}
	}
	return nil
}

func validateYAMLPackages(packages []string, values map[string]string) ([]string, error) {
	if len(packages) == 0 {
		return nil, fmt.Errorf("至少需要一个软件包")
	}
	result := make([]string, len(packages))
	for i, pkg := range packages {
		var err error
		pkg, err = expandBuildArgReferences(pkg, values, true)
		if err != nil {
			return nil, fmt.Errorf("packages[%d]: %w", i, err)
		}
		if pkg == "" || len(pkg) > 256 || strings.HasPrefix(pkg, "-") || strings.IndexFunc(pkg, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }) >= 0 {
			return nil, fmt.Errorf("软件包 %q 无效", pkg)
		}
		result[i] = pkg
	}
	return result, nil
}

func normalizeYAMLDownload(source yamlDownload, values map[string]string, prefix string) (DownloadSpec, error) {
	var spec DownloadSpec
	var err error
	spec.Output, err = expandBuildArgReferences(source.Output, values, true)
	if err != nil || strings.TrimSpace(spec.Output) == "" {
		if err == nil {
			err = fmt.Errorf("output 不能为空")
		}
		return spec, fmt.Errorf("%s.download.output: %w", prefix, err)
	}
	spec.Extract, err = expandBuildArgReferences(source.Extract, values, true)
	if err != nil || strings.TrimSpace(spec.Extract) == "" {
		if err == nil {
			err = fmt.Errorf("extract 不能为空")
		}
		return spec, fmt.Errorf("%s.download.extract: %w", prefix, err)
	}
	if len(source.Attempts) == 0 {
		return spec, fmt.Errorf("%s.download.attempts 至少需要一个下载源", prefix)
	}
	for i, item := range source.Attempts {
		attempt := DownloadAttempt{Timeout: item.Timeout, Tries: item.Tries}
		attempt.URL, err = expandBuildArgReferences(item.URL, values, true)
		if err != nil || strings.TrimSpace(attempt.URL) == "" {
			if err == nil {
				err = fmt.Errorf("url 不能为空")
			}
			return spec, fmt.Errorf("%s.download.attempts[%d].url: %w", prefix, i, err)
		}
		attempt.SHA256, err = expandBuildArgReferences(item.SHA256, values, true)
		if err != nil || !validSHA256(attempt.SHA256) {
			if err == nil {
				err = fmt.Errorf("sha256 必须是 64 位十六进制")
			}
			return spec, fmt.Errorf("%s.download.attempts[%d].sha256: %w", prefix, i, err)
		}
		attempt.Format = strings.ToLower(strings.TrimSpace(item.Format))
		if attempt.Format != "tar.gz" && attempt.Format != "tgz" && attempt.Format != "zip" {
			return spec, fmt.Errorf("%s.download.attempts[%d].format 无效", prefix, i)
		}
		if attempt.Timeout == 0 {
			attempt.Timeout = 30
		}
		if attempt.Tries == 0 {
			attempt.Tries = 1
		}
		if attempt.Timeout < 1 || attempt.Tries < 1 {
			return spec, fmt.Errorf("%s.download.attempts[%d] timeout 和 tries 必须为正数", prefix, i)
		}
		if item.Move != nil {
			from, e := expandBuildArgReferences(item.Move.From, values, true)
			if e != nil {
				return spec, fmt.Errorf("%s.download.attempts[%d].move.from: %w", prefix, i, e)
			}
			to, e := expandBuildArgReferences(item.Move.To, values, true)
			if e != nil {
				return spec, fmt.Errorf("%s.download.attempts[%d].move.to: %w", prefix, i, e)
			}
			attempt.Move = &MoveSpec{From: from, To: to}
		}
		spec.Attempts = append(spec.Attempts, attempt)
	}
	if source.Verify != nil {
		path, e := expandBuildArgReferences(source.Verify.Path, values, true)
		if e != nil {
			return spec, fmt.Errorf("%s.download.verify.path: %w", prefix, e)
		}
		pattern, e := expandBuildArgReferences(source.Verify.Pattern, values, true)
		if e != nil {
			return spec, fmt.Errorf("%s.download.verify.pattern: %w", prefix, e)
		}
		value, e := expandBuildArgReferences(source.Verify.Value, values, true)
		if e != nil {
			return spec, fmt.Errorf("%s.download.verify.value: %w", prefix, e)
		}
		if path == "" || pattern == "" || value == "" {
			return spec, fmt.Errorf("%s.download.verify 需要 path、pattern 和 value", prefix)
		}
		spec.Verify = &FileVerifySpec{Path: path, Pattern: pattern, Value: value}
	}
	return spec, nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func normalizeYAMLWrite(source yamlWrite, values map[string]string, prefix string) (WriteSpec, error) {
	path, err := expandBuildArgReferences(source.Path, values, true)
	if err != nil || strings.TrimSpace(path) == "" {
		if err == nil {
			err = fmt.Errorf("path 不能为空")
		}
		return WriteSpec{}, fmt.Errorf("%s.write.path: %w", prefix, err)
	}
	content, err := expandBuildArgReferences(source.Content, values, true)
	if err != nil {
		return WriteSpec{}, fmt.Errorf("%s.write.content: %w", prefix, err)
	}
	mode := source.Mode
	if mode == "" {
		mode = "0644"
	}
	if len(mode) != 4 || strings.Trim(mode, "01234567") != "" {
		return WriteSpec{}, fmt.Errorf("%s.write.mode 必须是四位八进制权限", prefix)
	}
	return WriteSpec{Path: path, Content: content, Mode: mode}, nil
}

func normalizeYAMLService(source yamlService, values map[string]string, prefix string) (ServiceSpec, error) {
	if len(source.Start)+len(source.Stop)+len(source.Enable) == 0 {
		return ServiceSpec{}, fmt.Errorf("%s.service 至少需要 start、stop 或 enable", prefix)
	}
	spec := ServiceSpec{}
	groups := []struct {
		name   string
		source []string
		target *[]string
	}{
		{name: "start", source: source.Start, target: &spec.Start},
		{name: "stop", source: source.Stop, target: &spec.Stop},
		{name: "enable", source: source.Enable, target: &spec.Enable},
	}
	for _, group := range groups {
		for i, item := range group.source {
			value, err := expandBuildArgReferences(item, values, true)
			if err != nil {
				return spec, fmt.Errorf("%s.service.%s[%d]: %w", prefix, group.name, i, err)
			}
			if !validServiceName(value) {
				return spec, fmt.Errorf("%s.service.%s[%d] 服务名无效", prefix, group.name, i)
			}
			*group.target = append(*group.target, value)
		}
	}
	return spec, nil
}

func validServiceName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("@._-", r) {
			continue
		}
		return false
	}
	return true
}

func applyYAMLRuntime(stage *Stage, runtime *yamlRuntime, values map[string]string, path string, index int) error {
	prefix := fmt.Sprintf("%s: stages[%d].runtime", path, index)
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
	for _, expose := range runtime.Expose {
		protocol := strings.ToLower(expose.Protocol)
		if protocol == "" {
			protocol = "tcp"
		}
		if expose.Port < 1 || expose.Port > 65535 || (protocol != "tcp" && protocol != "udp") {
			return fmt.Errorf("%s.expose 包含无效端口或协议", prefix)
		}
		stage.Exposes = append(stage.Exposes, PortSpec{Port: expose.Port, Protocol: protocol})
	}
	stage.Entrypoint, err = expandCommand(runtime.Entrypoint, values)
	if err != nil {
		return fmt.Errorf("%s.entrypoint: %w", prefix, err)
	}
	stage.Cmd, err = expandCommand(runtime.Cmd, values)
	if err != nil {
		return fmt.Errorf("%s.cmd: %w", prefix, err)
	}
	keys := make([]string, 0, len(runtime.Env))
	for key := range runtime.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := validateEnvKey(key); err != nil {
			return fmt.Errorf("%s.env.%s: %w", prefix, key, err)
		}
		value, e := expandBuildArgReferences(runtime.Env[key], values, false)
		if e != nil {
			return fmt.Errorf("%s.env.%s: %w", prefix, key, e)
		}
		stage.Steps = append(stage.Steps, BuildStep{Kind: "ENV", Env: EnvSpec{Key: key, Value: value}})
	}
	return nil
}

func expandCommand(command []string, values map[string]string) ([]string, error) {
	if command == nil {
		return nil, nil
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("命令不能为空")
	}
	result := make([]string, len(command))
	for i, value := range command {
		expanded, err := expandBuildArgReferences(value, values, false)
		if err != nil {
			return nil, err
		}
		if i == 0 && strings.TrimSpace(expanded) == "" {
			return nil, fmt.Errorf("可执行文件不能为空")
		}
		result[i] = expanded
	}
	return result, nil
}

func validateYAMLStageReferences(stages []Stage, path string) error {
	for index, stage := range stages {
		for _, step := range stage.Steps {
			if step.Kind != "COPY" || step.Copy.From == "" {
				continue
			}
			if _, err := resolvePriorStage(stages, index, step.Copy.From); err != nil {
				return fmt.Errorf("%s: stages[%d].copy.from: %w", path, index, err)
			}
		}
	}
	return nil
}
