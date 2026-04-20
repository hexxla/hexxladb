package hexxladb_test

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestTx_PutSeam_secondaryIndexes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "seam_sec.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 1, R: 0}
	pa, err := lattice.Pack(a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := lattice.Pack(b)
	if err != nil {
		t.Fatal(err)
	}
	vf := int64(5 * index.WeekNanos)
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	rec := record.SeamRecord{
		ID:               id,
		CellA:            pa,
		CellB:            pb,
		SeamType:         "t",
		Reason:           "",
		ConfidenceDelta:  0,
		DetectedAt:       1,
		ResolutionStatus: "",
		ResolutionNote:   "",
		Validity:         record.ValidityWire{ValidFrom: &vf},
		Provenance:       record.ProvenanceWire{SourceID: "seam-src"},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutSeam(rec) }); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var nSrc, nTime int
	err = db.View(func(tx *hexxladb.Tx) error {
		err := tx.AscendSeamsBySource(ctx, "seam-src", func(r record.SeamRecord) bool {
			if r.ID == id {
				nSrc++
			}
			return true
		})
		if err != nil {
			return err
		}
		bucket, _ := index.WeekBucketFromValidity(rec.Validity)
		return tx.AscendSeamsInTimeBucket(ctx, bucket, func(r record.SeamRecord) bool {
			if r.ID == id {
				nTime++
			}
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if nSrc != 1 || nTime != 1 {
		t.Fatalf("source=%d time=%d", nSrc, nTime)
	}

	// Update source and validity — old keys removed
	rec2 := rec
	rec2.Provenance.SourceID = "seam-other"
	vf2 := int64(12 * index.WeekNanos)
	rec2.Validity.ValidFrom = &vf2
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutSeam(rec2) }); err != nil {
		t.Fatal(err)
	}
	nSrc = 0
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendSeamsBySource(ctx, "seam-src", func(record.SeamRecord) bool {
			nSrc++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if nSrc != 0 {
		t.Fatalf("old source still indexed: %d", nSrc)
	}
	nSrc = 0
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendSeamsBySource(ctx, "seam-other", func(record.SeamRecord) bool {
			nSrc++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if nSrc != 1 {
		t.Fatalf("new source count=%d", nSrc)
	}
}

func TestTx_AscendSeamsBySource_contextCanceled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "seam_sec_ctx.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendSeamsBySource(ctx, "seam-src", func(record.SeamRecord) bool { return true })
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestTx_AscendSeamsBySource_mvccSnapshotIsolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "seam_sec_mvcc.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 1, R: 0}
	pa, err := lattice.Pack(a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := lattice.Pack(b)
	if err != nil {
		t.Fatal(err)
	}
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	rec := record.SeamRecord{
		ID:         id,
		CellA:      pa,
		CellB:      pb,
		SeamType:   "t",
		DetectedAt: 1,
		Provenance: record.ProvenanceWire{SourceID: "old-src"},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutSeam(rec) }); err != nil {
		t.Fatal(err)
	}
	rec.Provenance.SourceID = "new-src"
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutSeam(rec) }); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	countOld := 0
	if err := db.ViewAt(1, func(tx *hexxladb.Tx) error {
		return tx.AscendSeamsBySource(ctx, "old-src", func(record.SeamRecord) bool {
			countOld++
			return true
		})
	}); err != nil {
		t.Fatal(err)
	}
	if countOld != 1 {
		t.Fatalf("expected old source seam in seq=1 snapshot, got %d", countOld)
	}

	countNew := 0
	if err := db.ViewAt(2, func(tx *hexxladb.Tx) error {
		return tx.AscendSeamsBySource(ctx, "new-src", func(record.SeamRecord) bool {
			countNew++
			return true
		})
	}); err != nil {
		t.Fatal(err)
	}
	if countNew != 1 {
		t.Fatalf("expected new source seam in seq=2 snapshot, got %d", countNew)
	}
}
