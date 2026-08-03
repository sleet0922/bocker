package main

import (
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

func TestValidateIPv6NetworkSetting(t *testing.T) {
	for _, value := range []string{"auto", "none", "fd42:1234:5678::1/64", "2001:db8::1/64"} {
		if err := validateIPv6NetworkSetting(value); err != nil {
			t.Fatalf("validateIPv6NetworkSetting(%q): %v", value, err)
		}
	}
	for _, value := range []string{"10.0.0.1/24", "invalid", ""} {
		if err := validateIPv6NetworkSetting(value); err == nil {
			t.Fatalf("validateIPv6NetworkSetting(%q) unexpectedly succeeded", value)
		}
	}
}

func TestApplyNATIPv6Config(t *testing.T) {
	config := api.ConfigMap{}
	applyNATIPv6Config(config, "auto")
	if config["ipv6.address"] != "auto" || config["ipv6.nat"] != "true" || config["ipv6.dhcp"] != "true" || config["ipv6.dhcp.stateful"] != "false" {
		t.Fatalf("auto IPv6 config = %#v", config)
	}

	applyNATIPv6Config(config, "none")
	if config["ipv6.address"] != "none" || config["ipv6.nat"] != "false" || config["ipv6.dhcp"] != "false" || config["ipv6.dhcp.stateful"] != "false" {
		t.Fatalf("disabled IPv6 config = %#v", config)
	}
}

func TestContainerIPv6Addresses(t *testing.T) {
	ct := &Container{State: &ContainerState{Network: map[string]NICState{
		"eth0": {Addresses: []NICAddr{
			{Family: "inet", Address: "10.0.100.10", Scope: "global"},
			{Family: "inet6", Address: "fe80::1", Scope: "link"},
			{Family: "inet6", Address: "fd42::20", Scope: "global"},
			{Family: "inet6", Address: "2001:db8::20", Scope: "global"},
		}},
		"lo": {Type: "loopback", Addresses: []NICAddr{{Family: "inet6", Address: "::1", Scope: "host"}}},
	}}}

	addresses := ct.IPv6Addresses()
	if len(addresses) != 2 || addresses[0] != "2001:db8::20" || addresses[1] != "fd42::20" {
		t.Fatalf("IPv6Addresses() = %#v", addresses)
	}
	if ct.IPv6() != "2001:db8::20" {
		t.Fatalf("IPv6() = %q", ct.IPv6())
	}
	if got := ct.IPAddresses(); len(got) != 3 || got[0] != "10.0.100.10" {
		t.Fatalf("IPAddresses() = %#v", got)
	}
}

func TestDualStackPortMappings(t *testing.T) {
	ct := &Container{Devices: map[string]map[string]string{
		"port-8080-tcp": {"type": "proxy", "listen": "tcp:[::]:8080", "connect": "tcp:[fd42::20]:80"},
	}}
	mappings := ct.PortMappings()
	if len(mappings) != 1 {
		t.Fatalf("PortMappings() = %#v", mappings)
	}
	mapping := mappings[0]
	if !mapping.IPv4 || !mapping.IPv6 || mapping.HostPort != 8080 || mapping.ContainerPort != 80 || mapping.Protocol != "tcp" {
		t.Fatalf("mapping = %#v", mapping)
	}
	if got := proxyEndpoint("tcp", "fd42::20", 80); got != "tcp:[fd42::20]:80" {
		t.Fatalf("proxyEndpoint() = %q", got)
	}
}
