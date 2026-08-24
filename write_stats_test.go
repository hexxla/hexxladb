package hexxladb_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestDB_WriteStats(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "write-stats.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}

	callbackErr := errors.New("callback failed")
	if err := db.Update(func(*hexxladb.Tx) error { return callbackErr }); !errors.Is(err, callbackErr) {
		t.Fatalf("failed callback: got %v want %v", err, callbackErr)
	}
	failed := db.WriteStats()
	if failed.Calls != 1 || failed.Commits != 0 {
		t.Fatalf("after failed callback: %#v", failed)
	}
	if failed.Durability != 0 || failed.Finalization != 0 {
		t.Fatalf("failed callback reached commit phases: %#v", failed)
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		p, err := hexxladb.Pack(hexxladb.Coord{Q: 1, R: 2})
		if err != nil {
			return err
		}
		return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: p, RawContent: "committed"})
	}); err != nil {
		t.Fatal(err)
	}
	committed := db.WriteStats()
	if committed.Calls != 2 || committed.Commits != 1 {
		t.Fatalf("after commit: %#v", committed)
	}
	if committed.Callback < failed.Callback || committed.LockWait < failed.LockWait {
		t.Fatalf("cumulative durations regressed: failed=%#v committed=%#v", failed, committed)
	}
	if committed.Durability <= 0 || committed.Finalization < 0 {
		t.Fatalf("missing commit timing: %#v", committed)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if afterClose := db.WriteStats(); afterClose != committed {
		t.Fatalf("stats changed on close: before=%#v after=%#v", committed, afterClose)
	}
}
