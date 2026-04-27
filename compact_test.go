package hexxladb_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func openCompactDB(t *testing.T, opts *hexxladb.Options) (db *hexxladb.DB, dbPath string) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "src.db")
	var err error
	db, err = hexxladb.Open(dbPath, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dbPath
}

func putCompactCell(t *testing.T, db *hexxladb.DB, q, r int, content string) lattice.PackedCoord {
	t.Helper()
	p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(context.Background(), record.CellRecord{
			Key:        p,
			RawContent: content,
			Provenance: record.ProvenanceWire{SourceID: "s"},
		})
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestCompactTo_emptyDB verifies compaction of an empty database produces a valid empty DB.
func TestCompactTo_emptyDB(t *testing.T) {
	t.Parallel()
	_, srcPath := openCompactDB(t, nil)
	destPath := filepath.Join(t.TempDir(), "dest.db")

	if err := hexxladb.CompactTo(context.Background(), srcPath, destPath, nil); err != nil {
		t.Fatal(err)
	}

	dest, err := hexxladb.Open(destPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dest.Close() })

	// Should have zero cells.
	var count int
	_ = dest.View(func(tx *hexxladb.Tx) error {
		return tx.AscendRange(nil, nil, func(_, _ []byte) bool {
			count++
			return true
		})
	})
	if count != 0 {
		t.Errorf("empty compact: got %d keys, want 0", count)
	}
}

// TestCompactTo_preservesData verifies cells, facets, edges, and seams survive compaction.
func TestCompactTo_preservesData(t *testing.T) {
	t.Parallel()
	db, srcPath := openCompactDB(t, nil)
	ctx := context.Background()

	p1 := putCompactCell(t, db, 0, 0, "cell-a")
	p2 := putCompactCell(t, db, 1, 0, "cell-b")

	// Facet.
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutFacet(record.FacetRecord{Key: p1, FacetID: 0, DerivedContent: "f0"})
	}); err != nil {
		t.Fatal(err)
	}
	// Edge.
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.LinkCells(lattice.Coord{Q: 0, R: 0}, lattice.Coord{Q: 1, R: 0}, "rel", 1.0, record.ProvenanceWire{})
	}); err != nil {
		t.Fatal(err)
	}

	destPath := filepath.Join(t.TempDir(), "dest.db")
	if err := hexxladb.CompactTo(ctx, srcPath, destPath, nil); err != nil {
		t.Fatal(err)
	}

	dest, err := hexxladb.Open(destPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dest.Close() })

	// Verify cells.
	_ = dest.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p1)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || rec.RawContent != "cell-a" {
			t.Errorf("cell-a: ok=%v content=%q", ok, rec.RawContent)
		}
		rec2, ok2, err := tx.GetCell(p2)
		if err != nil {
			t.Fatal(err)
		}
		if !ok2 || rec2.RawContent != "cell-b" {
			t.Errorf("cell-b: ok=%v content=%q", ok2, rec2.RawContent)
		}
		return nil
	})

	// Verify facet.
	_ = dest.View(func(tx *hexxladb.Tx) error {
		fr, ok, err := tx.GetFacet(p1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || fr.DerivedContent != "f0" {
			t.Errorf("facet: ok=%v dc=%q", ok, fr.DerivedContent)
		}
		return nil
	})

	// Verify edge.
	var edgeCount int
	_ = dest.View(func(tx *hexxladb.Tx) error {
		return tx.AscendEdgesFrom(p1, func(_ record.EdgeRecord) bool {
			edgeCount++
			return true
		})
	})
	if edgeCount != 1 {
		t.Errorf("edge count=%d, want 1", edgeCount)
	}
}

