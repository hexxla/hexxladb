package hexxladb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/engine"
)

func TestBackupDirectorySyncFailureRemovesPartialFiles(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "source.db"), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	destination := filepath.Join(t.TempDir(), "backup.db")
	files := []backupFile{
		{destPath: destination},
		{destPath: engine.WalPath(destination)},
	}
	wantErr := errors.New("sync failed")
	syncCalls := 0
	err = db.copyBackupFilesWithSync(context.Background(), files, nil, func(...string) error {
		syncCalls++
		if syncCalls == 1 {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("copyBackupFilesWithSync error = %v, want %v", err, wantErr)
	}
	if syncCalls != 2 {
		t.Fatalf("sync calls = %d, want destination and cleanup calls", syncCalls)
	}
	for _, path := range []string{destination, engine.WalPath(destination)} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("partial backup %q still exists: %v", path, err)
		}
	}
}
