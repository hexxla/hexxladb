package lattice

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
)

// ErrInvalidWeight means a Voronoi weight was negative or non-finite, or a
// cumulative traversal cost overflowed.
var ErrInvalidWeight = errors.New("lattice: invalid Voronoi weight")

// WeightFunc returns an additional traversal cost for coord (must be >= 0).
// Return 0 for uniform cost (identical to standard BFS Voronoi behaviour).
// A nil WeightFunc is treated as returning 0 for all coordinates.
type WeightFunc func(coord Coord) float64

// VoronoiCell pairs a coordinate with its owning seed index and BFS distance.
type VoronoiCell struct {
	Coord    Coord
	SeedIdx  int
	Distance int
}

// Voronoi computes a hex-grid Voronoi diagram with a bounded-hop shortest-path search.
// Each hex within maxRadius of any seed is assigned to the seed whose
// cumulative traversal cost (hop count + WeightFunc penalties) is lowest.
// Ties are broken by seed order (lower index wins).
//
// When weightFn is nil, behaviour is identical to the previous uniform-BFS
// implementation — every hop costs 1.0 and the result is purely geometric.
//
// Returns the partition ordered by cost, seed index, hop count and coordinate, and
// a map from each coordinate to its owning seed index for fast lookup.
//
// maxRadius bounds the BFS hop depth from each seed (must be > 0).
func Voronoi(seeds []Coord, maxRadius int, weightFn WeightFunc) (cells []VoronoiCell, owner map[Coord]int) {
	cells, owner, _ = VoronoiChecked(seeds, maxRadius, weightFn)
	return cells, owner
}

type voronoiAssignment struct {
	cost    float64
	seedIdx int
	hops    int
}

type voronoiSearch struct {
	assignments    map[Coord]voronoiAssignment
	discoveryOrder []Coord
	weights        map[Coord]float64
	frontier       []voronoiState
	weightFn       WeightFunc
	maxRadius      int
}

// VoronoiChecked is [Voronoi] with validation errors returned to API adapters.
func VoronoiChecked(seeds []Coord, maxRadius int, weightFn WeightFunc) (cells []VoronoiCell, owner map[Coord]int, err error) {
	cells, err = VoronoiContext(context.Background(), seeds, maxRadius, weightFn)
	if err != nil || cells == nil {
		return nil, nil, err
	}
	owner = make(map[Coord]int, len(cells))
	for _, cell := range cells {
		owner[cell.Coord] = cell.SeedIdx
	}
	return cells, owner, nil
}

