package bocker

import (
	"fmt"
	"strings"
)

func parseJSONOutputOption(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) == 1 && args[0] == "--json" {
		return true, nil
	}
	return false, fmt.Errorf("只支持可选参数 --json")
}

func nameOptionFromArgs(args []string) (string, []string, error) {
	name := ""
	clean := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := ""
		switch {
		case arg == "--name":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s 需要容器名", arg)
			}
			value = args[i+1]
			i++
		case strings.HasPrefix(arg, "--name="):
			value = strings.TrimPrefix(arg, "--name=")
		default:
			clean = append(clean, arg)
			continue
		}
		if name != "" {
			return "", nil, fmt.Errorf("--name 只能指定一次")
		}
		name = strings.TrimSpace(value)
		if name == "" {
			return "", nil, fmt.Errorf("--name 需要非空容器名")
		}
	}
	return name, clean, nil
}
