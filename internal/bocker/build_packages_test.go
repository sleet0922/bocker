package bocker

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizePackageMirror(t *testing.T) {
	for _, alias := range []string{"china", "CHINA", "tuna"} {
		got, err := normalizePackageMirror(alias)
		if err != nil {
			t.Fatalf("normalizePackageMirror(%q): %v", alias, err)
		}
		if got != chinaPackageMirror {
			t.Fatalf("normalizePackageMirror(%q) = %q, want %q", alias, got, chinaPackageMirror)
		}
	}
	got, err := normalizePackageMirror("https://mirror.example.com/root/")
	if err != nil || got != "https://mirror.example.com/root" {
		t.Fatalf("custom mirror = %q, %v", got, err)
	}
	for _, invalid := range []string{"", "ftp://mirror.example.com", "https://", "https://user@example.com", "https://mirror.example.com?a=b", "https://mirror.example.com/$bad"} {
		if _, err := normalizePackageMirror(invalid); err == nil {
			t.Errorf("normalizePackageMirror(%q) should fail", invalid)
		}
	}
}

func TestParsePackagePayload(t *testing.T) {
	got, err := parsePackagePayload("ca-certificates 'postgresql-18' libssl3t64:amd64")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ca-certificates", "postgresql-18", "libssl3t64:amd64"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages = %#v, want %#v", got, want)
	}
	for _, invalid := range []string{"", "-y", "'bad package'"} {
		if _, err := parsePackagePayload(invalid); err == nil {
			t.Errorf("parsePackagePayload(%q) should fail", invalid)
		}
	}
}

func TestPackageCommandsSupportAptAndApk(t *testing.T) {
	mirrorCommand := packageMirrorCommand(chinaPackageMirror)
	for _, want := range []string{"command -v apt-get", "command -v apk", chinaPackageMirror + "/debian", "mirror='" + chinaPackageMirror + "'", "${mirror}/alpine"} {
		if !strings.Contains(mirrorCommand, want) {
			t.Errorf("mirror command missing %q", want)
		}
	}
	installCommand := packageInstallCommand([]string{"curl", "libssl3t64:amd64"})
	for _, want := range []string{"apt-get update", "apt-get install -y --no-install-recommends 'curl' 'libssl3t64:amd64'", "apk add --no-cache 'curl' 'libssl3t64:amd64'", "rm -rf /var/lib/apt/lists/*"} {
		if !strings.Contains(installCommand, want) {
			t.Errorf("install command missing %q", want)
		}
	}
}

func TestMirrorAndPackageDirectives(t *testing.T) {
	content := `ARG PACKAGE_MIRROR=https://mirror.example.com/root
ARG BUILD_PACKAGE=build-essential
MIRROR ${PACKAGE_MIRROR}
FROM debian/13
TEMP builder
  PKG ${BUILD_PACKAGE} git
  RUN test -x /usr/bin/git
END
PKG ca-certificates curl
`
	p := writeIncusfile(t, "Incusfile", content)
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.Mirror != "https://mirror.example.com/root" {
		t.Fatalf("Mirror = %q", f.Mirror)
	}
	if got, want := f.Stages[0].Steps[0].Packages, []string{"build-essential", "git"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TEMP packages = %#v, want %#v", got, want)
	}
	if got, want := f.Stages[1].Steps[0].Packages, []string{"ca-certificates", "curl"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("final packages = %#v, want %#v", got, want)
	}
}

func TestMirrorAndPackageDirectiveValidation(t *testing.T) {
	for name, content := range map[string]string{
		"mirror after FROM":   "FROM debian/13\nMIRROR china\n",
		"duplicate mirror":    "MIRROR china\nMIRROR tuna\nFROM debian/13\n",
		"invalid mirror":      "MIRROR ftp://mirror.example.com\nFROM debian/13\n",
		"package before FROM": "PKG curl\nFROM debian/13\n",
		"empty package":       "FROM debian/13\nPKG\n",
		"package option":      "FROM debian/13\nPKG -y\n",
	} {
		t.Run(name, func(t *testing.T) {
			p := writeIncusfile(t, "Incusfile", content)
			if _, err := parseIncusfile(p); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
