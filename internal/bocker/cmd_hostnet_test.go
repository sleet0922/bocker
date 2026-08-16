package bocker

import "testing"

func TestHostShimAndManagedBridgeNamesAreDistinct(t *testing.T) {
	if defaultHostShimName == bridgeNetworkName {
		t.Fatalf("host macvlan shim and managed Wi-Fi bridge must use different names: %q", defaultHostShimName)
	}
	if legacyHostShimName != bridgeNetworkName {
		t.Fatalf("legacy shim name = %q, want managed bridge name %q for migration guards", legacyHostShimName, bridgeNetworkName)
	}
}

func TestParseLinkParent(t *testing.T) {
	parent, ok := parseLinkParent("7: bocker-shim0@ens18: <BROADCAST,MULTICAST,UP> mtu 1500")
	if !ok || parent != "ens18" {
		t.Fatalf("parseLinkParent() = %q, %v; want ens18, true", parent, ok)
	}
	if parent, ok := parseLinkParent("8: bocker-br0: <BROADCAST,MULTICAST,UP> mtu 1500"); ok || parent != "" {
		t.Fatalf("managed bridge unexpectedly has a parent: %q, %v", parent, ok)
	}
}

func TestDetailedLinkIsMacvlan(t *testing.T) {
	macvlan := "7: bocker-shim0@ens18: <BROADCAST,MULTICAST,UP> mtu 1500 macvlan mode bridge"
	bridge := "8: bocker-br0: <BROADCAST,MULTICAST,UP> mtu 1500 bridge forward_delay 1500"
	if !detailedLinkIsMacvlan(macvlan) {
		t.Fatal("macvlan metadata was not detected")
	}
	if detailedLinkIsMacvlan(bridge) {
		t.Fatal("managed Linux bridge must not be classified as a macvlan shim")
	}
}

func TestFirstRouteDevSkipsBockerVirtualInterfaces(t *testing.T) {
	routes := "default dev bocker-shim0\ndefault dev bocker-br0\ndefault via 192.0.2.1 dev ens18\n"
	if got := firstRouteDev(routes); got != "ens18" {
		t.Fatalf("firstRouteDev() = %q, want ens18", got)
	}
}
