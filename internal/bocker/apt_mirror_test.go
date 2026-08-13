package bocker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAptMirrorSedScriptRewritesAllSourceFormats(t *testing.T) {
	root := t.TempDir()
	listDir := filepath.Join(root, "etc", "apt")
	listDDir := filepath.Join(listDir, "sources.list.d")
	if err := os.MkdirAll(listDDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Debian 11 及更早: 传统单文件格式
	sourcesList := "deb http://deb.debian.org/debian bullseye main\n" +
		"deb http://security.debian.org/debian-security bullseye-security main\n" +
		"deb-src http://archive.ubuntu.com/ubuntu jammy main\n"
	if err := os.WriteFile(filepath.Join(listDir, "sources.list"), []byte(sourcesList), 0o644); err != nil {
		t.Fatal(err)
	}

	// Debian 12+: deb822 格式
	debianSources := "URIs: http://deb.debian.org/debian\n" +
		"URIs: http://security.debian.org/debian-security\n"
	if err := os.WriteFile(filepath.Join(listDDir, "debian.sources"), []byte(debianSources), 0o644); err != nil {
		t.Fatal(err)
	}

	// Ubuntu 24.04+: ubuntu.sources
	ubuntuSources := "URIs: https://archive.ubuntu.com/ubuntu/\n" +
		"URIs: https://security.ubuntu.com/ubuntu/\n"
	if err := os.WriteFile(filepath.Join(listDDir, "ubuntu.sources"), []byte(ubuntuSources), 0o644); err != nil {
		t.Fatal(err)
	}

	script := aptMirrorSedScriptForRoot("https://mirrors.tuna.tsinghua.edu.cn", root)
	cmd := exec.Command("bash", "-c", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sed script failed: %v\n%s", err, out)
	}

	assertFileContains(t, filepath.Join(listDir, "sources.list"),
		"deb https://mirrors.tuna.tsinghua.edu.cn/debian bullseye main")
	assertFileContains(t, filepath.Join(listDir, "sources.list"),
		"deb https://mirrors.tuna.tsinghua.edu.cn/debian-security bullseye-security main")
	assertFileContains(t, filepath.Join(listDir, "sources.list"),
		"deb-src https://mirrors.tuna.tsinghua.edu.cn/ubuntu jammy main")
	assertFileContains(t, filepath.Join(listDDir, "debian.sources"),
		"URIs: https://mirrors.tuna.tsinghua.edu.cn/debian")
	assertFileContains(t, filepath.Join(listDDir, "debian.sources"),
		"URIs: https://mirrors.tuna.tsinghua.edu.cn/debian-security")
	assertFileContains(t, filepath.Join(listDDir, "ubuntu.sources"),
		"URIs: https://mirrors.tuna.tsinghua.edu.cn/ubuntu/")
	// No double path: /debian-security/debian-security must not appear anywhere.
	assertFileNotContains(t, filepath.Join(listDir, "sources.list"), "/debian-security/debian-security")
	assertFileNotContains(t, filepath.Join(listDDir, "debian.sources"), "/debian-security/debian-security")
}

func TestAptMirrorSedScriptSkipsMissingFiles(t *testing.T) {
	root := t.TempDir() // no /etc/apt at all
	script := aptMirrorSedScriptForRoot("https://mirror.example.com", root)
	cmd := exec.Command("bash", "-c", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script must succeed with no source files: %v\n%s", err, out)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, data)
	}
}

func assertFileNotContains(t *testing.T, path, unwanted string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), unwanted) {
		t.Fatalf("%s unexpectedly contains %q:\n%s", path, unwanted, data)
	}
}
