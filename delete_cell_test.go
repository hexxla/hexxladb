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

func openDeleteTestDB(t *testing.T, mvcc bool) *hexxladb.DB {
	t.Helper()
	var opts *hexxladb.Options
	if mvcc {
		opts = &hexxladb.Options{EnableMVCC: true}
	}
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "del.db"), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedCellForDelete(t *testing.T, db *hexxladb.DB, c lattice.Coord, content string, tags []string, sourceID string) lattice.PackedCoord {
	t.Helper()
	p, err := lattice.Pack(c)
	if err != nil {
		t.Fatal(err)
	}
	vf := int64(3 * index.WeekNanos)
	rec := record.CellRecord{
		Key:        p,
		RawContent: content,
		Tags:       tags,
		Provenance: record.ProvenanceWire{SourceID: sourceID},
		Validity:   record.ValidityWire{ValidFrom: &vf},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(context.Background(), rec)
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestDeleteCell_v1_primaryAndSecondaries verifies primary key and all
// secondary indexes are removed after deletion on a format-v1 database.
func TestDeleteCell_v1_primaryAndSecondaries(t *testing.T) {
	t.Parallel()
	db := openDeleteTestDB(t, false)
	ctx := context.Background()
	p := seedCellForDelete(t, db, lattice.Coord{Q: 1, R: 2}, "hello", []string{"tag-a"}, "src-a")

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}

	// Primary gone.
	if err := db.View(func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if ok {
			t.Error("cell should be gone after delete")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Source index gone.
	var srcCount int
	if err := db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, "src-a", func(_ record.CellRecord) bool {
			srcCount++
			return true
		})
	}); err != nil {
		t.Fatal(err)
	}
	if srcCount != 0 {
		t.Errorf("source index count=%d, want 0", srcCount)
	}

	// Tag index gone.
	var tagCount int
	if err := db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsByTag(ctx, "tag-a", func(_ record.CellRecord) bool {
			tagCount++
			return true
		})
	}); err != nil {
		t.Fatal(err)
	}
	if tagCount != 0 {
		t.Errorf("tag index count=%d, want 0", tagCount)
	}
}

// TestDeleteCell_idempotent verifies deleting a non-existent cell is a no-op.
func TestDeleteCell_idempotent(t *testing.T) {
	t.Parallel()
	db := openDeleteTestDB(t, false)
	p, err := lattice.Pack(lattice.Coord{Q: 99, R: 99})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(context.Background(), p)
	}); err != nil {
		t.Fatalf("idempotent delete should return nil, got %v", err)
	}
}

