package bocker

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/lxc/incus/v7/shared/api"
)

// NetworkMode is the only network choice exposed by bocker.
//
// bridge maps to Incus macvlan so a container gets a first-class address on
// the physical LAN. nat maps to an Incus managed bridge with masquerading.
type NetworkMode string

const (
	NetworkBridge NetworkMode = "bridge"
	NetworkNAT    NetworkMode = "nat"

	defaultNetworkMode     NetworkMode = NetworkBridge
	networkModeEnv                     = "BOCKER_NETWORK"
	natNetworkName                     = "bocker-nat"
	natNetworkCIDR                     = "10.0.100.1/24"
	natNetworkCIDREnv                  = "BOCKER_NAT_CIDR"
	natNetworkIPv6CIDR                 = "auto"
	natNetworkIPv6CIDREnv              = "BOCKER_NAT_IPV6_CIDR"
	containerNetworkConfig             = "user.bocker.network"
)

// ParseNetworkMode accepts only bocker's public network names. Incus
// implementation names (macvlan/bridge) deliberately are not accepted here.
func ParseNetworkMode(value string) (NetworkMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return defaultNetworkMode, nil
	case string(NetworkBridge):
		return NetworkBridge, nil
	case string(NetworkNAT):
		return NetworkNAT, nil
	default:
		return "", fmt.Errorf("网络模式 %q 无效，只支持 bridge 或 nat", value)
	}
}

func configuredNetworkMode() (NetworkMode, error) {
	return ParseNetworkMode(os.Getenv(networkModeEnv))
}

func networkModeChoices(current NetworkMode) []NetworkMode {
	if current != NetworkBridge && current != NetworkNAT {
		current = defaultNetworkMode
	}
	other := NetworkNAT
	if current == NetworkNAT {
		other = NetworkBridge
	}
	return []NetworkMode{current, other}
}

func selectNetworkMode(current NetworkMode) (NetworkMode, bool) {
	modes := networkModeChoices(current)
	labels := make([]string, len(modes))
	for i, mode := range modes {
		switch mode {
		case NetworkNAT:
			labels[i] = "NAT - 私有网络，通过宿主机访问外网"
		default:
			labels[i] = "Bridge - 局域网直连，容器获取局域网 IP"
		}
	}
	choice := selectMenu(labels, fmt.Sprintf("选择网络模式（当前默认: %s）", modes[0]))
	if choice < 0 {
		return "", false
	}
	return modes[choice], true
}

func networkModeFromArgs(args []string) (NetworkMode, []string, error) {
	mode, err := configuredNetworkMode()
	if err != nil {
		return "", nil, err
	}
	clean := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--network":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s 需要 bridge 或 nat", arg)
			}
			mode, err = ParseNetworkMode(args[i+1])
			if err != nil {
				return "", nil, err
			}
			i++
		case "--network=bridge":
			mode = NetworkBridge
		case "--network=nat":
			mode = NetworkNAT
		default:
			if strings.HasPrefix(arg, "--network=") {
				return "", nil, fmt.Errorf("网络模式 %q 无效，只支持 bridge 或 nat", strings.TrimPrefix(arg, "--network="))
			}
			clean = append(clean, arg)
		}
	}
	return mode, clean, nil
}

func hasNetworkOverride(args []string) bool {
	for _, arg := range args {
		if arg == "--network" || strings.HasPrefix(arg, "--network=") {
			return true
		}
	}
	return false
}

func networkModeFromContainer(ct *Container) (NetworkMode, error) {
	if ct != nil && ct.Config != nil {
		if raw := strings.TrimSpace(ct.Config[containerNetworkConfig]); raw != "" {
			return ParseNetworkMode(raw)
		}
	}
	if ct != nil {
		dev := ct.ExpandedDevices[defaultNICName]
		if dev == nil {
			dev = ct.Devices[defaultNICName]
		}
		if dev != nil && dev["type"] == "nic" {
			if dev["network"] == natNetworkName {
				return NetworkNAT, nil
			}
			switch dev["nictype"] {
			case "macvlan":
				return NetworkBridge, nil
			case "bridged":
				return NetworkNAT, nil
			}
		}
	}
	return configuredNetworkMode()
}

