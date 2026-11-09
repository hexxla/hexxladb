package hexxladb_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func openVoronoiTestDB(t *testing.T) *hexxladb.DB {
	t.Helper()
	return openPathfindTestDB(t)
}

func TestLoadContextVoronoi_CancelsDuringPartition(t *testing.T) {
	t.Parallel()
	db := openVoronoiTestDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checks := 0
	err := db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.LoadContextVoronoi(ctx, []hexxladb.Coord{{}}, hexxladb.VoronoiContextConfig{
			MaxRadius: 64,
			WeightFunc: func(hexxladb.Coord) float64 {
				checks++
				cancel()
				return 0
			},
		})
		return err
	})
	if !errors.Is(err, context.Canceled) || errors.Is(err, hexxladb.ErrInvalidArgument) || checks != 1 {
		t.Fatalf("Voronoi returned %v after %d weight checks", err, checks)
	}
}

func TestLoadContextVoronoi_singleSeed(t *testing.T) {
	t.Parallel()
	db := openVoronoiTestDB(t)
	ctx := t.Context()

	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range lattice.WalkRings(nil, lattice.Coord{Q: 0, R: 0}, 3) {
			p, err := lattice.Pack(c)
			if err != nil {
				continue
			}
			if err := tx.PutCell(ctx, record.CellRecord{
				Key:        p,
				RawContent: fmt.Sprintf("c-%d-%d", c.Q, c.R),
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
		result, err := tx.LoadContextVoronoi(ctx, []lattice.Coord{{Q: 0, R: 0}}, hexxladb.VoronoiContextConfig{
			MaxRadius: 3,
		})
		if err != nil {
			return err
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 region, got %d", len(result))
		}
		recs := result[0]
		// Radius 3: 3*9+9+1 = 37 cells.
		if len(recs) != 37 {
			t.Fatalf("expected 37 cells, got %d", len(recs))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextVoronoi_twoSeeds_noOverlap(t *testing.T) {
	t.Parallel()
	db := openVoronoiTestDB(t)
	ctx := t.Context()

	// Populate a wide area.
	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range lattice.WalkRings(nil, lattice.Coord{Q: 0, R: 0}, 6) {
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

	err = db.View(func(tx *hexxladb.Tx) error {
		seeds := []lattice.Coord{{Q: -3, R: 0}, {Q: 3, R: 0}}
		result, err := tx.LoadContextVoronoi(ctx, seeds, hexxladb.VoronoiContextConfig{
			MaxRadius: 4,
		})
		if err != nil {
			return err
		}
		if len(result) < 2 {
			t.Fatalf("expected 2 regions, got %d", len(result))
		}

		// Verify no overlap: collect all coords from both regions.
		seen := make(map[lattice.PackedCoord]int)
		for seedIdx, recs := range result {
			for _, rec := range recs {
				if prev, dup := seen[rec.Key]; dup {
					t.Fatalf("cell %v in both regions %d and %d", rec.Key, prev, seedIdx)
				}
				seen[rec.Key] = seedIdx
			}
		}

		t.Logf("region 0: %d cells, region 1: %d cells", len(result[0]), len(result[1]))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextVoronoi_maxCellsPerSeed(t *testing.T) {
	t.Parallel()
	db := openVoronoiTestDB(t)
	ctx := t.Context()

	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range lattice.WalkRings(nil, lattice.Coord{Q: 0, R: 0}, 4) {
			p, err := lattice.Pack(c)
			if err != nil {
				continue
			}
			if err := tx.PutCell(ctx, record.CellRecord{
				Key:        p,
				RawContent: "x",
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
		result, err := tx.LoadContextVoronoi(ctx, []lattice.Coord{{Q: 0, R: 0}}, hexxladb.VoronoiContextConfig{
			MaxRadius:       4,
			MaxCellsPerSeed: 5,
		})
		if err != nil {
			return err
		}
		if len(result[0]) > 5 {
			t.Fatalf("expected max 5 cells, got %d", len(result[0]))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextVoronoi_emptySeeds(t *testing.T) {
	t.Parallel()
	db := openVoronoiTestDB(t)
	err := db.View(func(tx *hexxladb.Tx) error {
		result, err := tx.LoadContextVoronoi(t.Context(), nil, hexxladb.VoronoiContextConfig{})
		if err != nil {
			return err
		}
		if result != nil {
			t.Fatalf("expected nil result, got %v", result)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextVoronoi_nilTx(t *testing.T) {
	t.Parallel()
	var tx *hexxladb.Tx
	_, err := tx.LoadContextVoronoi(t.Context(), []lattice.Coord{{Q: 0, R: 0}}, hexxladb.VoronoiContextConfig{})
	if err == nil {
		t.Fatal("expected error on nil tx")
	}
}

func TestLoadContextVoronoi_weightFunc(t *testing.T) {
	t.Parallel()
	db := openVoronoiTestDB(t)
	ctx := t.Context()

	// Seeds: A=(0,0), B=(4,0).
	// Cell (2,0) is given high cost so A cannot cheaply claim it.
	// With uniform cost A owns (2,0) (distance 2 < 2 from B=4).
	// With WeightFunc cost 20 on (2,0), A's effective cost = 1+1+20 = 22 > B's = 2, so B claims it.
	seedA := lattice.Coord{Q: 0, R: 0}
	seedB := lattice.Coord{Q: 4, R: 0}
	heavy := lattice.Coord{Q: 1, R: 0}

	coords := []lattice.Coord{seedA, seedB, heavy, {Q: 2, R: 0}, {Q: 3, R: 0}}
	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range coords {
			if err := putCell(tx, c, "x"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		weightFn := func(c lattice.Coord) float64 {
			if c == heavy {
				return 20.0
			}
			return 0
		}
		result, err := tx.LoadContextVoronoi(ctx, []lattice.Coord{seedA, seedB}, hexxladb.VoronoiContextConfig{
			MaxRadius:  4,
			WeightFunc: weightFn,
		})
		if err != nil {
			return err
		}
		// (2,0) should belong to B (index 1), not A (index 0), due to high cost through (1,0).
		target := lattice.Coord{Q: 2, R: 0}
		inA := false
		for _, rec := range result[0] {
			c, _ := lattice.Unpack(rec.Key)
			if c == target {
				inA = true
			}
		}
		if inA {
			t.Fatalf("expected (2,0) to be owned by seed B due to WeightFunc, but it was in seed A's region")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextVoronoiRejectsInvalidWeight(t *testing.T) {
	t.Parallel()
	db := openVoronoiTestDB(t)
	err := db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.LoadContextVoronoi(
			t.Context(),
			[]lattice.Coord{{Q: 0, R: 0}},
			hexxladb.VoronoiContextConfig{
				MaxRadius: 1,
				WeightFunc: func(lattice.Coord) float64 {
					return math.NaN()
				},
			},
		)
		return err
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
}

func TestLoadContextVoronoiRejectsUnboundedWork(t *testing.T) {
	t.Parallel()
	db := openVoronoiTestDB(t)
	tests := map[string]struct {
		seeds []lattice.Coord
		cfg   hexxladb.VoronoiContextConfig
	}{
		"radius": {
			seeds: []lattice.Coord{{Q: 0, R: 0}},
			cfg:   hexxladb.VoronoiContextConfig{MaxRadius: 65},
		},
		"seed-count": {
			seeds: make([]lattice.Coord, 33),
			cfg:   hexxladb.VoronoiContextConfig{MaxRadius: 1},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := db.View(func(tx *hexxladb.Tx) error {
				_, err := tx.LoadContextVoronoi(t.Context(), test.seeds, test.cfg)
				return err
			})
			if !errors.Is(err, hexxladb.ErrInvalidArgument) {
				t.Fatalf("error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestLoadContextVoronoiPreservesHopBudget(t *testing.T) {
	t.Parallel()
	db := openVoronoiTestDB(t)
	target, err := hexxladb.Pack(hexxladb.Coord{R: -4})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: target, RawContent: "reachable in four hops"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		regions, err := tx.LoadContextVoronoi(t.Context(), []hexxladb.Coord{{}, {Q: 3}}, hexxladb.VoronoiContextConfig{
			MaxRadius: 4,
			WeightFunc: func(c hexxladb.Coord) float64 {
				if c == (hexxladb.Coord{R: -1}) {
					return 5
				}
				return 0
			},
		})
		if err != nil {
			return err
		}
		if len(regions[0]) != 1 || regions[0][0].Key != target {
			t.Fatalf("seed zero lost its four-hop target: %v", regions)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextVoronoiRejectsCostOverflow(t *testing.T) {
	t.Parallel()
	db := openVoronoiTestDB(t)
	err := db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.LoadContextVoronoi(t.Context(), []hexxladb.Coord{{}}, hexxladb.VoronoiContextConfig{
			MaxRadius:  2,
			WeightFunc: func(hexxladb.Coord) float64 { return math.MaxFloat64 },
		})
		return err
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("error=%v, want ErrInvalidArgument for accumulated overflow", err)
	}
}
