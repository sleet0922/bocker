package bocker

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

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

func validateYAMLNode(node *yaml.Node, filePath string) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("%s:%d: YAML anchors/aliases 不被支持", filePath, node.Line)
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || seen[key.Value] {
				return fmt.Errorf("%s:%d: YAML mapping key 重复或无效", filePath, key.Line)
			}
			seen[key.Value] = true
			if err := validateYAMLNode(node.Content[i+1], filePath); err != nil {
				return err
			}
		}
	} else if node.Kind == yaml.SequenceNode {
		for _, child := range node.Content {
			if err := validateYAMLNode(child, filePath); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateGlobalYAMLConfig(f *Incusfile, mirror, network, filePath string, values map[string]string) error {
	var err error
	if mirror != "" {
		mirror, err = expandBuildArgReferences(mirror, values, true)
		if err != nil {
			return fmt.Errorf("%s: mirror: %w", filePath, err)
		}
		f.Mirror, err = normalizePackageMirror(mirror)
		if err != nil {
			return fmt.Errorf("%s: mirror: %w", filePath, err)
		}
	}
	if network != "" {
		network, err = expandBuildArgReferences(network, values, true)
		if err != nil {
			return fmt.Errorf("%s: network: %w", filePath, err)
		}
		mode, modeErr := ParseNetworkMode(network)
		if modeErr != nil {
			return fmt.Errorf("%s: network: %w", filePath, modeErr)
		}
		f.Network = string(mode)
	}
	if f.Name != "" {
		f.Name, err = expandBuildArgReferences(f.Name, values, true)
		if err != nil {
			return fmt.Errorf("%s: name: %w", filePath, err)
		}
		if err := validateBockerName(f.Name); err != nil {
			return fmt.Errorf("%s: name: %w", filePath, err)
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
		if pkg == "" || len(pkg) > 256 || strings.HasPrefix(pkg, "-") || strings.IndexFunc(pkg, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\r' || r == '\n'
		}) >= 0 {
			return nil, fmt.Errorf("软件包 %q 无效", pkg)
		}
		result[i] = pkg
	}
	return result, nil
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

func validateRuntimeMounts(mounts []RuntimeMount) error {
	seenTargets := make(map[string]int, len(mounts))
	for index, mount := range mounts {
		source := strings.TrimSpace(mount.Source)
		if source == "" {
			return fmt.Errorf("mounts[%d].source 不能为空", index)
		}
		if strings.ContainsRune(source, '\x00') {
			return fmt.Errorf("mounts[%d].source 不能包含 NUL 字符", index)
		}
		if !path.IsAbs(source) {
			return fmt.Errorf("mounts[%d].source 必须是绝对宿主路径", index)
		}
		target := strings.TrimSpace(mount.Target)
		if target == "" || !path.IsAbs(target) {
			return fmt.Errorf("mounts[%d].target 必须是绝对容器路径", index)
		}
		if strings.ContainsRune(target, '\x00') {
			return fmt.Errorf("mounts[%d].target 不能包含 NUL 字符", index)
		}
		target = path.Clean(target)
		if target == "/" {
			return fmt.Errorf("mounts[%d].target 不能是容器根路径 /", index)
		}
		mode := strings.ToLower(strings.TrimSpace(mount.Mode))
		if mode == "" {
			mode = "rw"
		}
		if mode != "ro" && mode != "rw" {
			return fmt.Errorf("mounts[%d].mode 必须是 ro 或 rw", index)
		}
		if previous, ok := seenTargets[target]; ok {
			return fmt.Errorf("mounts[%d].target %q 与 mounts[%d] 重复", index, target, previous)
		}
		seenTargets[target] = index
	}
	return nil
}

func validateYAMLStageReferences(stages []Stage, filePath string) error {
	for index, stage := range stages {
		for _, step := range stage.Steps {
			if step.Kind != "COPY" || step.Copy.From == "" {
				continue
			}
			if _, err := resolvePriorStage(stages, index, step.Copy.From); err != nil {
				return fmt.Errorf("%s: stages[%d].artifacts: %w", filePath, index, err)
			}
		}
	}
	return nil
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
