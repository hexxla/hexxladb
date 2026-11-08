package hexxladb

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexxla/hexxladb/internal/index"
)

func TestMVCCPrunePreservesCutoffStateAndPersistsFloor(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := Open(path, &Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	key, err := Pack(Coord{})
	if err != nil {
		t.Fatal(err)
	}
	deletedKey, err := Pack(Coord{Q: 1})
	if err != nil {
		t.Fatal(err)
	}
	put := func(cellKey PackedCoord, value string) error {
		return db.Update(func(tx *Tx) error {
			return tx.PutCell(t.Context(), CellRecord{Key: cellKey, RawContent: value, Tags: []string{value}})
		})
	}
	if err := db.Update(func(tx *Tx) error {
		if err := tx.PutCell(t.Context(), CellRecord{Key: key, RawContent: "original", Tags: []string{"original"}}); err != nil {
			return err
		}
		return tx.PutCell(t.Context(), CellRecord{Key: deletedKey, RawContent: "removed"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.TagSnapshot("old"); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *Tx) error { return tx.DeleteCell(t.Context(), deletedKey) }); err != nil {
		t.Fatal(err)
	}
	if err := db.TagSnapshot("cutoff"); err != nil {
		t.Fatal(err)
	}
	if err := put(key, "new"); err != nil {
		t.Fatal(err)
	}
	if err := put(deletedKey, "restored"); err != nil {
		t.Fatal(err)
	}
	if count, err := db.PruneCellVersions(2, 10); err != nil || count != 1 {
		t.Fatalf("prune at 2 = %d, %v, want one obsolete version", count, err)
	}

	check := func(db *DB) {
		t.Helper()
		if floor, err := db.OldestSnapshotSeq(); floor != 2 || err != nil {
			t.Fatalf("snapshot floor = %d, %v, want 2", floor, err)
		}
		checkCutoff := func(tx *Tx) error {
			hits, err := tx.QueryCells(t.Context(), CellQuery{RequireTags: []string{"original"}})
			if err != nil || len(hits) != 1 || hits[0].Cell.RawContent != "original" {
				t.Fatalf("cutoff tag query = %v, %v", hits, err)
			}
			if _, found, err := tx.GetCell(deletedKey); found || err != nil {
				t.Fatalf("cutoff tombstone: found=%t err=%v", found, err)
			}
			return nil
		}
		if err := db.ViewAt(2, checkCutoff); err != nil {
			t.Fatal(err)
		}
		if err := db.ViewAtTag("cutoff", checkCutoff); err != nil {
			t.Fatal(err)
		}
		expired := func(*Tx) error { t.Fatal("expired snapshot ran its callback"); return nil }
		for name, err := range map[string]error{
			"seq":  db.ViewAt(1, expired),
			"tag":  db.ViewAtTag("old", expired),
			"time": db.ViewAtTime(time.Unix(0, 0), expired),
		} {
			if !errors.Is(err, ErrSnapshotExpired) {
				t.Fatalf("%s: got %v, want ErrSnapshotExpired", name, err)
			}
		}
	}

	check(db)
	compacted := filepath.Join(t.TempDir(), "compacted.db")
	if err := db.Compact(t.Context(), compacted); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, compacted} {
		copyDB, err := OpenExisting(name, nil)
		if err != nil {
			t.Fatal(err)
		}
		check(copyDB)
		if err := copyDB.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMVCCUnmarkedFileAdoptsCurrentHead(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "legacy-history.db")
	db, err := Open(path, &Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(*Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *Tx) error {
		return tx.deleteDirect([]byte(index.MVCCSnapshotFloorKey))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = OpenExisting(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if floor, err := db.OldestSnapshotSeq(); floor != 2 || err != nil {
		t.Fatalf("legacy floor = %d, %v, want 2", floor, err)
	}
	if err := db.Update(func(*Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = OpenExisting(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if floor, err := db.OldestSnapshotSeq(); floor != 2 || err != nil {
		t.Fatalf("reopened floor = %d, %v, want 2", floor, err)
	}
	if err := db.ViewAt(2, func(*Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
