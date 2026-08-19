package bocker

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func writeYAMLBuildFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Incusfile")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestYAMLProjectParsesAllStructuredSteps(t *testing.T) {
	path := writeYAMLBuildFile(t, `version: 1
args:
  BASE: alpine/3.24
  APP_ENV: production
mirror: china
name: yaml-app
network: nat
stages:
  - name: builder
    from: ${BASE}
    steps:
      - workdir: /src
      - exec:
          command: chmod
          args: ["0755", /src]
      - shell: |
          set -eu
          printf '%s' ready > /out.txt
      - pkg: [ca-certificates, curl]
      - copy:
          sources: [package.json, package-lock.json]
          destination: /src/
      - env:
          BUILD_MODE: release
      - mise:
          tool: go
          version: "1.26.6"
  - from: alpine/3.24
    steps:
      - copy:
          from: builder
          sources: [/out.txt]
          destination: /app/
    runtime:
      env:
        APP_ENV: ${APP_ENV}
      entrypoint: [/bin/sh]
      cmd: [-c, cat /app/out.txt]
      expose:
        - port: 8080
          protocol: tcp
      domain: yaml.test
      autostart: false
`)
	f, err := parseIncusfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "yaml-app" || f.Network != "nat" || f.Mirror != chinaPackageMirror || len(f.Stages) != 2 {
		t.Fatalf("global config = name=%q network=%q mirror=%q stages=%d", f.Name, f.Network, f.Mirror, len(f.Stages))
	}
	if got := []string{f.Stages[0].Steps[0].Kind, f.Stages[0].Steps[1].Kind, f.Stages[0].Steps[2].Kind, f.Stages[0].Steps[3].Kind, f.Stages[0].Steps[4].Kind, f.Stages[0].Steps[5].Kind, f.Stages[0].Steps[6].Kind}; !reflect.DeepEqual(got, []string{"WORKDIR", "EXEC", "SHELL", "PKG", "COPY", "ENV", "MISE"}) {
		t.Fatalf("step kinds = %#v", got)
	}
	if got := f.Stages[0].Steps[1].ExecArgs; !reflect.DeepEqual(got, []string{"0755", "/src"}) {
		t.Fatalf("exec args = %#v", got)
	}
	if got := f.Stages[0].Steps[4].Copy.Sources; !reflect.DeepEqual(got, []string{"package.json", "package-lock.json"}) {
		t.Fatalf("copy sources = %#v", got)
	}
	if got := f.Cmd; !reflect.DeepEqual(got, []string{"-c", "cat /app/out.txt"}) {
		t.Fatalf("runtime cmd = %#v", got)
	}
	if f.Autostart == nil || *f.Autostart || f.Domain != "yaml.test" || len(f.Exposes) != 1 {
		t.Fatalf("runtime config = %#v %#v %#v", f.Autostart, f.Domain, f.Exposes)
	}
}

