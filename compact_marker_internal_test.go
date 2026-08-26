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
