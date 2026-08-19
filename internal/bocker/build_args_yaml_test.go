package bocker

import (
	"reflect"
	"testing"
)

func TestBuildArgsFromCLIStayStrictlyScoped(t *testing.T) {
	overrides, clean, err := buildArgsFromArgs([]string{
		"--network", "nat",
		"--build-arg", "VERSION=18",
		"--build-arg=MIRROR=https://mirror.example/repo?a=b",
		"Incusfile.yaml",
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
	if want := []string{"--network", "nat", "Incusfile.yaml"}; !reflect.DeepEqual(clean, want) {
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