func networkDevice(mode NetworkMode, parent, mac string) map[string]string {
	device := map[string]string{"type": "nic", "name": defaultNICName}
	if mac != "" {
		device["hwaddr"] = mac
	}
	if mode == NetworkBridge {
		device["nictype"] = "macvlan"
		device["parent"] = parent
	} else {
		device["network"] = natNetworkName
	}
	return device
}

// configuredNATIPv6CIDR follows Incus' managed bridge default: generate an
// unused IPv6 ULA subnet and enable IPv6 NAT. An explicit value can be a CIDR
// or "none"; "auto" asks Incus to generate the subnet on creation.
func configuredNATIPv6CIDR() (cidr string, explicitlySet bool, err error) {
	raw, explicitlySet := os.LookupEnv(natNetworkIPv6CIDREnv)
	if !explicitlySet || strings.TrimSpace(raw) == "" {
		return natNetworkIPv6CIDR, false, nil
	}
	cidr = strings.ToLower(strings.TrimSpace(raw))
	if err := validateIPv6NetworkSetting(cidr); err != nil {
		return "", true, fmt.Errorf("%s=%q invalid: %w", natNetworkIPv6CIDREnv, raw, err)
	}
	return cidr, true, nil
}

func validateIPv6NetworkSetting(value string) error {
	if value == "auto" || value == "none" {
		return nil
	}
	ip, _, err := net.ParseCIDR(value)
	if err != nil {
		return err
	}
	if ip == nil || ip.To4() != nil {
		return fmt.Errorf("must be an IPv6 CIDR, auto, or none")
	}
	return nil
}

func applyNATIPv6Config(config api.ConfigMap, cidr string) {
	config["ipv6.address"] = cidr
	if cidr == "none" {
		config["ipv6.nat"] = "false"
		config["ipv6.dhcp"] = "false"
		config["ipv6.dhcp.stateful"] = "false"
		return
	}

	// Incus defaults to stateless DHCPv6 plus router advertisements on new
	// managed bridges. Stateful DHCPv6 remains off until static leases exist.
	config["ipv6.nat"] = "true"
	config["ipv6.dhcp"] = "true"
	config["ipv6.dhcp.stateful"] = "false"
}

