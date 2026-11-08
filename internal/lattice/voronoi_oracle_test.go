package lattice_test

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

type oracleVoronoiPath struct {
	cost float64
	seed int
	hops int
}

// boundedVoronoiOracle enumerates every exact path length independently for
// each seed. No dominance pruning or shared multi-source state is used.
func boundedVoronoiOracle(seeds []lattice.Coord, radius int, weights map[lattice.Coord]float64) map[lattice.Coord]oracleVoronoiPath {
	best := map[lattice.Coord]oracleVoronoiPath{}
	for seed, coord := range seeds {
		layer := map[lattice.Coord]float64{coord: 0}
		for hops := range radius + 1 {
			for point, cost := range layer {
				old, exists := best[point]
				if !exists || cost < old.cost || cost == old.cost && (seed < old.seed || seed == old.seed && hops < old.hops) {
					best[point] = oracleVoronoiPath{cost: cost, seed: seed, hops: hops}
				}
			}
			next := map[lattice.Coord]float64{}
			for point, cost := range layer {
				for _, neighbor := range point.Neighbors() {
					candidate := cost + 1 + weights[neighbor]
					if old, exists := next[neighbor]; !exists || candidate < old {
						next[neighbor] = candidate
					}
				}
			}
			layer = next
		}
	}
	return best
}

func TestVoronoiCheckedHopBudgetOracle(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(17, 2026))
	for trial := range 80 {
		radius := 1 + rng.IntN(5)
		seeds := make([]lattice.Coord, 1+rng.IntN(4))
		weights := map[lattice.Coord]float64{}
		for i := range seeds {
			seeds[i] = lattice.Coord{Q: rng.IntN(9) - 4, R: rng.IntN(9) - 4}
			for _, coord := range lattice.WalkRings(nil, seeds[i], radius) {
				weights[coord] = float64(rng.IntN(10)) / 2
			}
		}
		if trial == 0 {
			seeds = []lattice.Coord{{}, {Q: 3}}
			radius = 4
			weights = map[lattice.Coord]float64{{R: -1}: 5}
		}
		want := boundedVoronoiOracle(seeds, radius, weights)
		cells, owners, err := lattice.VoronoiChecked(seeds, radius, func(c lattice.Coord) float64 { return weights[c] })
		if err != nil {
			t.Fatal(err)
		}
		if len(cells) != len(want) || len(owners) != len(want) {
			t.Fatalf("trial %d: cells=%d, owners=%d, oracle=%d", trial, len(cells), len(owners), len(want))
		}
		for _, cell := range cells {
			path, exists := want[cell.Coord]
			if !exists || cell.SeedIdx != path.seed || cell.Distance != path.hops || owners[cell.Coord] != path.seed {
				t.Fatalf("trial %d: cell=%+v, oracle=%+v", trial, cell, path)
			}
		}
	}
}

func TestVoronoiCheckedCostOverflow(t *testing.T) {
	t.Parallel()
	_, _, err := lattice.VoronoiChecked([]lattice.Coord{{}}, 2, func(lattice.Coord) float64 { return math.MaxFloat64 })
	if !errors.Is(err, lattice.ErrInvalidWeight) {
		t.Fatalf("error=%v, want ErrInvalidWeight for cumulative overflow", err)
	}
}
