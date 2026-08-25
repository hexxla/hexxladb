package fsutil

import (
	"path/filepath"
	"testing"
)

func TestOpenReadWriteReportsCreation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	file, created, err := OpenReadWrite(path, 0o600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created {
		t.Error("create reported an existing file")
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close created file: %v", err)
	}

	file, created, err = OpenReadWrite(path, 0o600)
	if err != nil {
		t.Fatalf("open existing: %v", err)
	}
	if created {
		t.Error("open existing reported a created file")
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close existing file: %v", err)
	}
}