func (c *IncusClient) ensureNATNetwork() error {
	if err := c.ready(); err != nil {
		return err
	}
	cidr := strings.TrimSpace(os.Getenv(natNetworkCIDREnv))
	if cidr == "" {
		cidr = natNetworkCIDR
	}
	if err := validateCIDR(cidr); err != nil {
		return fmt.Errorf("%s=%q 无效: %w", natNetworkCIDREnv, cidr, err)
	}
	ipv6CIDR, ipv6Explicit, err := configuredNATIPv6CIDR()
	if err != nil {
		return err
	}
	network, etag, err := c.server.GetNetwork(natNetworkName)
	if err != nil {
		if !api.StatusErrorCheck(err, 404) {
			return fmt.Errorf("读取 NAT 网络 %s 失败: %w", natNetworkName, err)
		}
		if err := rejectHostNetworkConflict(cidr); err != nil {
			return err
		}
		config := api.ConfigMap{
			"ipv4.address": cidr,
			"ipv4.nat":     "true",
		}
		applyNATIPv6Config(config, ipv6CIDR)
		if err := c.server.CreateNetwork(api.NetworksPost{
			Name: natNetworkName,
			Type: "bridge",
			NetworkPut: api.NetworkPut{
				Config:      config,
				Description: "Bocker NAT network",
			},
		}); err != nil {
			return fmt.Errorf("创建 NAT 网络 %s 失败: %w", natNetworkName, err)
		}
		return nil
	}
	if network == nil || network.Type != "bridge" || !network.Managed {
		return fmt.Errorf("网络 %s 已存在但不是 Bocker 可管理的 bridge", natNetworkName)
	}
	config := cloneConfig(network.Config)
	changed := false
	for key, value := range map[string]string{
		"ipv4.address": cidr,
		"ipv4.nat":     "true",
	} {
		if config[key] != value {
			config[key] = value
			changed = true
		}
	}
	// Migrate the IPv4-only Bocker 1.0 network, but retain an already
	// provisioned IPv6 subnet unless the operator explicitly overrides it.
	if (ipv6Explicit && ipv6CIDR != "auto") || config["ipv6.address"] == "" || config["ipv6.address"] == "none" {
		before := cloneConfig(api.ConfigMap(config))
		applyNATIPv6Config(api.ConfigMap(config), ipv6CIDR)
		for _, key := range []string{"ipv6.address", "ipv6.nat", "ipv6.dhcp", "ipv6.dhcp.stateful"} {
			if before[key] != config[key] {
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	if err := c.server.UpdateNetwork(natNetworkName, api.NetworkPut{Config: apiConfig(config), Description: network.Description}, etag); err != nil {
		return fmt.Errorf("更新 NAT 网络 %s 失败: %w", natNetworkName, err)
	}
	return nil
}

// rejectHostNetworkConflict prevents a managed NAT bridge from stealing a
// subnet already routed by the host. Incus otherwise reports this only after
// partially creating the network.
func rejectHostNetworkConflict(cidr string) error {
	_, requested, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}
	out, err := exec.Command("ip", "-4", "route", "show").Output()
	if err != nil {
		return fmt.Errorf("读取宿主机 IPv4 路由失败: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "default" {
			continue
		}
		_, existing, parseErr := net.ParseCIDR(fields[0])
		if parseErr != nil {
			continue
		}
		if requested.Contains(existing.IP) || existing.Contains(requested.IP) {
			return fmt.Errorf("NAT 网段 %s 与宿主机路由 %s 冲突；请设置 %s 为未使用网段", requested, existing, natNetworkCIDREnv)
		}
	}
	return nil
}

// EnsureNetworkMode validates or creates only the backing network needed by
// Bocker. NICs are always attached locally to each container so bridge and NAT
// containers can coexist without mutating a shared profile.
func (c *IncusClient) EnsureNetworkMode(mode NetworkMode) error {
	if err := c.ready(); err != nil {
		return err
	}
	parsed, err := ParseNetworkMode(string(mode))
	if err != nil {
		return err
	}
	if parsed == NetworkBridge {
		_, err = detectBridgeParent()
		return err
	}
	return c.ensureNATNetwork()
}

func (c *IncusClient) newContainerNetworkDevice(mode NetworkMode) (map[string]string, error) {
	parsed, err := ParseNetworkMode(string(mode))
	if err != nil {
		return nil, err
	}
	if err := c.EnsureNetworkMode(parsed); err != nil {
		return nil, err
	}
	if parsed == NetworkNAT {
		return networkDevice(parsed, "", ""), nil
	}
	parent, err := detectBridgeParent()
	if err != nil {
		return nil, err
	}
	mac, err := randomMAC()
	if err != nil {
		return nil, err
	}
	return networkDevice(parsed, parent, mac), nil
}

func (c *IncusClient) SetContainerNetwork(name string, mode NetworkMode, forceNewMAC bool) error {
	if err := c.ready(); err != nil {
		return err
	}
	parsed, err := ParseNetworkMode(string(mode))
	if err != nil {
		return err
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	current := convertContainer(full)
	if !forceNewMAC {
		currentMode, modeErr := networkModeFromContainer(current)
		if modeErr == nil && currentMode == parsed && current.Devices[defaultNICName] != nil {
			if parsed != NetworkNAT {
				return nil
			}
			if err := c.EnsureNetworkMode(parsed); err != nil {
				return err
			}
			return nil
		}
	}
	if err := c.EnsureNetworkMode(parsed); err != nil {
		return err
	}
	mac := ""
	if !forceNewMAC && parsed == NetworkBridge {
		mac = current.NICMAC(defaultNICName)
	}
	if parsed == NetworkBridge && mac == "" {
		mac, err = randomMAC()
		if err != nil {
			return err
		}
	}
	put := writableInstance(full)
	devices := cloneDevices(full.Devices)
	if devices == nil {
		devices = map[string]map[string]string{}
	}
	parent := ""
	if parsed == NetworkBridge {
		parent, err = detectBridgeParent()
		if err != nil {
			return err
		}
	}
	devices[defaultNICName] = networkDevice(parsed, parent, mac)
	put.Devices = apiDevices(devices)
	if put.Config == nil {
		put.Config = api.ConfigMap{}
	}
	put.Config[containerNetworkConfig] = string(parsed)
	return c.updateInstance(name, etag, put)
}
