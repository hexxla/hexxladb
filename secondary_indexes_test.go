package hexxladb_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestPutCell_secondaryIndexes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "d.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	c := lattice.Coord{Q: 0, R: 0}
	p, err := lattice.Pack(c)
	if err != nil {
		t.Fatal(err)
	}
	vf := int64(3 * index.WeekNanos)
	rec := record.CellRecord{
		Key:        p,
		RawContent: "x",
		Provenance: record.ProvenanceWire{SourceID: "src-a"},
		Validity:   record.ValidityWire{ValidFrom: &vf},
		Tags:       []string{"tag-a"},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), rec) }); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var bySource int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, "src-a", func(r record.CellRecord) bool {
			bySource++
			if r.RawContent != "x" {
				t.Errorf("content %q", r.RawContent)
			}
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if bySource != 1 {
		t.Fatalf("AscendCellsBySource count=%d", bySource)
	}
	bucket, _ := index.WeekBucketFromValidity(rec.Validity)
	var byTime int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsInTimeBucket(ctx, bucket, func(r record.CellRecord) bool {
			byTime++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if byTime != 1 {
		t.Fatalf("AscendCellsInTimeBucket count=%d", byTime)
	}
	var byTag int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, "tag-a", func(record.CellRecord) bool {
			byTag++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if byTag != 1 {
		t.Fatalf("AscendCellsByTag count=%d", byTag)
	}

	// Change source, validity, and tags — old secondary keys removed
	rec2 := rec
	rec2.Provenance.SourceID = "src-b"
	vf2 := int64(10 * index.WeekNanos)
	rec2.Validity.ValidFrom = &vf2
	rec2.Tags = []string{"tag-b"}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), rec2) }); err != nil {
		t.Fatal(err)
	}
	bySource = 0
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, "src-a", func(record.CellRecord) bool {
			bySource++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if bySource != 0 {
		t.Fatalf("old source still indexed: %d", bySource)
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, "src-b", func(record.CellRecord) bool {
			bySource++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if bySource != 1 {
		t.Fatalf("new source count=%d", bySource)
	}
	byTag = 0
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, "tag-a", func(record.CellRecord) bool {
			byTag++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if byTag != 0 {
		t.Fatalf("old tag still indexed: %d", byTag)
	}
	byTag = 0
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, "tag-b", func(record.CellRecord) bool {
			byTag++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if byTag != 1 {
		t.Fatalf("new tag count=%d", byTag)
	}
}

func TestTx_Put_mvcc_rejects_raw_cell_key_without_version_suffix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "mvcc_raw_put.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p, err := lattice.Pack(lattice.Coord{Q: 0, R: 1})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.Put(index.CellKey(p), []byte{1})
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("want ErrInvalidArgument, got %v", err)
	}
}

func TestTx_AscendCellsBySource_mvccSnapshotIsolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "cell_src_mvcc.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	c := lattice.Coord{Q: 2, R: -1}
	p, err := lattice.Pack(c)
	if err != nil {
		t.Fatal(err)
	}
	rec := record.CellRecord{
		Key:        p,
		RawContent: "snap",
		Provenance: record.ProvenanceWire{SourceID: "old-src"},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), rec) }); err != nil {
		t.Fatal(err)
	}
	rec.Provenance.SourceID = "new-src"
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), rec) }); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	countOld := 0
	if err := db.ViewAt(1, func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, "old-src", func(record.CellRecord) bool {
			countOld++
			return true
		})
	}); err != nil {
		t.Fatal(err)
	}
	if countOld != 1 {
		t.Fatalf("expected old source cell in seq=1 snapshot, got %d", countOld)
	}

	countNew := 0
	if err := db.ViewAt(2, func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, "new-src", func(record.CellRecord) bool {
			countNew++
			return true
		})
	}); err != nil {
		t.Fatal(err)
	}
	if countNew != 1 {
		t.Fatalf("expected new source cell in seq=2 snapshot, got %d", countNew)
	}
}

func TestTx_AscendCellsByTag_mvccSnapshotIsolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "cell_tag_mvcc.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	c := lattice.Coord{Q: -2, R: 3}
	p, err := lattice.Pack(c)
	if err != nil {
		t.Fatal(err)
	}
	rec := record.CellRecord{
		Key:        p,
		RawContent: "tag-snap",
		Provenance: record.ProvenanceWire{SourceID: "s"},
		Tags:       []string{"alpha"},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), rec) }); err != nil {
		t.Fatal(err)
	}
	rec.Tags = []string{"beta"}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), rec) }); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	countAlpha := 0
	if err := db.ViewAt(1, func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, "alpha", func(record.CellRecord) bool {
			countAlpha++
			return true
		})
	}); err != nil {
		t.Fatal(err)
	}
	if countAlpha != 1 {
		t.Fatalf("expected alpha tag in seq=1 snapshot, got %d", countAlpha)
	}

	countBeta := 0
	if err := db.ViewAt(2, func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, "beta", func(record.CellRecord) bool {
			countBeta++
			return true
		})
	}); err != nil {
		t.Fatal(err)
	}
	if countBeta != 1 {
		t.Fatalf("expected beta tag in seq=2 snapshot, got %d", countBeta)
	}
}

func TestAscendCellsBySource_contextCanceled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "d_ctx.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p, err := lattice.Pack(lattice.Coord{Q: 1, R: 1})
	if err != nil {
		t.Fatal(err)
	}
	rec := record.CellRecord{
		Key:        p,
		RawContent: "x",
		Provenance: record.ProvenanceWire{SourceID: "src-a"},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), rec) }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, "src-a", func(record.CellRecord) bool { return true })
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}
