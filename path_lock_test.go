package hexxladb

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hexxla/hexxladb/internal/engine"
)

func TestOpenPathLockContentionDoesNotCreatePrimary(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "database.db")
	lock, err := engine.AcquirePathLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if db, err := Open(path, nil); !errors.Is(err, ErrDatabaseLocked) {
		if db != nil {
			_ = db.Close()
		}
		t.Fatalf("Open under path lock: %v", err)
	}
	for _, component := range []string{path, path + "-wal", rotationStatePath(path)} {
		if _, err := os.Lstat(component); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("lock contention touched %q: %v", component, err)
		}
	}
}

func TestOpenReusesPathLockInReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permission semantics")
	}
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "database.db")
	db, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(directory, 0o700) }()
	db, err = OpenExisting(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("Open replaced stable path-lock inode")
	}
}
