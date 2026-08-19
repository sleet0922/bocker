package bocker

import (
	"reflect"
	"strings"
	"testing"
)

func TestPackageMirrorAndCommandGeneration(t *testing.T) {
	for _, alias := range []string{"china", "CHINA", "tuna"} {
		got, err := normalizePackageMirror(alias)
		if err != nil || got != chinaPackageMirror {
			t.Fatalf("normalizePackageMirror(%q) = %q, %v", alias, got, err)
		}
	}
	if got, err := normalizePackageMirror("https://mirror.example.com/root/"); err != nil || got != "https://mirror.example.com/root" {
		t.Fatalf("custom mirror = %q, %v", got, err)
	}
	for _, invalid := range []string{"", "ftp://mirror.example.com", "https://", "https://user@example.com", "https://mirror.example.com?a=b"} {
		if _, err := normalizePackageMirror(invalid); err == nil {
			t.Errorf("normalizePackageMirror(%q) should fail", invalid)
		}
	}
	packages, err := validateYAMLPackages([]string{"ca-certificates", "postgresql-18", "libssl3t64:amd64"}, nil)
	if err != nil || !reflect.DeepEqual(packages, []string{"ca-certificates", "postgresql-18", "libssl3t64:amd64"}) {
		t.Fatalf("packages = %#v, %v", packages, err)
	}
	for _, invalid := range [][]string{nil, {""}, {"-y"}, {"bad package"}} {
		if _, err := validateYAMLPackages(invalid, nil); err == nil {
			t.Errorf("validateYAMLPackages(%q) should fail", invalid)
		}
	}
	install := packageInstallCommand([]string{"curl", "libssl3t64:amd64"})
	for _, required := range []string{"apt-get update", "apt-get install -y --no-install-recommends 'curl' 'libssl3t64:amd64'", "apk add --no-cache 'curl' 'libssl3t64:amd64'", "rm -rf /var/lib/apt/lists/*"} {
		if !strings.Contains(install, required) {
			t.Errorf("package install command missing %q", required)
		}
	}
}
