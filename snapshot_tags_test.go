package hexxladb_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func openSnapTagDB(t *testing.T) *hexxladb.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snap.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return db
}

func putSnapCell(t *testing.T, db *hexxladb.DB, q, r int) {
	t.Helper()
	pk, err := lattice.Pack(lattice.Coord{Q: q, R: r})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{
			Key:        pk,
			RawContent: "cell content",
			Provenance: record.ProvenanceWire{SourceID: "test", Confidence: 1.0},
		})
	}); err != nil {
		t.Fatalf("put cell: %v", err)
	}
}

func countCells(t *testing.T, db *hexxladb.DB, fn func(*hexxladb.Tx) error) {
	t.Helper()
	if err := db.View(fn); err != nil {
		t.Fatalf("view: %v", err)
	}
}

func TestSnapshotTags_TagAndView(t *testing.T) {
	t.Parallel()
	db := openSnapTagDB(t)

	putSnapCell(t, db, 0, 0)
	if err := db.TagSnapshot("after-first"); err != nil {
		t.Fatalf("TagSnapshot: %v", err)
	}
	putSnapCell(t, db, 1, 0)
	putSnapCell(t, db, 2, 0)

	// ViewAtTag should see only 1 cell (snapshot before the 2 later writes).
	var countAtTag int
	if err := db.ViewAtTag("after-first", func(tx *hexxladb.Tx) error {
		cells, err := tx.LoadContext(t.Context(), hexxladb.Coord{}, 10, 100)
		countAtTag = len(cells)
		return err
	}); err != nil {
		t.Fatalf("ViewAtTag: %v", err)
	}

	var countHead int
	countCells(t, db, func(tx *hexxladb.Tx) error {
		cells, err := tx.LoadContext(t.Context(), hexxladb.Coord{}, 10, 100)
		countHead = len(cells)
		return err
	})

	if countAtTag >= countHead {
		t.Errorf("snapshot should see fewer cells: tag=%d head=%d", countAtTag, countHead)
	}
}

func TestSnapshotTags_TwoTags_DifferentSeqs(t *testing.T) {
	t.Parallel()
	db := openSnapTagDB(t)

	putSnapCell(t, db, 0, 0)
	if err := db.TagSnapshot("v1"); err != nil {
		t.Fatalf("TagSnapshot v1: %v", err)
	}
	putSnapCell(t, db, 1, 0)
	putSnapCell(t, db, 2, 0)
	if err := db.TagSnapshot("v2"); err != nil {
		t.Fatalf("TagSnapshot v2: %v", err)
	}

	var c1, c2 int
	if err := db.ViewAtTag("v1", func(tx *hexxladb.Tx) error {
		cells, err := tx.LoadContext(t.Context(), hexxladb.Coord{}, 10, 100)
		c1 = len(cells)
		return err
	}); err != nil {
		t.Fatalf("ViewAtTag v1: %v", err)
	}
	if err := db.ViewAtTag("v2", func(tx *hexxladb.Tx) error {
		cells, err := tx.LoadContext(t.Context(), hexxladb.Coord{}, 10, 100)
		c2 = len(cells)
		return err
	}); err != nil {
		t.Fatalf("ViewAtTag v2: %v", err)
	}

	if c1 >= c2 {
		t.Errorf("v1 should see fewer cells than v2: c1=%d c2=%d", c1, c2)
	}
}

func TestSnapshotTags_NotFound(t *testing.T) {
	t.Parallel()
	db := openSnapTagDB(t)

	err := db.ViewAtTag("nonexistent", func(*hexxladb.Tx) error { return nil })
	if !errors.Is(err, hexxladb.ErrSnapshotTagNotFound) {
		t.Errorf("expected ErrSnapshotTagNotFound, got %v", err)
	}
}

