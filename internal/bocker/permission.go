package bocker

import (
	"fmt"
	"strings"
)

type PermissionMode string

const (
	PermissionNormal PermissionMode = "normal"
	PermissionSuper  PermissionMode = "super"
)

const permissionConfigKey = "user.bocker.permission"

func ParsePermissionMode(value string) (PermissionMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(PermissionNormal):
		return PermissionNormal, nil
	case string(PermissionSuper):
		return PermissionSuper, nil
	default:
		return "", fmt.Errorf("权限 %q 无效，只支持 normal 或 super", value)
	}
}

func permissionModeFromArgs(args []string) (PermissionMode, []string, error) {
	mode := PermissionNormal
	clean := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--permission":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s 需要 normal 或 super", arg)
			}
			parsed, err := ParsePermissionMode(args[i+1])
			if err != nil {
				return "", nil, err
			}
			mode = parsed
			i++
		case "--permission=normal":
			mode = PermissionNormal
		case "--permission=super":
			mode = PermissionSuper
		default:
			if strings.HasPrefix(arg, "--permission=") {
				return "", nil, fmt.Errorf("权限模式 %q 无效，只支持 normal 或 super", strings.SplitN(arg, "=", 2)[1])
			}
			clean = append(clean, arg)
		}
	}
	return mode, clean, nil
}

func hasPermissionOverride(args []string) bool {
	for _, arg := range args {
		if arg == "--permission" || strings.HasPrefix(arg, "--permission=") {
			return true
		}
	}
	return false
}

func selectPermissionMode(current PermissionMode) (PermissionMode, bool) {
	options := []string{
		"普通权限 - 保持默认安全配置",
		"超级权限 - 放宽容器及内部服务隔离",
	}
	if current == PermissionSuper {
		options[0], options[1] = options[1], options[0]
	}
	choice := selectMenu(options, "选择权限模式（当前默认: "+string(current)+"）")
	if choice < 0 {
		return "", false
	}
	if (current == PermissionSuper && choice == 0) || (current != PermissionSuper && choice == 1) {
		return PermissionSuper, true
	}
	return PermissionNormal, true
}

func applyPermissionConfig(config map[string]string, mode PermissionMode) {
	if mode != PermissionSuper {
		delete(config, "security.nesting")
		if raw := strings.TrimSpace(config["raw.lxc"]); raw != "" {
			kept := make([]string, 0)
			for _, line := range strings.Split(raw, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || line == "lxc.apparmor.profile = unconfined" || line == "lxc.cap.drop =" {
					continue
				}
				kept = append(kept, line)
			}
			if len(kept) == 0 {
				delete(config, "raw.lxc")
			} else {
				config["raw.lxc"] = strings.Join(kept, "\n") + "\n"
			}
		}
		delete(config, permissionConfigKey)
		return
	}
	config["security.nesting"] = "true"
	lines := make([]string, 0)
	for _, line := range strings.Split(config["raw.lxc"], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "lxc.apparmor.profile") || strings.HasPrefix(line, "lxc.cap.drop") {
			continue
		}
		lines = append(lines, line)
	}
	lines = append(lines, "lxc.apparmor.profile = unconfined", "lxc.cap.drop =")
	config["raw.lxc"] = strings.Join(lines, "\n") + "\n"
	config[permissionConfigKey] = string(PermissionSuper)
}

// superRuntimeCompatibility installs compatibility settings inside one
// container. It never changes the host systemd manager or an Incus profile.
const superRuntimeCompatibility = `
if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
  mkdir -p /etc/systemd/system/service.d
  cat > /etc/systemd/system/service.d/90-bocker-super.conf <<'EOF'
[Service]
PrivateUsers=no
RestrictNamespaces=no
NoExecPaths=
ExecPaths=
PrivateTmp=no
ProtectSystem=no
ProtectHome=no
ProtectKernelTunables=no
ProtectKernelLogs=no
ProtectKernelModules=no
ProtectControlGroups=no
PrivateDevices=no
EOF

  failed_units=$(systemctl --failed --type=service --no-legend 2>/dev/null | awk '{print $2}')
  # Some minimal images expose systemctl but do not run a systemd bus. The
  # drop-in is still useful for the next boot, so reload is best-effort.
  systemctl daemon-reload >/dev/null 2>&1 || true

  # Debian packages can leave service-owned paths as root:adm when startup
  # was blocked by namespace restrictions. Repair only paths matching each
  # installed unit's declared User/Group.
  for unit in $(systemctl list-unit-files --type=service --no-legend 2>/dev/null | awk '{print $1}'); do
    user=$(systemctl show "$unit" -p User --value 2>/dev/null)
    [ -n "$user" ] || continue
    group=$(systemctl show "$unit" -p Group --value 2>/dev/null)
    [ -n "$group" ] || group="$user"
    name=${unit%.service}
    for dir in "/var/log/$name" "/var/lib/$name" "/var/cache/$name" "/run/$name"; do
      if [ -e "$dir" ]; then chown -R "$user:$group" "$dir" 2>/dev/null || true; fi
    done
  done

  for unit in $failed_units; do systemctl restart "$unit" >/dev/null 2>&1 || true; done
fi
`
