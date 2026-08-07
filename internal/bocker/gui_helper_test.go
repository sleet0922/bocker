package bocker

import (
	"reflect"
	"testing"
)

func TestRequestedTerminalSize(t *testing.T) {
	t.Setenv("BOCKER_TERM_WIDTH", "96")
	t.Setenv("BOCKER_TERM_HEIGHT", "31")
	if width, height := requestedTerminalSize(); width != 96 || height != 31 {
		t.Fatalf("requested terminal size = %dx%d, want 96x31", width, height)
	}

	t.Setenv("BOCKER_TERM_WIDTH", "invalid")
	if width, height := requestedTerminalSize(); width != 0 || height != 0 {
		t.Fatalf("invalid requested terminal size = %dx%d, want 0x0", width, height)
	}
}

func TestSetEnvironmentValueReplacesExistingEntry(t *testing.T) {
	environment := []string{"TERM=old", "PATH=/bin", "TERM=duplicate"}
	want := []string{"PATH=/bin", "TERM=xterm-256color"}
	got := setEnvironmentValue(environment, "TERM", "xterm-256color")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}

func TestGUIHelperAcceptsOnlyResourceCommands(t *testing.T) {
	for _, args := range [][]string{
		{"template", "list", "--json"},
		{"image", "list", "--json"},
		{"container", "list", "--json"},
	} {
		if err := validateGUIHelperArguments(args); err != nil {
			t.Fatalf("validateGUIHelperArguments(%#v): %v", args, err)
		}
	}
	for _, args := range [][]string{{"list"}, {"images"}, {"build"}, {"run"}, {"in", "demo"}} {
		if err := validateGUIHelperArguments(args); err == nil {
			t.Fatalf("legacy GUI command %#v unexpectedly succeeded", args)
		}
	}
}
