package bocker

import "testing"

func TestParseNetworkMode(t *testing.T) {
	tests := []struct {
		input string
		want  NetworkMode
		ok    bool
	}{
		{input: "", want: NetworkBridge, ok: true},
		{input: "bridge", want: NetworkBridge, ok: true},
		{input: "nat", want: NetworkNAT, ok: true},
		{input: "macvlan", ok: false},
		{input: "bridged", ok: false},
	}
	for _, tc := range tests {
		got, err := ParseNetworkMode(tc.input)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Errorf("ParseNetworkMode(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
			}
		} else if err == nil {
			t.Errorf("ParseNetworkMode(%q) 应失败", tc.input)
		}
	}
}

func TestApplyBockerDNSConfig(t *testing.T) {
	config := map[string]string{"ipv4.address": "10.0.100.1/24"}
	applyBockerDNSConfig(config)
	if config["dns.mode"] != "managed" || config["dns.domain"] != "bocker" {
		t.Fatalf("DNS config = %#v, want managed bocker DNS", config)
	}
}

func TestNetworkModeChoices(t *testing.T) {
	tests := []struct {
		name    string
		current NetworkMode
		want    []NetworkMode
	}{
		{name: "bridge current", current: NetworkBridge, want: []NetworkMode{NetworkBridge, NetworkNAT}},
		{name: "nat current", current: NetworkNAT, want: []NetworkMode{NetworkNAT, NetworkBridge}},
		{name: "invalid current", current: NetworkMode("unknown"), want: []NetworkMode{NetworkBridge, NetworkNAT}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := networkModeChoices(tc.current)
			if len(got) != len(tc.want) {
				t.Fatalf("networkModeChoices(%q) = %#v, want %#v", tc.current, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("networkModeChoices(%q) = %#v, want %#v", tc.current, got, tc.want)
				}
			}
		})
	}
}

func TestNetworkModeFromArgs(t *testing.T) {
	mode, args, err := networkModeFromArgs([]string{"--network", "nat", "debian:12", "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if mode != NetworkNAT {
		t.Fatalf("网络模式 = %q, want nat", mode)
	}
	if len(args) != 2 || args[0] != "debian:12" || args[1] != "demo" {
		t.Fatalf("清理后的参数 = %#v", args)
	}
}

func TestNetworkModeFromArgsRejectsUnknown(t *testing.T) {
	if _, _, err := networkModeFromArgs([]string{"--network=ovn"}); err == nil {
		t.Fatal("未知网络模式应失败")
	}
}

func TestNetworkDeviceMappings(t *testing.T) {
	bridge := networkDevice(NetworkBridge, "ens18", "00:16:3e:00:00:01")
	if bridge["nictype"] != "macvlan" || bridge["parent"] != "ens18" || bridge["network"] != "" {
		t.Fatalf("bridge device = %#v", bridge)
	}
	if bridge["hwaddr"] != "00:16:3e:00:00:01" {
		t.Fatalf("bridge hwaddr = %q", bridge["hwaddr"])
	}

	nat := networkDevice(NetworkNAT, "ignored", "")
	if nat["nictype"] != "" || nat["network"] != natNetworkName || nat["parent"] != "" {
		t.Fatalf("nat device = %#v", nat)
	}
}

func TestContainerNetworkModeUsesLocalDevice(t *testing.T) {
	bridge := &Container{
		Config:  map[string]string{containerNetworkConfig: string(NetworkBridge)},
		Devices: map[string]map[string]string{defaultNICName: networkDevice(NetworkBridge, "ens18", "")},
	}
	mode, err := networkModeFromContainer(bridge)
	if err != nil || mode != NetworkBridge {
		t.Fatalf("bridge mode = %q, %v", mode, err)
	}

	nat := &Container{
		Config:  map[string]string{containerNetworkConfig: string(NetworkNAT)},
		Devices: map[string]map[string]string{defaultNICName: networkDevice(NetworkNAT, "", "")},
	}
	mode, err = networkModeFromContainer(nat)
	if err != nil || mode != NetworkNAT {
		t.Fatalf("nat mode = %q, %v", mode, err)
	}
}

func TestContainerNetworkModeDetectsManagedBridgeDevice(t *testing.T) {
	ct := &Container{
		Devices: map[string]map[string]string{
			defaultNICName: networkDevice(NetworkNAT, "", ""),
		},
	}
	mode, err := networkModeFromContainer(ct)
	if err != nil || mode != NetworkNAT {
		t.Fatalf("managed bridge mode = %q, %v", mode, err)
	}
}

func TestContainerNetworkModeDetectsBockerBr0Bridge(t *testing.T) {
	ct := &Container{
		Devices: map[string]map[string]string{
			defaultNICName: {
				"type":    "nic",
				"name":    defaultNICName,
				"network": bridgeNetworkName,
			},
		},
	}
	mode, err := networkModeFromContainer(ct)
	if err != nil || mode != NetworkBridge {
		t.Fatalf("bocker-br0 bridge mode = %q, %v", mode, err)
	}
}

func TestIsWirelessInterface(t *testing.T) {
	if isWirelessInterface("lo") {
		t.Error("lo interface should not be wireless")
	}
}

func TestConflictingIPv4Route(t *testing.T) {
	routes := `default via 192.168.10.1 dev eth0
10.0.100.0/24 dev existing proto kernel scope link src 10.0.100.1
172.16.0.0/16 via 192.168.10.2 dev eth0
`
	for _, test := range []struct {
		cidr     string
		conflict bool
	}{
		{cidr: "10.0.100.1/24", conflict: true},
		{cidr: "10.0.100.129/25", conflict: true},
		{cidr: "172.16.20.1/24", conflict: true},
		{cidr: "10.0.200.1/24", conflict: false},
	} {
		_, conflict, err := conflictingIPv4Route(test.cidr, routes)
		if err != nil {
			t.Fatalf("conflictingIPv4Route(%q): %v", test.cidr, err)
		}
		if conflict != test.conflict {
			t.Errorf("conflictingIPv4Route(%q) = %v, want %v", test.cidr, conflict, test.conflict)
		}
	}
	if _, _, err := conflictingIPv4Route("not-a-cidr", routes); err == nil {
		t.Fatal("invalid CIDR should fail")
	}
	if _, conflict, err := conflictingIPv4Route("10.0.100.1/24", routes, "existing"); err != nil || conflict {
		t.Fatalf("own network route should be ignored: conflict=%v err=%v", conflict, err)
	}
}