func TestYAMLBuildArgsOverrideAndStrictExpansion(t *testing.T) {
	path := writeYAMLBuildFile(t, `version: 1
args:
  BASE: alpine/3.24
  PORT: "8080"
name: app-${PORT}
stages:
  - from: ${BASE}
    steps:
      - exec:
          command: echo
          args: ["${PORT}"]
`)
	f, err := parseIncusfileWithBuildArgs(path, map[string]string{"PORT": "9090"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "app-9090" || f.Args[1].Value != "9090" || f.Stages[0].Steps[0].ExecArgs[0] != "9090" {
		t.Fatalf("override not applied: %#v %#v", f.Args, f.Stages[0].Steps[0])
	}
	bad := writeYAMLBuildFile(t, `version: 1
args: {BASE: "${MISSING}"}
stages: [{from: "${BASE}"}]
`)
	if _, err := parseIncusfile(bad); err == nil || !strings.Contains(err.Error(), "未声明") {
		t.Fatalf("undeclared argument should fail: %v", err)
	}
}

func TestYAMLDeclarativeOperationalSteps(t *testing.T) {
	path := writeYAMLBuildFile(t, `version: 1
args:
  VERSION: 1.2.3
stages:
  - from: debian/13
    steps:
      - download:
          output: /tmp/source.archive
          extract: /src
          attempts:
            - url: https://example.invalid/v${VERSION}.tar.gz
              sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
              format: tar.gz
              timeout: 20
              tries: 1
            - url: https://proxy.invalid/v${VERSION}.zip
              sha256: fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210
              format: zip
              move: {from: /src/module, to: /src/source}
          verify:
            path: /src/source/version.go
            pattern: 's/^Version=//p'
            value: ${VERSION}
      - exec:
          command: openssl
          args: [rand, -hex, "24"]
          capture: DB_PASSWORD
      - exec:
          command: psql
          args: [-c, "password=${DB_PASSWORD}"]
      - write:
          path: /etc/app.env
          mode: "0600"
          content: "PASSWORD=${DB_PASSWORD}\n"
      - service:
          start: [postgresql.service]
          stop: [postgresql.service]
          enable: [postgresql.service, app.service]
`)
	f, err := parseIncusfile(path)
	if err != nil {
		t.Fatal(err)
	}
	steps := f.Stages[0].Steps
	if len(steps) != 5 || steps[0].Kind != "DOWNLOAD" || steps[1].ExecCapture != "DB_PASSWORD" || steps[2].ExecArgs[1] != "password=${DB_PASSWORD}" || steps[3].Kind != "WRITE" || steps[4].Kind != "SERVICE" {
		t.Fatalf("declarative steps = %#v", steps)
	}
	if len(steps[0].Download.Attempts) != 2 || steps[0].Download.Verify.Value != "1.2.3" {
		t.Fatalf("download = %#v", steps[0].Download)
	}
	if steps[3].Write.Mode != "0600" || steps[4].Service.Stop[0] != "postgresql.service" {
		t.Fatalf("write/service = %#v %#v", steps[3].Write, steps[4].Service)
	}
}

func TestYAMLRejectsLegacyTextAndInvalidSchema(t *testing.T) {
	cases := []string{
		"FROM alpine/3.24\n",
		"version: 2\nstages: [{from: alpine/3.24}]\n",
		"version: 1\nunknown: true\nstages: [{from: alpine/3.24}]\n",
		"version: 1\nstages: [{from: alpine/3.24, unknown: true}]\n",
		"version: 1\nstages: [{from: alpine/3.24, steps: [{exec: {command: echo, unknown: true}}]}]\n",
		"version: 1\nstages: [{from: alpine/3.24, steps: [{exec: {command: echo}, shell: hi}]}]\n",
		"version: 1\nstages: [{from: alpine/3.24, steps: [{}]}]\n",
		"version: 1\nstages: [{from: alpine/3.24, steps: [{shell: '   '}]}]\n",
		"version: 1\nstages: [{from: alpine/3.24, steps: [{pkg: []}]}]\n",
		"version: 1\nstages: [{from: alpine/3.24, steps: [{env: {}}]}]\n",
		"version: 1\nstages: [{from: alpine/3.24, steps: [{copy: {sources: [a, b], destination: /tmp/out}}]}]\n",
		"version: 1\nstages: [{from: alpine/3.24, runtime: {expose: [{port: 0}]}}]\n",
		"version: 1\nstages: [{name: build, from: alpine/3.24, runtime: {autostart: false}}, {from: alpine/3.24}]\n",
		"version: 1\nstages: [{name: build, from: alpine/3.24}, {name: BUILD, from: alpine/3.24}]\n",
		"version: 1\nstages: [{name: '0', from: alpine/3.24}]\n",
		"version: 1\nstages: [{from: alpine/3.24, steps: [{mise: {tool: go, version: latest}}]}]\n",
		"version: 1\nstages: [{from: alpine/3.24, steps: [{copy: {from: later, sources: [/out], destination: /out}}]}, {name: later, from: alpine/3.24}]\n",
		"version: 1\nstages: [{from: alpine/3.24, runtime: {expose: [{port: 80}, {port: 80, protocol: tcp}]}}]\n",
		"version: 1\nargs: {A: '${B}', B: '${A}'}\nstages: [{from: alpine/3.24}]\n",
		"version: 1\nstages: [{from: alpine/3.24, steps: [{exec: {command: echo, args: ['${MISSING}']}}]}]\n",
		"version: 1\nstages:\n  - &base {from: alpine/3.24}\n  - *base\n",
		"version: 1\nstages:\n  - &base {from: alpine/3.24}\n",
		"version: 1\nstages: [{from: alpine/3.24}]\n---\nversion: 1\nstages: [{from: alpine/3.24}]\n",
	}
	for _, content := range cases {
		path := writeYAMLBuildFile(t, content)
		if _, err := parseIncusfile(path); err == nil {
			t.Fatalf("invalid YAML unexpectedly accepted:\n%s", content)
		}
	}
	duplicate := writeYAMLBuildFile(t, "version: 1\nstages: [{from: alpine/3.24}]\nversion: 1\n")
	if _, err := parseIncusfile(duplicate); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("duplicate key should fail: %v", err)
	}
}

func TestYAMLExpandsStructuredPackageAndCopyFields(t *testing.T) {
	path := writeYAMLBuildFile(t, `version: 1
args:
  PACKAGE: curl
  SOURCE_A: one.txt
  DESTINATION: /opt/app/
  BUILDER: builder
stages:
  - name: builder
    from: alpine/3.24
    steps:
      - pkg: [ca-certificates, "${PACKAGE}"]
  - from: alpine/3.24
    steps:
      - copy:
          from: "${BUILDER}"
          sources: ["${SOURCE_A}", two.txt]
          destination: "${DESTINATION}"
`)
	f, err := parseIncusfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Stages[0].Steps[0].Packages; !reflect.DeepEqual(got, []string{"ca-certificates", "curl"}) {
		t.Fatalf("packages = %#v", got)
	}
	cp := f.Stages[1].Steps[0].Copy
	if cp.From != "builder" || cp.Dst != "/opt/app/" || !reflect.DeepEqual(cp.Sources, []string{"one.txt", "two.txt"}) {
		t.Fatalf("copy = %#v", cp)
	}
}

func TestYAMLDefaultPathAndRealProjects(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.WriteFile("Incusfile", []byte("version: 1\nstages: [{from: alpine/3.24}]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := parseIncusfile("")
	if err != nil || f.Path != "Incusfile" {
		t.Fatalf("default YAML path: f=%#v err=%v", f, err)
	}
	_, testFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(testFile), "..", "..")
	projects := []string{
		"yaml-projects/hello",
		"yaml-projects/multi-stage",
		"yaml-projects/runtime",
		"mise/node",
		"mise/python",
		"mise/rust",
		"packages/alpine",
		"packages/ubuntu",
	}
	for _, project := range projects {
		path := filepath.Join(repoRoot, "testdata", project, "Incusfile")
		if _, err := parseIncusfile(path); err != nil {
			t.Fatalf("real project %s: %v", project, err)
		}
	}
}

func TestYAMLDefaultPathDoesNotUseLegacyYAMLExtension(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	legacy := []byte("version: 1\nstages: [{from: alpine/3.24}]\n")
	if err := os.WriteFile("Incusfile.yaml", legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseIncusfile(""); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default lookup unexpectedly accepted Incusfile.yaml: %v", err)
	}
}
