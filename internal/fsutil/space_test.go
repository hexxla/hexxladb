//go:build linux || darwin

package fsutil_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/fsutil"
)

func TestAvailableBytes(t *testing.T) {
	directory := t.TempDir()
	available, known, err := fsutil.AvailableBytes(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !known {
		t.Fatal("available space unexpectedly unknown")
	}
	if available == 0 {
		t.Fatal("available space is zero")
	}
	id, known, err := fsutil.FilesystemID(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !known || id == "" {
		t.Fatalf("filesystem identity: known=%v id=%q", known, id)
	}
}
