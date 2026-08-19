package bocker

import (
	"fmt"
	"strings"
)

// ArgSpec is an effective build-time argument. Unlike ENV, ARG values are
// supplied only to build steps and are never persisted in image metadata.
type ArgSpec struct {
	Key   string
	Value string
}

func buildArgsFromArgs(args []string) (map[string]string, []string, error) {
	overrides := make(map[string]string)
	clean := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		assignment := ""
		switch {
		case arg == "--build-arg":
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("--build-arg 需要 KEY=VALUE")
			}
			assignment = args[i+1]
			i++
		case strings.HasPrefix(arg, "--build-arg="):
			assignment = strings.TrimPrefix(arg, "--build-arg=")
		default:
			clean = append(clean, arg)
			continue
		}
		key, value, err := parseBuildArgAssignment(assignment)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := overrides[key]; exists {
			return nil, nil, fmt.Errorf("--build-arg %s 只能指定一次", key)
		}
		overrides[key] = value
	}
	return overrides, clean, nil
}

func parseBuildArgAssignment(assignment string) (string, string, error) {
	key, value, ok := strings.Cut(assignment, "=")
	if !ok {
		return "", "", fmt.Errorf("--build-arg 需要 KEY=VALUE，得到 %q", assignment)
	}
	if err := validateArgKey(key); err != nil {
		return "", "", err
	}
	if strings.IndexByte(value, 0) >= 0 {
		return "", "", fmt.Errorf("ARG %s 的值不能包含 NUL 字符", key)
	}
	return key, value, nil
}

func validateArgKey(key string) error {
	if err := validateVariableKey(key); err != nil {
		return fmt.Errorf("ARG 变量名 %q 无效", key)
	}
	return nil
}

func validateVariableKey(key string) error {
	if key == "" {
		return fmt.Errorf("变量名不能为空")
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		valid := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9'
		if !valid {
			return fmt.Errorf("变量名含有非法字符")
		}
	}
	return nil
}

// expandBuildArgReferences expands ${NAME}. A doubled dollar sign escapes a
// reference, so $${NAME} becomes a literal ${NAME}. Unknown references may be
// preserved for runtime expansion in ENV, ENTRYPOINT and CMD.
func expandBuildArgReferences(value string, buildArgs map[string]string, strict bool) (string, error) {
	var result strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '$' {
			result.WriteByte(value[i])
			i++
			continue
		}
		if i+2 < len(value) && value[i+1] == '$' && value[i+2] == '{' {
			end := strings.IndexByte(value[i+3:], '}')
			if end < 0 {
				result.WriteString(value[i:])
				break
			}
			end += i + 3
			result.WriteString(value[i+1 : end+1])
			i = end + 1
			continue
		}
		if i+1 >= len(value) || value[i+1] != '{' {
			result.WriteByte(value[i])
			i++
			continue
		}
		end := strings.IndexByte(value[i+2:], '}')
		if end < 0 {
			if strict {
				return "", fmt.Errorf("未闭合的构建参数引用")
			}
			result.WriteString(value[i:])
			break
		}
		end += i + 2
		key := value[i+2 : end]
		replacement, exists := buildArgs[key]
		if !exists {
			if strict {
				return "", fmt.Errorf("构建参数 %s 未声明", key)
			}
			result.WriteString(value[i : end+1])
		} else {
			result.WriteString(replacement)
		}
		i = end + 1
	}
	return result.String(), nil
}

func buildArgEnvironment(args []ArgSpec) map[string]string {
	environment := make(map[string]string, len(args))
	for _, arg := range args {
		environment[arg.Key] = arg.Value
	}
	return environment
}
