//go:build linux

package bocker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenExclusiveExportFileRefusesExistingAndSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.tar.gz")

	file, err := openExclusiveExportFile(path)
	if err != nil {
		t.Fatalf("create export file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openExclusiveExportFile(path); err == nil {
		t.Fatal("existing export file unexpectedly accepted")
	}

	link := filepath.Join(dir, "link.tar.gz")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := openExclusiveExportFile(link); err == nil {
		t.Fatal("symlink export path unexpectedly accepted")
	}
}
