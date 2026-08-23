package bocker

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeV2BuildFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "Incusfile")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestV2IntentSchemaNormalizesStages(t *testing.T) {
	file := writeV2BuildFile(t, `version: 2
args:
  BASE: alpine/3.24
  APP_ENV: production
mirror: china
name: v2-app
network: nat
stages:
  - name: builder
    from: ${BASE}
    workdir: /src
    env: {BUILD_MODE: release}
    packages: [ca-certificates]
    tools: {go: "1.26.6"}
    files:
      /src/: [go.mod, cmd]
    commands:
      - [printf, '%s', ready]
      - run: [printf, '%s', generated]
        capture: VALUE
  - from: alpine/3.24
    artifacts:
      builder:
        /src/result: /app/result
    runtime:
      env: {APP_ENV: "${APP_ENV}"}
      entrypoint: [/bin/sh]
      cmd: [-c, cat /app/result]
      expose: [8080, 8081/udp]
      domain: v2.test
      autostart: false
`)
	f, err := parseIncusfile(file)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "v2-app" || f.Network != "nat" || f.Mirror != chinaPackageMirror || len(f.Stages) != 2 {
		t.Fatalf("global config = %#v", f)
	}
	got := []string{}
	for _, step := range f.Stages[0].Steps {
		got = append(got, step.Kind)
	}
	want := []string{"WORKDIR", "ENV", "PKG", "MISE", "COPY", "EXEC", "EXEC"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("step kinds = %#v, want %#v", got, want)
	}
	if f.Stages[0].Steps[6].ExecCapture != "VALUE" {
		t.Fatalf("capture = %#v", f.Stages[0].Steps[6])
	}
	if f.Stages[1].Steps[0].Copy.From != "builder" || f.Stages[1].Steps[0].Copy.Dst != "/app/result" {
		t.Fatalf("artifact = %#v", f.Stages[1].Steps[0])
	}
	if len(f.Exposes) != 2 || f.Exposes[1].Protocol != "udp" || f.Autostart == nil || *f.Autostart {
		t.Fatalf("runtime = %#v %#v", f.Exposes, f.Autostart)
	}
}

func TestV2FetchAllowsUnhashedTrustedArtifact(t *testing.T) {
	file := writeV2BuildFile(t, `version: 2
stages:
  - from: debian/13
    fetch:
      - url: https://example.test/livekit.tar.gz
        extract: /out
        format: tar.gz
`)
	f, err := parseIncusfile(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Stages[0].Steps) != 1 || f.Stages[0].Steps[0].Download.Attempts[0].SHA256 != "" {
		t.Fatalf("fetch = %#v", f.Stages[0].Steps)
	}
	if command := downloadCommand(f.Stages[0].Steps[0].Download); strings.Contains(command, "sha256sum") {
		t.Fatalf("unhashed fetch unexpectedly verifies checksum: %s", command)
	}
}

func TestV2RejectsOldShapeAndInvalidSchema(t *testing.T) {
	cases := []string{
		"version: 1\nstages: [{from: alpine/3.24}]\n",
		"version: 2\nstages: [{from: alpine/3.24, steps: [{exec: {command: echo}}]}]\n",
		"version: 2\nstages: [{from: alpine/3.24, commands: [{run: [echo], shell: bad}]}]\n",
		"version: 2\nstages: [{from: alpine/3.24, commands: [[echo, '${MISSING}']]}]\n",
		"version: 2\nstages: [{from: alpine/3.24, artifacts: {later: {/out: /out}}}, {name: later, from: alpine/3.24}]\n",
		"version: 2\nstages: [{from: alpine/3.24, runtime: {expose: [80, 80]}}]\n",
	}
	for _, content := range cases {
		if _, err := parseIncusfile(writeV2BuildFile(t, content)); err == nil {
			t.Fatalf("invalid v2 YAML unexpectedly accepted:\n%s", content)
		}
	}
}

func TestV2RealProjectFixturesParse(t *testing.T) {
	root := filepath.Join("..", "..", "testdata")
	projects := []string{
		"yaml-projects/hello",
		"yaml-projects/multi-stage",
		"yaml-projects/runtime",
		"mise/node",
		"mise/python",
		"mise/rust",
		"packages/alpine",
		"packages/ubuntu",
		"languages/c",
		"languages/java",
	}
	for _, project := range projects {
		if _, err := parseIncusfile(filepath.Join(root, project, "Incusfile")); err != nil {
			t.Fatalf("fixture %s: %v", project, err)
		}
	}
}
