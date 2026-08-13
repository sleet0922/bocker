package bocker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSubidFile(t *testing.T) {
	data := "# comment\nsleet:100000:65536\nroot:1000000:1000000000\n"
	ranges, rootOK := parseSubidFile(data)
	if !rootOK {
		t.Fatal("root map should be recognized")
	}
	if len(ranges) != 2 {
		t.Fatalf("ranges = %d, want 2", len(ranges))
	}

	_, rootOK = parseSubidFile("sleet:100000:65536\n")
	if rootOK {
		t.Fatal("missing root map must not be recognized")
	}

	// A root entry smaller than the minimum is not enough.
	_, rootOK = parseSubidFile("root:1000:1000\n")
	if rootOK {
		t.Fatal("undersized root map must not be recognized")
	}

	// The numeric uid form is accepted too.
	_, rootOK = parseSubidFile("0:1000000:1000000000\n")
	if !rootOK {
		t.Fatal("numeric root uid map should be recognized")
	}
}

func TestEnsureRootSubidEntryAppendsOnlyWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subuid")
	if err := os.WriteFile(path, []byte("sleet:100000:65536\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureRootSubidEntry(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "root:1000000:1000000000") {
		t.Fatalf("root entry missing after append: %q", data)
	}
	before := string(data)

	// Second call must be a no-op.
	if err := ensureRootSubidEntry(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != before {
		t.Fatalf("second call modified the file: %q -> %q", before, after)
	}
}

func TestEnsureRootSubidEntrySkipsOccupiedRanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subuid")
	// Another user already claims the default 1000000 range.
	if err := os.WriteFile(path, []byte("other:1000000:2000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureRootSubidEntry(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "root:3000000:1000000000") {
		t.Fatalf("root entry should start after the occupied range: %q", data)
	}
}

func TestEnsureRootSubidEntryCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subgid")
	if err := ensureRootSubidEntry(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "root:1000000:1000000000") {
		t.Fatalf("unexpected content: %q", data)
	}
}