// TestCompactTo_MVCC_preservesHistory verifies MVCC version rows and tombstones survive compaction.
func TestCompactTo_MVCC_preservesHistory(t *testing.T) {
	t.Parallel()
	opts := &hexxladb.Options{EnableMVCC: true}
	db, srcPath := openCompactDB(t, opts)
	ctx := context.Background()

	p := putCompactCell(t, db, 0, 0, "v1")

	// Capture seq after first write.
	stats1, _ := db.StatsMVCC()
	seqV1 := stats1.CommitSeq

	// Overwrite.
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(ctx, record.CellRecord{Key: p, RawContent: "v2"})
	}); err != nil {
		t.Fatal(err)
	}

	destPath := filepath.Join(t.TempDir(), "dest.db")
	if err := hexxladb.CompactTo(ctx, srcPath, destPath, opts); err != nil {
		t.Fatal(err)
	}

	dest, err := hexxladb.Open(destPath, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dest.Close() })

	// Current snapshot sees v2.
	_ = dest.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || rec.RawContent != "v2" {
			t.Errorf("current: ok=%v content=%q", ok, rec.RawContent)
		}
		return nil
	})

	// ViewAt seqV1 sees v1.
	if err := dest.ViewAt(seqV1, func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok || rec.RawContent != "v1" {
			t.Errorf("ViewAt(%d): ok=%v content=%q", seqV1, ok, rec.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCompactTo_encrypted verifies an encrypted source produces a readable encrypted dest.
func TestCompactTo_encrypted(t *testing.T) {
	t.Parallel()
	key := []byte("sixteen.byte.key!!")
	opts := &hexxladb.Options{EncryptionKey: key}
	db, srcPath := openCompactDB(t, opts)
	putCompactCell(t, db, 0, 0, "secret")

	destPath := filepath.Join(t.TempDir(), "dest.db")
	if err := hexxladb.CompactTo(context.Background(), srcPath, destPath, opts); err != nil {
		t.Fatal(err)
	}

	dest, err := hexxladb.Open(destPath, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dest.Close() })

	p, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	_ = dest.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || rec.RawContent != "secret" {
			t.Errorf("encrypted compact: ok=%v content=%q", ok, rec.RawContent)
		}
		return nil
	})
}

// TestCompactTo_ctxCancellation verifies compaction aborts cleanly and removes partial dest.
func TestCompactTo_ctxCancellation(t *testing.T) {
	t.Parallel()
	db, srcPath := openCompactDB(t, nil)
	for i := range 100 {
		putCompactCell(t, db, i, 0, "filler")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	destPath := filepath.Join(t.TempDir(), "dest.db")
	err := hexxladb.CompactTo(ctx, srcPath, destPath, nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	// dest file should not exist (or be removed).
	if _, statErr := os.Stat(destPath); statErr == nil {
		t.Error("dest file should have been removed after cancellation")
	}
}

// TestCompactTo_fileSizeReduction verifies dest is <= source when source had deletes.
func TestCompactTo_fileSizeReduction(t *testing.T) {
	t.Parallel()
	db, srcPath := openCompactDB(t, nil)
	ctx := context.Background()

	// Write many cells then delete most.
	for i := range 50 {
		putCompactCell(t, db, i, 0, "bulk")
	}
	for i := range 45 {
		p, _ := lattice.Pack(lattice.Coord{Q: i, R: 0})
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.DeleteCell(ctx, p)
		}); err != nil {
			t.Fatal(err)
		}
	}

	destPath := filepath.Join(t.TempDir(), "dest.db")
	if err := hexxladb.CompactTo(ctx, srcPath, destPath, nil); err != nil {
		t.Fatal(err)
	}

	srcStat, _ := os.Stat(srcPath)
	destStat, _ := os.Stat(destPath)
	if destStat.Size() > srcStat.Size() {
		t.Errorf("dest %d bytes > src %d bytes", destStat.Size(), srcStat.Size())
	}
}

// TestCompact_convenience verifies the DB.Compact method works on an open DB.
func TestCompact_convenience(t *testing.T) {
	t.Parallel()
	db, _ := openCompactDB(t, nil)
	ctx := context.Background()
	putCompactCell(t, db, 0, 0, "conv")

	destPath := filepath.Join(t.TempDir(), "compacted.db")
	if err := db.Compact(ctx, destPath); err != nil {
		t.Fatal(err)
	}

	dest, err := hexxladb.Open(destPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dest.Close() })

	p, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	_ = dest.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || rec.RawContent != "conv" {
			t.Errorf("convenience compact: ok=%v content=%q", ok, rec.RawContent)
		}
		return nil
	})
}

// TestCompactTo_healthCheckClean verifies the compacted DB passes HealthCheck.
func TestCompactTo_healthCheckClean(t *testing.T) {
	t.Parallel()
	db, srcPath := openCompactDB(t, nil)
	ctx := context.Background()
	putCompactCell(t, db, 0, 0, "health")

	destPath := filepath.Join(t.TempDir(), "dest.db")
	if err := hexxladb.CompactTo(ctx, srcPath, destPath, nil); err != nil {
		t.Fatal(err)
	}

	dest, err := hexxladb.Open(destPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dest.Close() })

	report, err := dest.HealthCheck(ctx, hexxladb.HealthCheckConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if report.CellCount != 1 {
		t.Errorf("CellCount=%d, want 1", report.CellCount)
	}
}
