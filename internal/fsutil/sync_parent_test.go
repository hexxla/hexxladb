package fsutil

import (
	"path/filepath"
	"testing"
)

func TestSyncParents(t *testing.T) {
	directory := t.TempDir()
	if err := SyncParents(
		filepath.Join(directory, "primary.db"),
		filepath.Join(directory, "primary.db-wal"),
	); err != nil {
		t.Fatalf("SyncParents: %v", err)
	}
}
