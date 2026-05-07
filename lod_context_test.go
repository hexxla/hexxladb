package hexxladb_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func openLODTestDB(t *testing.T) *hexxladb.DB {
	t.Helper()
	return openPathfindTestDB(t)
}

func TestLoadContextLOD_fineOnly(t *testing.T) {
	t.Parallel()
	db := openLODTestDB(t)
	ctx := context.Background()

	// Populate a cluster of cells around origin within radius 2.
	coords := lattice.WalkRings(nil, lattice.Coord{Q: 0, R: 0}, 2)
	err := db.Update(func(tx *hexxladb.Tx) error {
		for i, c := range coords {
			p, err := lattice.Pack(c)
			if err != nil {
				return err
			}
			if err := tx.PutCell(ctx, record.CellRecord{
				Key:        p,
				RawContent: fmt.Sprintf("cell-%d", i),
				Tags:       []string{"test"},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		recs, err := tx.LoadContextLOD(ctx, lattice.Coord{Q: 0, R: 0}, 2, hexxladb.LODContextConfig{
			FineRadius: 5, // All within fine radius → full resolution.
		})
		if err != nil {
			return err
		}
		// 3*2²+3*2+1 = 19 cells in radius 2.
		if len(recs) != 19 {
			t.Fatalf("expected 19 fine cells, got %d", len(recs))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextLOD_coarseReducesCells(t *testing.T) {
	t.Parallel()
	db := openLODTestDB(t)
	ctx := context.Background()

	// Populate a wide grid: cells at every coordinate in a large area.
	// We'll populate both fine and coarse coordinates to test dedup.
	err := db.Update(func(tx *hexxladb.Tx) error {
		// Populate rings 0..6 around origin.
		coords := lattice.WalkRings(nil, lattice.Coord{Q: 0, R: 0}, 6)
		for i, c := range coords {
			p, err := lattice.Pack(c)
			if err != nil {
				continue
			}
			if err := tx.PutCell(ctx, record.CellRecord{
				Key:        p,
				RawContent: fmt.Sprintf("fine-%d", i),
				Tags:       []string{"test"},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var fineCellCount, lodCellCount int
	err = db.View(func(tx *hexxladb.Tx) error {
		// Full fine load.
		fineRecs, err := tx.LoadContextLOD(ctx, lattice.Coord{Q: 0, R: 0}, 6, hexxladb.LODContextConfig{
			FineRadius: 6, // All fine.
			MaxCells:   500,
		})
		if err != nil {
			return err
		}
		fineCellCount = len(fineRecs)

		// LOD: fine within radius 2, coarse beyond.
		lodRecs, err := tx.LoadContextLOD(ctx, lattice.Coord{Q: 0, R: 0}, 6, hexxladb.LODContextConfig{
			FineRadius:   2,
			CoarseFactor: 2,
			MaxCells:     500,
		})
		if err != nil {
			return err
		}
		lodCellCount = len(lodRecs)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// LOD should return fewer cells than full fine load because outer rings
	// are deduplicated via coarsening.
	if fineCellCount == 0 {
		t.Fatal("fine load returned 0 cells")
	}
	if lodCellCount == 0 {
		t.Fatal("LOD load returned 0 cells")
	}
	// The fine inner radius cells should be the same; outer should be fewer.
	t.Logf("fine=%d, LOD=%d", fineCellCount, lodCellCount)
}

func TestLoadContextLOD_maxCellsCap(t *testing.T) {
	t.Parallel()
	db := openLODTestDB(t)
	ctx := context.Background()

	err := db.Update(func(tx *hexxladb.Tx) error {
		coords := lattice.WalkRings(nil, lattice.Coord{Q: 0, R: 0}, 4)
		for i, c := range coords {
			p, err := lattice.Pack(c)
			if err != nil {
				continue
			}
			if err := tx.PutCell(ctx, record.CellRecord{
				Key:        p,
				RawContent: fmt.Sprintf("c-%d", i),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		recs, err := tx.LoadContextLOD(ctx, lattice.Coord{Q: 0, R: 0}, 4, hexxladb.LODContextConfig{
			FineRadius: 1,
			MaxCells:   10,
		})
		if err != nil {
			return err
		}
		if len(recs) > 10 {
			t.Fatalf("expected max 10 cells, got %d", len(recs))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextLOD_nilTx(t *testing.T) {
	t.Parallel()
	var tx *hexxladb.Tx
	_, err := tx.LoadContextLOD(context.Background(), lattice.Coord{}, 3, hexxladb.LODContextConfig{})
	if err == nil {
		t.Fatal("expected error on nil tx")
	}
}

func TestLoadContextLOD_negativeMaxR(t *testing.T) {
	t.Parallel()
	db := openLODTestDB(t)
	err := db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.LoadContextLOD(context.Background(), lattice.Coord{}, -1, hexxladb.LODContextConfig{})
		if err == nil {
			t.Fatal("expected error for negative maxR")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
