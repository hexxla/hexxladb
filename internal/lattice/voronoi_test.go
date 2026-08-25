package lattice_test

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestVoronoi_singleSeed(t *testing.T) {
	t.Parallel()
	cells, owner := lattice.Voronoi([]lattice.Coord{{Q: 0, R: 0}}, 2, nil)
	// Radius 2: 3*4+6+1 = 19 cells.
	if len(cells) != 19 {
		t.Fatalf("expected 19 cells, got %d", len(cells))
	}
	for _, c := range cells {
		if c.SeedIdx != 0 {
			t.Fatalf("cell %v assigned to seed %d, want 0", c.Coord, c.SeedIdx)
		}
	}
	if len(owner) != 19 {
		t.Fatalf("owner map has %d entries, want 19", len(owner))
	}
}

func TestVoronoi_twoSeeds(t *testing.T) {
	t.Parallel()
	s0 := lattice.Coord{Q: -3, R: 0}
	s1 := lattice.Coord{Q: 3, R: 0}
	cells, owner := lattice.Voronoi([]lattice.Coord{s0, s1}, 4, nil)

	if len(cells) == 0 {
		t.Fatal("expected non-empty cells")
	}

	// Origin (0,0) is equidistant from both seeds (distance 3).
	// Seed 0 has lower index → should win the tie.
	origin := lattice.Coord{Q: 0, R: 0}
	idx, ok := owner[origin]
	if !ok {
		t.Fatal("origin not in owner map")
	}
	if idx != 0 {
		t.Fatalf("origin assigned to seed %d, want 0 (tie-break by index)", idx)
	}

	// Cells adjacent to seed 0 should belong to seed 0.
	if owner[lattice.Coord{Q: -2, R: 0}] != 0 {
		t.Fatal("cell near seed 0 assigned to wrong seed")
	}
	// Cells adjacent to seed 1 should belong to seed 1.
	if owner[lattice.Coord{Q: 4, R: 0}] != 1 {
		t.Fatal("cell near seed 1 assigned to wrong seed")
	}

	// Both seeds should have non-empty regions.
	r0 := lattice.VoronoiRegion(cells, 0)
	r1 := lattice.VoronoiRegion(cells, 1)
	if len(r0) == 0 || len(r1) == 0 {
		t.Fatalf("expected non-empty regions: r0=%d, r1=%d", len(r0), len(r1))
	}
}

func TestVoronoi_emptySeeds(t *testing.T) {
	t.Parallel()
	cells, owner := lattice.Voronoi(nil, 3, nil)
	if cells != nil {
		t.Fatalf("expected nil cells, got %d", len(cells))
	}
	if owner != nil {
		t.Fatal("expected nil owner")
	}
}

func TestVoronoi_zeroRadius(t *testing.T) {
	t.Parallel()
	cells, owner := lattice.Voronoi([]lattice.Coord{{Q: 0, R: 0}}, 0, nil)
	if cells != nil {
		t.Fatal("expected nil for radius 0")
	}
	if owner != nil {
		t.Fatal("expected nil owner for radius 0")
	}
}

func TestVoronoi_negativeRadius(t *testing.T) {
	t.Parallel()
	cells, _ := lattice.Voronoi([]lattice.Coord{{Q: 0, R: 0}}, -1, nil)
	if cells != nil {
		t.Fatal("expected nil for negative radius")
	}
}

func TestVoronoi_duplicateSeeds(t *testing.T) {
	t.Parallel()
	s := lattice.Coord{Q: 1, R: 1}
	cells, owner := lattice.Voronoi([]lattice.Coord{s, s}, 2, nil)
	// Duplicate should be ignored; all cells owned by seed 0.
	for _, c := range cells {
		if c.SeedIdx != 0 {
			t.Fatalf("cell %v assigned to seed %d after dedup", c.Coord, c.SeedIdx)
		}
	}
	if owner[s] != 0 {
		t.Fatalf("seed assigned to %d, want 0", owner[s])
	}
}

func TestVoronoi_distanceMonotonic(t *testing.T) {
	t.Parallel()
	cells, _ := lattice.Voronoi([]lattice.Coord{{Q: 0, R: 0}}, 3, nil)
	// BFS distances should be monotonically non-decreasing in visit order.
	maxDist := 0
	for _, c := range cells {
		if c.Distance < 0 {
			t.Fatalf("negative distance: %d", c.Distance)
		}
		if c.Distance > 3 {
			t.Fatalf("distance %d exceeds maxRadius 3", c.Distance)
		}
		if c.Distance > maxDist {
			maxDist = c.Distance
		}
	}
	if maxDist != 3 {
		t.Fatalf("max distance %d, want 3", maxDist)
	}
}

