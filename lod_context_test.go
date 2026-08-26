package hexxladb_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func openLargeContextTestDB(t *testing.T) *hexxladb.DB {
	t.Helper()
	return openPathfindTestDB(t)
}

// TestLoadContext_LargeRadius verifies that a large radial context load returns
// a non-empty assembled pack over a populated grid.
func TestLoadContext_LargeRadius(t *testing.T) {
	t.Parallel()
	db := openLargeContextTestDB(t)
	ctx := context.Background()

	coords := lattice.WalkRings(nil, lattice.Coord{Q: 0, R: 0}, 6)
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
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{{Q: 0, R: 0}},
			MaxRing:  12,
			MaxCells: 256,
			Assembly: hexxladb.ContextAssemblyConfig{Assemble: hexxladb.DefaultAssembleCellViewOpts()},
		})
		if err != nil {
			return err
		}
		if len(pack.Cells) == 0 {
			t.Fatal("expected cells in large-radius pack")
		}
		t.Logf("large-radius pack: %d cells", len(pack.Cells))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContext_LargeRadiusDoesNotSubstituteCoarsenedCoordinates(t *testing.T) {
	t.Parallel()
	db := openLargeContextTestDB(t)
	ctx := t.Context()

	seed := hexxladb.Coord{Q: 100, R: 100}
	outer := hexxladb.Coord{Q: 110, R: 100}
	unrelated := hexxladb.Coord{Q: 55, R: 50}
	err := db.Update(func(tx *hexxladb.Tx) error {
		for coord, content := range map[hexxladb.Coord]string{
			outer:     "outer",
			unrelated: "unrelated",
		} {
			key, err := hexxladb.Pack(coord)
			if err != nil {
				return err
			}
			if err := tx.PutCell(ctx, hexxladb.CellRecord{Key: key, RawContent: content}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{seed},
			MaxRing:  10,
			MaxCells: 10,
		})
		if err != nil {
			return err
		}
		if len(pack.Cells) != 1 || pack.Cells[0].Coord != outer {
			t.Fatalf("LoadContext cells = %+v, want only outer cell %v", pack.Cells, outer)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestLoadContext_LargeRadiusMaxCellsCap verifies that MaxCells limits results.
func TestLoadContext_LargeRadiusMaxCellsCap(t *testing.T) {
	t.Parallel()
	db := openLargeContextTestDB(t)
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

	const maxCells = 10
	err = db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{{Q: 0, R: 0}},
			MaxRing:  12,
			MaxCells: maxCells,
			Assembly: hexxladb.ContextAssemblyConfig{
				Assemble: hexxladb.DefaultAssembleCellViewOpts(),
			},
		})
		if err != nil {
			return err
		}
		if len(pack.Cells) > maxCells {
			t.Fatalf("expected max %d cells, got %d", maxCells, len(pack.Cells))
		}
		if !pack.Stats.ResultLimitReached {
			t.Fatal("ResultLimitReached = false, want true")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContext_LargeRadiusNilTx(t *testing.T) {
	t.Parallel()
	var tx *hexxladb.Tx
	_, err := tx.LoadContext(context.Background(), hexxladb.LoadContextConfig{
		Seeds: []hexxladb.Coord{{Q: 0, R: 0}}, MaxRing: 12,
	})
	if err == nil {
		t.Fatal("expected error on nil tx")
	}
}
