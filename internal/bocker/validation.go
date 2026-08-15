package bocker

import (
	"fmt"
	"strings"
)

// validateBockerName applies the common subset required by Incus instance
// names and by image aliases because Incusfile NAME is used for both.
func validateBockerName(name string) error {
	if len(name) < 1 || len(name) > 63 {
		return fmt.Errorf("名称必须为 1-63 个 ASCII 字符")
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return fmt.Errorf("名称不能以连字符开头或结尾")
	}
	allDigits := true
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
			return fmt.Errorf("名称 %q 只能包含 ASCII 字母、数字和连字符", name)
		}
		if c < '0' || c > '9' {
			allDigits = false
		}
	}
	if allDigits {
		return fmt.Errorf("名称不能是纯数字")
	}
	return nil
}

func validateDomainName(domain string) error {
	if len(domain) < 1 || len(domain) > 253 {
		return fmt.Errorf("域名长度必须为 1-253 个 ASCII 字符")
	}
	if strings.HasSuffix(domain, ".") {
		return fmt.Errorf("域名不能以点结尾")
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) < 1 || len(label) > 63 {
			return fmt.Errorf("域名 %q 含有空标签或超过 63 字符的标签", domain)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("域名标签 %q 不能以连字符开头或结尾", label)
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return fmt.Errorf("域名 %q 只能包含 ASCII 字母、数字、点和连字符", domain)
			}
		}
	}
	return nil
}