func TestSnapshotTags_List(t *testing.T) {
	t.Parallel()
	db := openSnapTagDB(t)

	putSnapCell(t, db, 0, 0)
	for _, label := range []string{"beta", "alpha", "gamma"} {
		if err := db.TagSnapshot(label); err != nil {
			t.Fatalf("TagSnapshot %s: %v", label, err)
		}
	}

	tags, err := db.ListSnapshotTags()
	if err != nil {
		t.Fatalf("ListSnapshotTags: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tags))
	}
	// Must be sorted by label (B+ tree key order = lexicographic).
	if tags[0].Label != "alpha" || tags[1].Label != "beta" || tags[2].Label != "gamma" {
		t.Errorf("unexpected order: %v", tags)
	}
}

func TestSnapshotTags_Delete(t *testing.T) {
	t.Parallel()
	db := openSnapTagDB(t)

	if err := db.TagSnapshot("to-delete"); err != nil {
		t.Fatalf("TagSnapshot: %v", err)
	}
	if err := db.DeleteSnapshotTag("to-delete"); err != nil {
		t.Fatalf("DeleteSnapshotTag: %v", err)
	}

	// Should be gone.
	err := db.ViewAtTag("to-delete", func(*hexxladb.Tx) error { return nil })
	if !errors.Is(err, hexxladb.ErrSnapshotTagNotFound) {
		t.Errorf("expected not-found after delete, got %v", err)
	}

	// Deleting again should also return not-found.
	err = db.DeleteSnapshotTag("to-delete")
	if !errors.Is(err, hexxladb.ErrSnapshotTagNotFound) {
		t.Errorf("expected not-found on second delete, got %v", err)
	}
}

func TestSnapshotTags_Overwrite(t *testing.T) {
	t.Parallel()
	db := openSnapTagDB(t)

	putSnapCell(t, db, 0, 0)
	if err := db.TagSnapshot("tip"); err != nil {
		t.Fatalf("first TagSnapshot: %v", err)
	}

	tags, _ := db.ListSnapshotTags()
	firstSeq := tags[0].CommitSeq

	putSnapCell(t, db, 1, 0)
	if err := db.TagSnapshot("tip"); err != nil {
		t.Fatalf("second TagSnapshot: %v", err)
	}

	tags, _ = db.ListSnapshotTags()
	if len(tags) != 1 {
		t.Errorf("expected 1 tag after overwrite, got %d", len(tags))
	}
	if tags[0].CommitSeq <= firstSeq {
		t.Errorf("overwritten tag should have higher seq: first=%d updated=%d", firstSeq, tags[0].CommitSeq)
	}
}

func TestSnapshotTags_LabelTooLong(t *testing.T) {
	t.Parallel()
	db := openSnapTagDB(t)

	label := strings.Repeat("x", 201)
	err := db.TagSnapshot(label)
	if !errors.Is(err, hexxladb.ErrSnapshotTagLabelTooLong) {
		t.Errorf("expected ErrSnapshotTagLabelTooLong, got %v", err)
	}
}

func TestSnapshotTags_EmptyLabel(t *testing.T) {
	t.Parallel()
	db := openSnapTagDB(t)

	if err := db.TagSnapshot(""); !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty label, got %v", err)
	}
	if err := db.ViewAtTag("", func(*hexxladb.Tx) error { return nil }); !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty label in ViewAtTag, got %v", err)
	}
	if err := db.DeleteSnapshotTag(""); !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty label in DeleteSnapshotTag, got %v", err)
	}
}

func TestSnapshotTags_ListEmpty(t *testing.T) {
	t.Parallel()
	db := openSnapTagDB(t)

	tags, err := db.ListSnapshotTags()
	if err != nil {
		t.Fatalf("ListSnapshotTags on empty db: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}
}

func TestSnapshotTags_NilCallback(t *testing.T) {
	t.Parallel()
	db := openSnapTagDB(t)

	_ = db.TagSnapshot("x")
	err := db.ViewAtTag("x", nil)
	if !errors.Is(err, hexxladb.ErrNilCallback) {
		t.Errorf("expected ErrNilCallback, got %v", err)
	}
}

func TestSnapshotTags_PersistsAcrossReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.db")

	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	pk, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	_ = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{
			Key:        pk,
			RawContent: "persisted",
			Provenance: record.ProvenanceWire{SourceID: "s", Confidence: 1},
		})
	})
	if err := db.TagSnapshot("persistent"); err != nil {
		t.Fatalf("TagSnapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and verify tag is still there.
	db2, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	tags, err := db2.ListSnapshotTags()
	if err != nil {
		t.Fatalf("ListSnapshotTags after reopen: %v", err)
	}
	if len(tags) != 1 || tags[0].Label != "persistent" {
		t.Errorf("tag not persisted: %v", tags)
	}

	var found bool
	if err := db2.ViewAtTag("persistent", func(tx *hexxladb.Tx) error {
		cells, err := tx.LoadContext(t.Context(), hexxladb.Coord{}, 5, 100)
		found = len(cells) == 1
		return err
	}); err != nil {
		t.Fatalf("ViewAtTag after reopen: %v", err)
	}
	if !found {
		t.Error("cell not visible via persisted tag")
	}

	_ = os.Remove(path)
}
