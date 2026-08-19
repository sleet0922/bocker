package bocker

import "strings"

// applyContainerSecurity applies Bocker's single container runtime policy.
// All Bocker containers are privileged and allow nested systemd namespaces.
func applyContainerSecurity(config map[string]string) {
	config["security.privileged"] = "true"
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
}

// runtimeCompatibility relaxes service sandboxing that cannot be initialized
// reliably inside a nested container. It only changes the target container.
const runtimeCompatibility = `
if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
  mkdir -p /etc/systemd/system/service.d
  cat > /etc/systemd/system/service.d/90-bocker-runtime.conf <<'EOF'
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
