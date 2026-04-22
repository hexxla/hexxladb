package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
)

func TestLiveSessionSeedAndVerify(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping example DB I/O in -short")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "session.db")
	t.Cleanup(func() {
		_ = os.Remove(p)
		_ = os.Remove(p + "-wal")
	})

	db, err := hexxladb.Open(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	start := time.Now().UTC().Truncate(time.Hour)
	n := min(48, len(sessionScript))
	if n < 8 {
		t.Fatalf("sessionScript too short: %d", len(sessionScript))
	}
	if err := seedSession(context.Background(), db, start, n); err != nil {
		t.Fatal(err)
	}
	if err := verifySession(context.Background(), db, start, n); err != nil {
		t.Fatal(err)
	}
}