// VoronoiContext computes the ordered partition and stops on cancellation.
func VoronoiContext(ctx context.Context, seeds []Coord, maxRadius int, weightFn WeightFunc) ([]VoronoiCell, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(seeds) == 0 || maxRadius <= 0 {
		return nil, nil
	}

	search := newVoronoiSearch(seeds, maxRadius, weightFn)
	if err := search.run(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return search.result(), ctx.Err()
}

func newVoronoiSearch(seeds []Coord, maxRadius int, weightFn WeightFunc) *voronoiSearch {
	search := &voronoiSearch{
		assignments:    make(map[Coord]voronoiAssignment, 3*maxRadius*maxRadius+3*maxRadius+1),
		discoveryOrder: make([]Coord, 0, 3*maxRadius*maxRadius+3*maxRadius+1),
		weights:        make(map[Coord]float64),
		frontier:       make([]voronoiState, 0, len(seeds)),
		weightFn:       weightFn,
		maxRadius:      maxRadius,
	}
	for i, s := range seeds {
		if _, seen := search.assignments[s]; seen {
			continue
		}
		search.assignments[s] = voronoiAssignment{seedIdx: i}
		search.discoveryOrder = append(search.discoveryOrder, s)
		search.frontier = append(search.frontier, voronoiState{coord: s, assignment: voronoiAssignment{seedIdx: i}})
	}
	return search
}

// Every frontier contains paths with the same hop count. A cheaper path found
// in a later frontier cannot erase the earlier path's remaining hop budget.
// A state is expanded only when it improves the best cost/seed already reached
// with at most that many hops. Uniform costs therefore visit each cell once.
// Two rolling frontiers bound memory by the union of the seed disks.
func (search *voronoiSearch) run(ctx context.Context) error {
	for range search.maxRadius {
		next := make([]voronoiState, 0, len(search.frontier))
		positions := make(map[Coord]int, len(search.frontier))
		for _, current := range search.frontier {
			var err error
			next, err = search.expand(ctx, current, next, positions)
			if err != nil {
				return err
			}
		}
		search.frontier = next
		if len(next) == 0 {
			break
		}
	}
	return nil
}

type voronoiState struct {
	coord      Coord
	assignment voronoiAssignment
}

func (search *voronoiSearch) expand(ctx context.Context, current voronoiState, next []voronoiState, positions map[Coord]int) ([]voronoiState, error) {
	for _, neighbor := range current.coord.Neighbors() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		penalty, err := search.penalty(neighbor)
		if err != nil {
			return nil, err
		}
		candidate := voronoiAssignment{
			cost:    current.assignment.cost + 1 + penalty,
			seedIdx: current.assignment.seedIdx,
			hops:    current.assignment.hops + 1,
		}
		if math.IsInf(candidate.cost, 0) {
			return nil, fmt.Errorf("%w: cumulative cost overflow at coordinate %+v", ErrInvalidWeight, neighbor)
		}
		previous, seen := search.assignments[neighbor]
		if seen && !betterVoronoiAssignment(candidate.cost, candidate.seedIdx, candidate.hops, previous.cost, previous.seedIdx, previous.hops) {
			continue
		}
		if !seen {
			search.discoveryOrder = append(search.discoveryOrder, neighbor)
		}
		search.assignments[neighbor] = candidate
		state := voronoiState{coord: neighbor, assignment: candidate}
		if position, found := positions[neighbor]; found {
			next[position] = state
		} else {
			positions[neighbor] = len(next)
			next = append(next, state)
		}
	}
	return next, nil
}

func (search *voronoiSearch) penalty(coord Coord) (float64, error) {
	if search.weightFn == nil {
		return 0, nil
	}
	if penalty, ok := search.weights[coord]; ok {
		return penalty, nil
	}
	penalty := search.weightFn(coord)
	if penalty < 0 || math.IsNaN(penalty) || math.IsInf(penalty, 0) {
		return 0, fmt.Errorf("%w at coordinate %+v: %v", ErrInvalidWeight, coord, penalty)
	}
	search.weights[coord] = penalty
	return penalty, nil
}

func (search *voronoiSearch) result() []VoronoiCell {
	cells := make([]VoronoiCell, 0, len(search.assignments))
	for _, coord := range search.discoveryOrder {
		assigned := search.assignments[coord]
		cells = append(cells, VoronoiCell{
			Coord:    coord,
			SeedIdx:  assigned.seedIdx,
			Distance: assigned.hops,
		})
	}
	slices.SortFunc(cells, func(a, b VoronoiCell) int {
		left, right := search.assignments[a.Coord], search.assignments[b.Coord]
		if betterVoronoiAssignment(left.cost, left.seedIdx, left.hops, right.cost, right.seedIdx, right.hops) {
			return -1
		}
		if betterVoronoiAssignment(right.cost, right.seedIdx, right.hops, left.cost, left.seedIdx, left.hops) {
			return 1
		}
		if a.Coord.Q != b.Coord.Q {
			return cmp.Compare(a.Coord.Q, b.Coord.Q)
		}
		return cmp.Compare(a.Coord.R, b.Coord.R)
	})
	return cells
}

func betterVoronoiAssignment(cost float64, seedIdx, hops int, oldCost float64, oldSeedIdx, oldHops int) bool {
	if cost != oldCost {
		return cost < oldCost
	}
	if seedIdx != oldSeedIdx {
		return seedIdx < oldSeedIdx
	}
	return hops < oldHops
}

// VoronoiRegion returns only the coordinates assigned to a specific seed index.
func VoronoiRegion(cells []VoronoiCell, seedIdx int) []Coord {
	var out []Coord
	for _, c := range cells {
		if c.SeedIdx == seedIdx {
			out = append(out, c.Coord)
		}
	}
	return out
}
