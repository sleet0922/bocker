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

func TestReadBuildFileRejectsOversizedRegularFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Incusfile")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxIncusfileBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readBuildFile(file); err == nil || !strings.Contains(err.Error(), "超过上限") {
		t.Fatalf("readBuildFile oversized regular file error = %v", err)
	}
}

func TestReadBuildFileRejectsOversizedStream(t *testing.T) {
	stream := strings.NewReader(strings.Repeat("x", maxIncusfileBytes+1))
	if _, err := readBuildFileStream(stream, "stream"); err == nil || !strings.Contains(err.Error(), "超过上限") {
		t.Fatalf("readBuildFileStream oversized input error = %v", err)
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
		"languages/go",
		"languages/node",
		"languages/python",
	}
	for _, project := range projects {
		if _, err := parseIncusfile(filepath.Join(root, project, "Incusfile")); err != nil {
			t.Fatalf("fixture %s: %v", project, err)
		}
	}
}

func TestV2RuntimeMountsNormalizeAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.conf"), []byte("ready\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "Incusfile")
	content := `version: 2
name: mounted-app
stages:
  - from: alpine/3.24
    runtime:
      mounts:
        - source: ./data
          target: /srv/data
          mode: ro
        - source: ./app.conf
          target: /etc/mounted-app.conf
          readonly: true
`
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := parseIncusfile(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Mounts) != 2 {
		t.Fatalf("mounts = %#v", f.Mounts)
	}
	wantSource := filepath.Join(dir, "data")
	if f.Mounts[0] != (RuntimeMount{Source: wantSource, Target: "/srv/data", Mode: "ro"}) {
		t.Fatalf("first mount = %#v", f.Mounts[0])
	}
	if f.Mounts[1].Source != filepath.Join(dir, "app.conf") || f.Mounts[1].Target != "/etc/mounted-app.conf" || f.Mounts[1].Mode != "ro" {
		t.Fatalf("second mount = %#v", f.Mounts[1])
	}
	f.Entrypoint = []string{"/bin/sh"}
	f.Cmd = []string{"-c", "printf '${APP_ENV}'"}
	properties := buildImageProperties(f)
	roundTrip, err := runtimeConfigFromImageProperties("mounted-app", properties)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundTrip.Mounts, f.Mounts) {
		t.Fatalf("round-trip mounts = %#v, want %#v", roundTrip.Mounts, f.Mounts)
	}
	if !reflect.DeepEqual(roundTrip.Entrypoint, f.Entrypoint) || !reflect.DeepEqual(roundTrip.Cmd, f.Cmd) {
		t.Fatalf("round-trip command = %#v %#v, want %#v %#v", roundTrip.Entrypoint, roundTrip.Cmd, f.Entrypoint, f.Cmd)
	}
	devices, err := runtimeMountDevices(f.Mounts)
	if err != nil {
		t.Fatal(err)
	}
	if devices["mount-runtime-0"]["type"] != "disk" || devices["mount-runtime-0"]["readonly"] != "true" || devices["mount-runtime-0"]["source"] != wantSource {
		t.Fatalf("directory device = %#v", devices["mount-runtime-0"])
	}
	if devices["mount-runtime-1"]["type"] != "disk" || devices["mount-runtime-1"]["readonly"] != "true" || devices["mount-runtime-1"]["source"] != filepath.Join(dir, "app.conf") {
		t.Fatalf("file device = %#v", devices["mount-runtime-1"])
	}
}

func TestV2RuntimeMountsExpandBuildArgsInMode(t *testing.T) {
	file := writeV2BuildFile(t, `version: 2
args:
  MOUNT_MODE: ro
stages:
  - from: alpine/3.24
    runtime:
      mounts:
        - source: /tmp
          target: /data
          mode: ${MOUNT_MODE}
`)
	f, err := parseIncusfile(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Mounts) != 1 || f.Mounts[0].Mode != "ro" {
		t.Fatalf("mounts = %#v, want one read-only mount", f.Mounts)
	}
}

func TestV2RuntimeMountsRejectInvalidDefinitions(t *testing.T) {
	cases := []string{
		`version: 2
stages:
  - from: alpine/3.24
    runtime:
      mounts: [{source: /tmp, target: /data, mode: bad}]
`,
		`version: 2
stages:
  - from: alpine/3.24
    runtime:
      mounts: [{source: /tmp, target: /}]
`,
		`version: 2
stages:
  - from: alpine/3.24
    runtime:
      mounts: [{source: /tmp, target: data}]
`,
		`version: 2
stages:
  - from: alpine/3.24
    runtime:
      mounts:
        - {source: /tmp, target: /data}
        - {source: /tmp, target: /data}
`,
		`version: 2
stages:
  - from: alpine/3.24
    runtime:
      mounts: [{source: /tmp, target: /data, mode: rw, readonly: true}]
`,
		`version: 2
stages:
  - from: alpine/3.24
    runtime:
      mounts: [{source: /tmp, target: /data, mode: "", readonly: true}]
`,
		`version: 2
stages:
  - from: alpine/3.24
    runtime:
      mounts: [{source: /tmp, target: /data, mode: null, readonly: false}]
`,
		`version: 2
stages:
  - from: alpine/3.24
    runtime:
      mounts: [{source: /tmp, target: /data}]
  - from: alpine/3.24
`,
	}
	for _, content := range cases {
		if _, err := parseIncusfile(writeV2BuildFile(t, content)); err == nil {
			t.Fatalf("invalid runtime.mounts unexpectedly accepted:\n%s", content)
		}
	}
}
