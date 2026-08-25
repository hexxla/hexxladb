package hexxladb_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func openFOVTestDB(t *testing.T) *hexxladb.DB {
	t.Helper()
	return openPathfindTestDB(t)
}

func populateFOVCells(t *testing.T, db *hexxladb.DB, maxR int) {
	t.Helper()
	ctx := context.Background()
	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range lattice.WalkRings(nil, lattice.Coord{Q: 0, R: 0}, maxR) {
			p, err := lattice.Pack(c)
			if err != nil {
				continue
			}
			if err := tx.PutCell(ctx, record.CellRecord{
				Key:        p,
				RawContent: fmt.Sprintf("c-%d-%d", c.Q, c.R),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextFOV_noOpaque(t *testing.T) {
	t.Parallel()
	db := openFOVTestDB(t)
	populateFOVCells(t, db, 3)

	err := db.View(func(tx *hexxladb.Tx) error {
		recs, err := tx.LoadContextFOV(context.Background(),
			lattice.Coord{Q: 0, R: 0}, 3,
			func(lattice.Coord) bool { return false },
			hexxladb.FOVContextConfig{})
		if err != nil {
			return err
		}
		// No opaque cells → all 37 cells visible.
		if len(recs) != 37 {
			t.Fatalf("expected 37 cells, got %d", len(recs))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextFOV_withWall(t *testing.T) {
	t.Parallel()
	db := openFOVTestDB(t)
	populateFOVCells(t, db, 3)

	wall := lattice.Coord{Q: 1, R: 0}
	err := db.View(func(tx *hexxladb.Tx) error {
		recs, err := tx.LoadContextFOV(context.Background(),
			lattice.Coord{Q: 0, R: 0}, 3,
			func(c lattice.Coord) bool { return c == wall },
			hexxladb.FOVContextConfig{})
		if err != nil {
			return err
		}
		// Wall blocks some cells → fewer than 37.
		if len(recs) >= 37 {
			t.Fatalf("expected fewer than 37 cells with wall, got %d", len(recs))
		}
		// Should still have origin and wall itself.
		if len(recs) < 2 {
			t.Fatalf("expected at least 2 cells (origin + wall), got %d", len(recs))
		}
		t.Logf("visible with wall: %d / 37", len(recs))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextFOV_maxCells(t *testing.T) {
	t.Parallel()
	db := openFOVTestDB(t)
	populateFOVCells(t, db, 3)

	err := db.View(func(tx *hexxladb.Tx) error {
		recs, err := tx.LoadContextFOV(context.Background(),
			lattice.Coord{Q: 0, R: 0}, 3,
			func(lattice.Coord) bool { return false },
			hexxladb.FOVContextConfig{MaxCells: 5})
		if err != nil {
			return err
		}
		if len(recs) > 5 {
			t.Fatalf("expected max 5 cells, got %d", len(recs))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextFOV_maxCellsDeterministicNearestFirst(t *testing.T) {
	t.Parallel()
	db := openFOVTestDB(t)
	populateFOVCells(t, db, 3)

	var want []lattice.PackedCoord
	for range 20 {
		if err := db.View(func(tx *hexxladb.Tx) error {
			recs, err := tx.LoadContextFOV(t.Context(), lattice.Coord{}, 3,
				func(lattice.Coord) bool { return false },
				hexxladb.FOVContextConfig{MaxCells: 5})
			if err != nil {
				return err
			}
			got := make([]lattice.PackedCoord, len(recs))
			for i := range recs {
				got[i] = recs[i].Key
				coord, unpackErr := lattice.Unpack(recs[i].Key)
				if unpackErr != nil {
					return unpackErr
				}
				if i == 0 && coord != (lattice.Coord{}) {
					t.Fatalf("first capped FOV cell = %v, want origin", coord)
				}
				if coord.Distance(lattice.Coord{}) > 1 {
					t.Fatalf("capped FOV selected distance-%d cell before ring 1 was exhausted", coord.Distance(lattice.Coord{}))
				}
			}
			if want == nil {
				want = got
			} else if !slices.Equal(got, want) {
				t.Fatalf("capped FOV changed between calls: first=%v next=%v", want, got)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadContextFOV_nilTx(t *testing.T) {
	t.Parallel()
	var tx *hexxladb.Tx
	_, err := tx.LoadContextFOV(context.Background(),
		lattice.Coord{}, 3,
		func(lattice.Coord) bool { return false },
		hexxladb.FOVContextConfig{})
	if err == nil {
		t.Fatal("expected error on nil tx")
	}
}

func TestLoadContextFOV_nilOpaque(t *testing.T) {
	t.Parallel()
	db := openFOVTestDB(t)
	err := db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.LoadContextFOV(context.Background(),
			lattice.Coord{}, 3, nil, hexxladb.FOVContextConfig{})
		if err == nil {
			t.Fatal("expected error on nil opaque")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextFOV_negativeRadius(t *testing.T) {
	t.Parallel()
	db := openFOVTestDB(t)
	err := db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.LoadContextFOV(context.Background(),
			lattice.Coord{}, -1,
			func(lattice.Coord) bool { return false },
			hexxladb.FOVContextConfig{})
		if err == nil {
			t.Fatal("expected error on negative radius")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextFOV_rejectsUnboundedWork(t *testing.T) {
	t.Parallel()
	db := openFOVTestDB(t)
	opacityCalls := 0
	err := db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.LoadContextFOV(t.Context(), lattice.Coord{}, 257, func(lattice.Coord) bool {
			opacityCalls++
			return false
		}, hexxladb.FOVContextConfig{})
		return err
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
	if opacityCalls != 0 {
		t.Fatalf("opacity callback called %d times for rejected radius", opacityCalls)
	}
}