func TestDeleteCellWithOutcome_reports_removed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("mvcc_miss_then_hit", func(t *testing.T) {
		t.Parallel()
		db := openDeleteTestDB(t, true)
		ep, err := lattice.Pack(lattice.Coord{Q: 7, R: -2})
		if err != nil {
			t.Fatal(err)
		}
		var emptyHit bool
		if err := db.Update(func(tx *hexxladb.Tx) error {
			var err error
			emptyHit, err = tx.DeleteCellWithOutcome(ctx, ep)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if emptyHit {
			t.Fatal("no visible cell: want removed=false")
		}

		seedCellForDelete(t, db, lattice.Coord{Q: 7, R: -2}, "body", nil, "src")

		var removedLive, repeat bool
		if err := db.Update(func(tx *hexxladb.Tx) error {
			var err error
			removedLive, err = tx.DeleteCellWithOutcome(ctx, ep)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if !removedLive {
			t.Fatal("want removed=true after seeding coord")
		}
		if err := db.Update(func(tx *hexxladb.Tx) error {
			var err error
			repeat, err = tx.DeleteCellWithOutcome(ctx, ep)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if repeat {
			t.Fatal("idempotent tombstone repeat: want removed=false")
		}
	})

	t.Run("v1_hit_then_miss", func(t *testing.T) {
		t.Parallel()
		db := openDeleteTestDB(t, false)
		p := seedCellForDelete(t, db, lattice.Coord{Q: 0, R: 5}, "v1", []string{"t"}, "src")
		var once, twice bool
		if err := db.Update(func(tx *hexxladb.Tx) error {
			var err error
			once, err = tx.DeleteCellWithOutcome(ctx, p)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if !once {
			t.Fatal("want removed=true on first delete")
		}
		if err := db.Update(func(tx *hexxladb.Tx) error {
			var err error
			twice, err = tx.DeleteCellWithOutcome(ctx, p)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if twice {
			t.Fatal("want removed=false on idempotent repeat")
		}
	})
}

// TestDeleteCell_readOnlyTx verifies delete inside View returns ErrTxReadOnly.
func TestDeleteCell_readOnlyTx(t *testing.T) {
	t.Parallel()
	db := openDeleteTestDB(t, false)
	p, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	var gotErr error
	_ = db.View(func(tx *hexxladb.Tx) error {
		gotErr = tx.DeleteCell(context.Background(), p)
		return nil
	})
	if !errors.Is(gotErr, hexxladb.ErrTxReadOnly) {
		t.Fatalf("want ErrTxReadOnly, got %v", gotErr)
	}
}

// TestDeleteCell_withFacets verifies facets are removed on delete (v1).
func TestDeleteCell_withFacets(t *testing.T) {
	t.Parallel()
	db := openDeleteTestDB(t, false)
	ctx := context.Background()
	p := seedCellForDelete(t, db, lattice.Coord{Q: 3, R: 0}, "facet-test", nil, "")

	// Write facets.
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutFacet(record.FacetRecord{Key: p, FacetID: 0, DerivedContent: "f0"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutFacet(record.FacetRecord{Key: p, FacetID: 2, DerivedContent: "f2"})
	}); err != nil {
		t.Fatal(err)
	}

	// Delete cell.
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}

	// Facets should be gone.
	if err := db.View(func(tx *hexxladb.Tx) error {
		for _, fid := range []byte{0, 2} {
			_, ok, err := tx.GetFacet(p, fid)
			if err != nil {
				return err
			}
			if ok {
				t.Errorf("facet %d should be gone", fid)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestDeleteCell_withEdges verifies outbound edges are removed on delete (v1).
func TestDeleteCell_withEdges(t *testing.T) {
	t.Parallel()
	db := openDeleteTestDB(t, false)
	ctx := context.Background()
	c1 := lattice.Coord{Q: 0, R: 0}
	c2 := lattice.Coord{Q: 1, R: 0}
	p1 := seedCellForDelete(t, db, c1, "from", nil, "")
	_ = seedCellForDelete(t, db, c2, "to", nil, "")

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.LinkCells(c1, c2, "related", 1.0, record.ProvenanceWire{})
	}); err != nil {
		t.Fatal(err)
	}

	// Delete the from-cell.
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, p1)
	}); err != nil {
		t.Fatal(err)
	}

	// Outbound edge should be gone.
	var edgeCount int
	if err := db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendEdgesFrom(p1, func(_ record.EdgeRecord) bool {
			edgeCount++
			return true
		})
	}); err != nil {
		t.Fatal(err)
	}
	if edgeCount != 0 {
		t.Errorf("outbound edges count=%d, want 0", edgeCount)
	}
}

// TestDeleteCell_MVCC_tombstoneAndSnapshot verifies tombstone write and
// snapshot isolation on format-v2 databases.
func TestDeleteCell_MVCC_tombstoneAndSnapshot(t *testing.T) {
	t.Parallel()
	db := openDeleteTestDB(t, true)
	ctx := context.Background()
	p := seedCellForDelete(t, db, lattice.Coord{Q: 0, R: 0}, "versioned", []string{"v"}, "src-v")

	// Capture commit_seq before delete.
	stats, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	seqBeforeDelete := stats.CommitSeq

	// Delete.
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}

	// Current snapshot: cell should be not-found.
	if err := db.View(func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if ok {
			t.Error("cell visible in current snapshot after delete")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// ViewAt before delete: cell should be visible.
	if err := db.ViewAt(seqBeforeDelete, func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok {
			t.Error("cell should be visible at snapshot before delete")
		}
		if rec.RawContent != "versioned" {
			t.Errorf("content=%q, want %q", rec.RawContent, "versioned")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestDeleteCell_MVCC_facetTombstone verifies facets are tombstoned under MVCC.
func TestDeleteCell_MVCC_facetTombstone(t *testing.T) {
	t.Parallel()
	db := openDeleteTestDB(t, true)
	ctx := context.Background()
	p := seedCellForDelete(t, db, lattice.Coord{Q: 2, R: -1}, "facet-mvcc", nil, "")

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutFacet(record.FacetRecord{Key: p, FacetID: 1, DerivedContent: "f1"})
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetFacet(p, 1)
		if err != nil {
			return err
		}
		if ok {
			t.Error("facet should be not-found after cell delete (MVCC)")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestDeleteCell_thenReput verifies a coord can be reused after deletion.
func TestDeleteCell_thenReput(t *testing.T) {
	t.Parallel()
	db := openDeleteTestDB(t, true)
	ctx := context.Background()
	c := lattice.Coord{Q: 5, R: 5}
	p := seedCellForDelete(t, db, c, "original", nil, "")

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}

	// Re-put at same coord.
	newRec := record.CellRecord{Key: p, RawContent: "new-content"}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(ctx, newRec)
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("cell should exist after re-put")
		}
		if rec.RawContent != "new-content" {
			t.Errorf("content=%q, want %q", rec.RawContent, "new-content")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestDeleteCell_sameTx_overlay verifies put→delete→get within one tx returns not-found,
// and put→delete→put returns the new value.
func TestDeleteCell_sameTx_overlay(t *testing.T) {
	t.Parallel()
	db := openDeleteTestDB(t, true)
	ctx := context.Background()
	p, err := lattice.Pack(lattice.Coord{Q: 7, R: -3})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		rec := record.CellRecord{Key: p, RawContent: "first"}
		if err := tx.PutCell(ctx, rec); err != nil {
			return err
		}
		// Delete in same tx.
		if err := tx.DeleteCell(ctx, p); err != nil {
			return err
		}
		// Should be not-found.
		_, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if ok {
			t.Error("cell should be not-found after same-tx delete")
		}
		// Re-put in same tx.
		rec2 := record.CellRecord{Key: p, RawContent: "second"}
		if err := tx.PutCell(ctx, rec2); err != nil {
			return err
		}
		got, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok {
			t.Error("cell should exist after same-tx re-put")
		}
		if got.RawContent != "second" {
			t.Errorf("content=%q, want %q", got.RawContent, "second")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestDeleteCell_healthCheckClean verifies HealthCheck reports no issues after delete.
func TestDeleteCell_healthCheckClean(t *testing.T) {
	t.Parallel()
	db := openDeleteTestDB(t, false)
	ctx := context.Background()
	p := seedCellForDelete(t, db, lattice.Coord{Q: 0, R: 0}, "health", []string{"h"}, "src-h")

	// Add a facet and edge to exercise cleanup.
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutFacet(record.FacetRecord{Key: p, FacetID: 0, DerivedContent: "d"}); err != nil {
			return err
		}
		other, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
		return tx.PutEdge(record.EdgeRecord{
			From: p, To: other, RelationType: "r", Weight: 1,
		})
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}

	report, err := db.HealthCheck(ctx, hexxladb.HealthCheckConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if report.CellCount != 0 {
		t.Errorf("CellCount=%d, want 0", report.CellCount)
	}
}
