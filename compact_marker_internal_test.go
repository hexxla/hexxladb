package hexxladb

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestIncompleteCompactionHeaderFailsClosedUntilFinalized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.db")
	internalOptions := &Options{newIncompleteCompaction: true}
	candidate, err := openDB(path, internalOptions, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path, nil); !errors.Is(err, ErrCompactionIncomplete) {
		t.Fatalf("ordinary open error=%v, want ErrCompactionIncomplete", err)
	}
	candidate, err = openDB(path, internalOptions, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := updateCompactIncompleteMarker(t.Context(), candidate, false); err != nil {
		_ = candidate.Close()
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := opened.StorageStats()
	if err != nil {
		_ = opened.Close()
		t.Fatal(err)
	}
	if storage.ReclaimableBytes != 0 {
		_ = opened.Close()
		t.Fatalf("header marker created reclaimable data pages: %#v", storage)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRefusesUnfinishedOfflineConversion(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "candidate.db")
	db, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(func(tx *Tx) error {
		return tx.putDirect([]byte("__meta/migration/v1-to-v2/checkpoint"), []byte{1})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenExisting(path, nil); !errors.Is(err, ErrMigrationIncomplete) {
		t.Fatalf("OpenExisting = %v, want ErrMigrationIncomplete", err)
	}
}
