package bocker

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildArgsFromArgs(t *testing.T) {
	overrides, clean, err := buildArgsFromArgs([]string{
		"--network", "nat",
		"--build-arg", "VERSION=18",
		"--build-arg=MIRROR=https://mirror.example/repo?a=b",
		"Incusfile",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOverrides := map[string]string{
		"VERSION": "18",
		"MIRROR":  "https://mirror.example/repo?a=b",
	}
	if !reflect.DeepEqual(overrides, wantOverrides) {
		t.Fatalf("build args = %#v, want %#v", overrides, wantOverrides)
	}
	if want := []string{"--network", "nat", "Incusfile"}; !reflect.DeepEqual(clean, want) {
		t.Fatalf("clean args = %#v, want %#v", clean, want)
	}

	for _, args := range [][]string{
		{"--build-arg"},
		{"--build-arg", "VERSION"},
		{"--build-arg", "BAD-NAME=1"},
		{"--build-arg", "VERSION=1", "--build-arg=VERSION=2"},
	} {
		if _, _, err := buildArgsFromArgs(args); err == nil {
			t.Fatalf("buildArgsFromArgs(%#v) unexpectedly succeeded", args)
		}
	}
}

func TestBuildArgsExpandAndStaySeparateFromEnv(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", `ARG DISTRO=alpine
ARG BASE=${DISTRO}/3.24
ARG FLAVOR=default
ARG SOURCE=artifact
ARG PORT=8080
NAME arg-${FLAVOR}
NETWORK nat
FROM ${BASE}
WORKDIR /opt/${FLAVOR}
COPY ${SOURCE} /tmp/${SOURCE}
RUN test "$FLAVOR" = override
ENV RUNTIME_FLAVOR=${FLAVOR}
ENV RUNTIME_REFERENCE=$${HOME}
EXPOSE ${PORT}/tcp
CMD ["/bin/sh", "-c", "printf '%s:%s' '${FLAVOR}' '${HOME}'"]
`)
	f, err := parseIncusfileWithBuildArgs(p, map[string]string{
		"FLAVOR": "override",
		"PORT":   "9090",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.From, "alpine/3.24"; got != want {
		t.Fatalf("FROM = %q, want %q", got, want)
	}
	if got, want := f.Name, "arg-override"; got != want {
		t.Fatalf("NAME = %q, want %q", got, want)
	}
	if got, want := f.Exposes, []PortSpec{{Port: 9090, Protocol: "tcp"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EXPOSE = %#v, want %#v", got, want)
	}
	if got, want := f.Args, []ArgSpec{
		{Key: "DISTRO", Value: "alpine"},
		{Key: "BASE", Value: "alpine/3.24"},
		{Key: "FLAVOR", Value: "override"},
		{Key: "SOURCE", Value: "artifact"},
		{Key: "PORT", Value: "9090"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ARG = %#v, want %#v", got, want)
	}
	if got, want := f.Env, []EnvSpec{
		{Key: "RUNTIME_FLAVOR", Value: "override"},
		{Key: "RUNTIME_REFERENCE", Value: "${HOME}"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ENV = %#v, want %#v", got, want)
	}
	if got := buildArgEnvironment(f.Args)["FLAVOR"]; got != "override" {
		t.Fatalf("RUN environment FLAVOR = %q", got)
	}
	if got, want := f.Cmd[2], "printf '%s:%s' 'override' '${HOME}'"; got != want {
		t.Fatalf("CMD = %q, want %q", got, want)
	}

	var sawWorkdir, sawCopy, sawRun bool
	for _, step := range f.Steps {
		switch step.Kind {
		case "WORKDIR":
			sawWorkdir = step.Workdir == "/opt/override"
		case "COPY":
			sawCopy = step.Copy.Src == "artifact" && step.Copy.Dst == "/tmp/artifact"
		case "RUN":
			sawRun = step.Run == `test "$FLAVOR" = override`
		}
	}
	if !sawWorkdir || !sawCopy || !sawRun {
		t.Fatalf("expanded steps missing: WORKDIR=%v COPY=%v RUN=%v", sawWorkdir, sawCopy, sawRun)
	}

	properties := buildImageProperties(f)
	for key, value := range properties {
		if strings.Contains(strings.ToLower(key), "arg") || strings.Contains(value, "DISTRO") || strings.Contains(value, "FLAVOR\":\"override") {
			t.Fatalf("ARG leaked into image property %s=%q", key, value)
		}
	}
	if !strings.Contains(properties["user.bocker.env"], "RUNTIME_FLAVOR") {
		t.Fatalf("runtime ENV missing from image properties: %#v", properties)
	}
}

func TestBuildArgsApplyToEveryStage(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", `ARG VALUE=shared
FROM alpine/3.24 AS builder
RUN test "$VALUE" = shared
FROM alpine/3.24
RUN test "$VALUE" = shared
`)
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatal(err)
	}
	for stage := range f.Stages {
		if got := buildArgEnvironment(f.Args)["VALUE"]; got != "shared" {
			t.Fatalf("stage %d ARG VALUE = %q", stage, got)
		}
	}
}

func TestBuildArgValidation(t *testing.T) {
	tests := []string{
		"FROM alpine/3.24\nARG LATE=value\n",
		"ARG DUP=one\nARG DUP=two\nFROM alpine/3.24\n",
		"ARG BAD-NAME=value\nFROM alpine/3.24\n",
		"ARG SECOND=${MISSING}\nFROM alpine/3.24\n",
		"ARG BASE=alpine/3.24\nFROM ${MISSING}\n",
	}
	for _, content := range tests {
		p := writeIncusfile(t, "Incusfile", content)
		if _, err := parseIncusfile(p); err == nil {
			t.Fatalf("invalid Incusfile unexpectedly succeeded:\n%s", content)
		}
	}

	p := writeIncusfile(t, "Incusfile", "ARG KNOWN=value\nFROM alpine/3.24\n")
	if _, err := parseIncusfileWithBuildArgs(p, map[string]string{"UNKNOWN": "value"}); err == nil || !strings.Contains(err.Error(), "未在 Incusfile 中声明") {
		t.Fatalf("unknown override error = %v", err)
	}
}
