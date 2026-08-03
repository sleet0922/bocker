package main

import (
	"fmt"
	"os"
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
		case "--network", "-N":
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
		if arg == "--network" || arg == "-N" || strings.HasPrefix(arg, "--network=") {
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
	network, etag, err := c.server.GetNetwork(natNetworkName)
	if err != nil {
		config := api.ConfigMap{
			"ipv4.address": cidr,
			"ipv4.nat":     "true",
			"ipv6.address": "none",
		}
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
		"ipv6.address": "none",
	} {
		if config[key] != value {
			config[key] = value
			changed = true
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
	if err := c.EnsureNetworkMode(parsed); err != nil {
		return err
	}
	full, etag, err := c.server.GetInstanceFull(name)
	if err != nil {
		return err
	}
	mac := ""
	if !forceNewMAC && parsed == NetworkBridge {
		mac = convertContainer(full).NICMAC(defaultNICName)
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
