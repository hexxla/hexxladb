package lattice_test

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

type oracleVoronoiPath struct {
	cost *big.Rat
	seed int
	hops int
}

// boundedVoronoiOracle enumerates every exact path length independently for
// each seed. No dominance pruning or shared multi-source state is used.
func boundedVoronoiOracle(seeds []lattice.Coord, radius int, weights map[lattice.Coord]float64) map[lattice.Coord]oracleVoronoiPath {
	best := map[lattice.Coord]oracleVoronoiPath{}
	for seed, coord := range seeds {
		layer := map[lattice.Coord]*big.Rat{coord: new(big.Rat)}
		for hops := range radius + 1 {
			for point, cost := range layer {
				old, exists := best[point]
				if !exists || cost.Cmp(old.cost) < 0 || cost.Cmp(old.cost) == 0 && (seed < old.seed || seed == old.seed && hops < old.hops) {
					best[point] = oracleVoronoiPath{cost: cost, seed: seed, hops: hops}
				}
			}
			next := map[lattice.Coord]*big.Rat{}
			for point, cost := range layer {
				for _, neighbor := range point.Neighbors() {
					candidate := new(big.Rat).Add(cost, big.NewRat(1, 1))
					candidate.Add(candidate, new(big.Rat).SetFloat64(weights[neighbor]))
					if old, exists := next[neighbor]; !exists || candidate.Cmp(old) < 0 {
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
				switch trial % 4 {
				case 1:
					weights[coord] /= 5
				case 2:
					weights[coord] = math.Ldexp(weights[coord], rng.IntN(220)-120)
				}
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

func TestVoronoiComparesExactBinaryCosts(t *testing.T) {
	t.Parallel()
	seeds := []lattice.Coord{{Q: -3}, {Q: 3}}
	// To reach the origin in three hops, each seed has one possible route.
	// Moving 2^53 to the first step used to round away the right route's
	// later unit costs, giving the right seed an incorrectly cheaper path.
	for _, rightPenalty := range []float64{0, 1} {
		_, owners, err := lattice.VoronoiChecked(seeds, 3, func(c lattice.Coord) float64 {
			switch c {
			case lattice.Coord{Q: -1}, lattice.Coord{Q: 2}:
				return 1 << 53
			case lattice.Coord{Q: 1}:
				return rightPenalty
			default:
				return 0
			}
		})
		if err != nil || owners[lattice.Coord{}] != 0 {
			t.Fatalf("right penalty %v: owner %v, error %v, want first seed", rightPenalty, owners[lattice.Coord{}], err)
		}
	}
}
