//go:build linux

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncParentsMissingParent(t *testing.T) {
	err := SyncParents(filepath.Join(t.TempDir(), "missing", "primary.db"))
	if err == nil {
		t.Fatal("SyncParents succeeded for a missing parent")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SyncParents error = %v, want os.ErrNotExist", err)
	}
}
