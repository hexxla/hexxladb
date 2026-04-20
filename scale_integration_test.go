//go:build integration

package hexxladb_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// TestIntegration_putManyCells_survivesReopen writes a grid of cells (10k by default),
// exercises secondary indexes, reopens the DB, and spot-checks reads. Lattice coordinates
// stay within Pack range (small positive q,r grid).
func TestIntegration_putManyCells_survivesReopen(t *testing.T) {
	t.Parallel()
	nCells := 10_000
	nq, nr := 100, 100
	if testing.Short() {
		nCells = 1_000
		nq, nr = 25, 40
	}
	if nq*nr != nCells {
		t.Fatalf("grid size %d*%d != nCells %d", nq, nr, nCells)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "scale_cells.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	vf := int64(7) * index.WeekNanos
	src := "scale-integration"

	const batch = 500
	for base := 0; base < nCells; base += batch {
		end := base + batch
		if end > nCells {
			end = nCells
		}
		err = db.Update(func(tx *hexxladb.Tx) error {
			for i := base; i < end; i++ {
				q := i % nq
				r := i / nq
				p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
				if err != nil {
					return err
				}
				rec := record.CellRecord{
					Key:        p,
					RawContent: fmt.Sprintf("cell-%d", i),
					Provenance: record.ProvenanceWire{
						SourceID:   src,
						Confidence: 1,
						CreatedAt:  int64(i),
						UpdatedAt:  int64(i),
					},
					Validity: record.ValidityWire{ValidFrom: &vf},
				}
				if err := tx.PutCell(context.Background(), rec); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	var bySource int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsBySource(ctx, src, func(record.CellRecord) bool {
			bySource++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if bySource != nCells {
		t.Fatalf("AscendCellsBySource want %d got %d", nCells, bySource)
	}

	bucket, _ := index.WeekBucketFromValidity(record.ValidityWire{ValidFrom: &vf})
	var byTime int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendCellsInTimeBucket(ctx, bucket, func(record.CellRecord) bool {
			byTime++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if byTime != nCells {
		t.Fatalf("AscendCellsInTimeBucket want %d got %d", nCells, byTime)
	}

	spot := []int{0, nCells / 2, nCells - 1}
	for _, si := range spot {
		q := si % nq
		r := si / nq
		p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
		if err != nil {
			t.Fatal(err)
		}
		err = db.View(func(tx *hexxladb.Tx) error {
			got, ok, err := tx.GetCell(p)
			if err != nil {
				return err
			}
			if !ok || got.RawContent != fmt.Sprintf("cell-%d", si) {
				t.Fatalf("spot si=%d ok=%v content=%q", si, ok, got.RawContent)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	for _, si := range spot {
		q := si % nq
		r := si / nq
		p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
		if err != nil {
			t.Fatal(err)
		}
		err = db2.View(func(tx *hexxladb.Tx) error {
			got, ok, err := tx.GetCell(p)
			if err != nil {
				return err
			}
			if !ok || got.RawContent != fmt.Sprintf("cell-%d", si) {
				t.Fatalf("after reopen si=%d ok=%v content=%q", si, ok, got.RawContent)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