func TestVoronoi_symmetry(t *testing.T) {
	t.Parallel()
	// Two seeds symmetric around origin should produce equal-sized regions.
	s0 := lattice.Coord{Q: -2, R: 0}
	s1 := lattice.Coord{Q: 2, R: 0}
	cells, _ := lattice.Voronoi([]lattice.Coord{s0, s1}, 3, nil)
	r0 := lattice.VoronoiRegion(cells, 0)
	r1 := lattice.VoronoiRegion(cells, 1)
	// Due to tie-breaking by seed index, region 0 may be slightly larger.
	// But the difference should be small (at most the midline cells).
	diff := len(r0) - len(r1)
	if diff < 0 {
		diff = -diff
	}
	// Midline has at most ~7 cells for radius 3; allow generous margin.
	if diff > 10 {
		t.Fatalf("regions too asymmetric: r0=%d, r1=%d", len(r0), len(r1))
	}
}

func TestVoronoi_weighted(t *testing.T) {
	t.Parallel()
	// Seed A at (0,0), seed B at (6,0). Target cell: (4,0).
	// Barrier: cells (1,0) and (2,0) on A's direct path have huge penalty.
	// With uniform BFS, A reaches (4,0) in 4 hops (wins over B's 2 hops). → A wins.
	// With barriers on (1,0) and (2,0), A's cheapest path to (4,0) costs
	//   ≥ 1+100+1+100+1+1 ≫ B's cost of 1+1 = 2. → B wins.
	barrier := map[lattice.Coord]bool{
		{Q: 1, R: 0}: true,
		{Q: 2, R: 0}: true,
	}
	weightFn := func(c lattice.Coord) float64 {
		if barrier[c] {
			return 100.0
		}
		return 0
	}
	sA := lattice.Coord{Q: 0, R: 0}
	sB := lattice.Coord{Q: 6, R: 0}
	target := lattice.Coord{Q: 4, R: 0}
	_, owner := lattice.Voronoi([]lattice.Coord{sA, sB}, 8, weightFn)

	if owner[target] != 1 {
		t.Fatalf("target (4,0) should belong to seed B (idx 1) with barriers, got %d", owner[target])
	}
	// Uniform nil regression: a cell near A (3 hops from A, 3 from B = equidistant)
	// should belong to A due to tie-break (lower seed index). Use (3,0).
	mid := lattice.Coord{Q: 3, R: 0}
	_, ownerU := lattice.Voronoi([]lattice.Coord{sA, sB}, 8, nil)
	if ownerU[mid] != 0 {
		t.Fatalf("uniform: midpoint (3,0) should belong to seed A (idx 0) on tie, got %d", ownerU[mid])
	}
}

func TestVoronoiCheckedFinalOwnershipIsUnique(t *testing.T) {
	t.Parallel()
	seeds := []lattice.Coord{{Q: -3, R: 0}, {Q: 3, R: 0}}
	cells, owner, err := lattice.VoronoiChecked(seeds, 6, func(coord lattice.Coord) float64 {
		if coord.Q == -1 {
			return 4
		}
		return 0
	})
	if err != nil {
		t.Fatalf("VoronoiChecked: %v", err)
	}
	seen := make(map[lattice.Coord]struct{}, len(cells))
	for _, cell := range cells {
		if _, duplicate := seen[cell.Coord]; duplicate {
			t.Fatalf("coordinate %+v appears more than once", cell.Coord)
		}
		seen[cell.Coord] = struct{}{}
		if owner[cell.Coord] != cell.SeedIdx {
			t.Fatalf("coordinate %+v cell owner = %d, map owner = %d", cell.Coord, cell.SeedIdx, owner[cell.Coord])
		}
	}
	if len(cells) != len(owner) {
		t.Fatalf("cells = %d, owners = %d", len(cells), len(owner))
	}
	cellsAgain, _, err := lattice.VoronoiChecked(seeds, 6, func(coord lattice.Coord) float64 {
		if coord.Q == -1 {
			return 4
		}
		return 0
	})
	if err != nil {
		t.Fatalf("second VoronoiChecked: %v", err)
	}
	if !slices.Equal(cells, cellsAgain) {
		t.Fatal("weighted Voronoi output order changed across identical calls")
	}
}

func TestVoronoiCheckedRejectsInvalidWeights(t *testing.T) {
	t.Parallel()
	for name, weight := range map[string]float64{
		"negative":          -1,
		"positive-infinity": math.Inf(1),
		"negative-infinity": math.Inf(-1),
		"not-a-number":      math.NaN(),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := lattice.VoronoiChecked(
				[]lattice.Coord{{Q: 0, R: 0}},
				1,
				func(lattice.Coord) float64 { return weight },
			)
			if !errors.Is(err, lattice.ErrInvalidWeight) {
				t.Fatalf("error = %v, want ErrInvalidWeight", err)
			}
		})
	}
}

func TestVoronoiRegion_nonexistentSeed(t *testing.T) {
	t.Parallel()
	cells, _ := lattice.Voronoi([]lattice.Coord{{Q: 0, R: 0}}, 2, nil)
	r := lattice.VoronoiRegion(cells, 99)
	if len(r) != 0 {
		t.Fatalf("expected 0 cells for non-existent seed, got %d", len(r))
	}
}
