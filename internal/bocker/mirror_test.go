package bocker

import (
	"os"
	"testing"
)

func TestMirrorURLDefaultsToOfficial(t *testing.T) {
	os.Unsetenv(imageServerEnv)
	if got := MirrorURL(); got != defaultMirrorURL {
		t.Fatalf("MirrorURL() = %q, want %q", got, defaultMirrorURL)
	}
}

func TestMirrorURLHonorsEnvironmentOverride(t *testing.T) {
	const custom = "https://mirrors.tuna.tsinghua.edu.cn/lxc-images/"
	t.Setenv(imageServerEnv, custom)
	if got := MirrorURL(); got != custom {
		t.Fatalf("MirrorURL() = %q, want %q", got, custom)
	}
	os.Unsetenv(imageServerEnv)
}

func TestValidateMirrorServer(t *testing.T) {
	for _, valid := range []string{
		"https://images.linuxcontainers.org/",
		"http://mirror.example.com/lxc-images/",
	} {
		if err := validateMirrorServer(valid); err != nil {
			t.Errorf("validateMirrorServer(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{
		"",
		"ftp://mirror.example.com/lxc-images/",
		"https://",
		"not-a-url",
	} {
		if err := validateMirrorServer(invalid); err == nil {
			t.Errorf("validateMirrorServer(%q) = nil, want error", invalid)
		}
	}
}
