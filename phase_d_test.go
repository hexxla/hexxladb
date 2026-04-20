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

func TestPhaseD_PutCell_secondaryIndexes(t *testing.T) {
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

	// Change source and validity — old secondary keys removed
	rec2 := rec
	rec2.Provenance.SourceID = "src-b"
	vf2 := int64(10 * index.WeekNanos)
	rec2.Validity.ValidFrom = &vf2
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
}

func TestPhaseD_AscendCellsBySource_contextCanceled(t *testing.T) {
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
