package hexxladb

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSuperHexAcquiresDatabaseLockFirst(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "superhex.db"), &Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	idx, err := NewSuperHexSummaryIndex(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Rebuild(t.Context(), db); err != nil {
		t.Fatal(err)
	}

	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"sync", func() error { _, err := idx.Sync(t.Context(), db, 1); return err }},
		{"rebuild", func() error { return idx.Rebuild(t.Context(), db) }},
	} {
		t.Run(op.name, func(t *testing.T) {
			idx.mu.Lock()
			done := make(chan error, 1)
			go func() { done <- op.run() }()
			// While the summary lock is unavailable, the operation must
			// already hold the database lock. The reverse order leaves the
			// database lock free and can deadlock with a transaction callback.
			deadline := time.Now().Add(time.Second)
			acquiredDB := false
			for time.Now().Before(deadline) {
				if !db.mu.TryLock() {
					acquiredDB = true
					break
				}
				db.mu.Unlock()
				runtime.Gosched()
			}
			idx.mu.Unlock()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if !acquiredDB {
				t.Fatal("summary operation waited without taking the database lock")
			}
		})
	}
}
