package lattice

import (
	"container/heap"
	"errors"
	"fmt"
	"math"
)

// ErrInvalidWeight means a Voronoi weight was negative or non-finite.
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

// Voronoi computes a hex-grid Voronoi diagram via multi-source Dijkstra.
// Each hex within maxRadius of any seed is assigned to the seed whose
// cumulative traversal cost (hop count + WeightFunc penalties) is lowest.
// Ties are broken by seed order (lower index wins).
//
// When weightFn is nil, behaviour is identical to the previous uniform-BFS
// implementation — every hop costs 1.0 and the result is purely geometric.
//
// Returns the partition as a slice of VoronoiCell in visit order, and
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
	queue          voronoiPQ
	weightFn       WeightFunc
	maxRadius      int
}

// VoronoiChecked is [Voronoi] with validation errors returned to API adapters.
func VoronoiChecked(seeds []Coord, maxRadius int, weightFn WeightFunc) (cells []VoronoiCell, owner map[Coord]int, err error) {
	if len(seeds) == 0 || maxRadius <= 0 {
		return nil, nil, nil
	}

	search := newVoronoiSearch(seeds, maxRadius, weightFn)
	if err := search.run(); err != nil {
		return nil, nil, err
	}
	return search.result()
}

func newVoronoiSearch(seeds []Coord, maxRadius int, weightFn WeightFunc) *voronoiSearch {
	search := &voronoiSearch{
		assignments:    make(map[Coord]voronoiAssignment, 3*maxRadius*maxRadius+3*maxRadius+1),
		discoveryOrder: make([]Coord, 0, 3*maxRadius*maxRadius+3*maxRadius+1),
		weights:        make(map[Coord]float64),
		queue:          make(voronoiPQ, 0, len(seeds)),
		weightFn:       weightFn,
		maxRadius:      maxRadius,
	}
	for i, s := range seeds {
		if _, seen := search.assignments[s]; seen {
			continue
		}
		search.assignments[s] = voronoiAssignment{seedIdx: i}
		search.discoveryOrder = append(search.discoveryOrder, s)
		heap.Push(&search.queue, &voronoiItem{coord: s, seedIdx: i})
	}
	heap.Init(&search.queue)
	return search
}

func (search *voronoiSearch) run() error {
	for search.queue.Len() > 0 {
		cur := heap.Pop(&search.queue).(*voronoiItem)
		assigned := search.assignments[cur.coord]
		if cur.cost != assigned.cost || cur.seedIdx != assigned.seedIdx || cur.hops != assigned.hops {
			continue // stale entry
		}
		if cur.hops >= search.maxRadius {
			continue
		}
		if err := search.expand(cur); err != nil {
			return err
		}
	}
	return nil
}

func (search *voronoiSearch) expand(cur *voronoiItem) error {
	for _, neighbor := range cur.coord.Neighbors() {
		penalty, err := search.penalty(neighbor)
		if err != nil {
			return err
		}
		newCost := cur.cost + 1.0 + penalty
		newHops := cur.hops + 1
		previous, seen := search.assignments[neighbor]
		if seen && !betterVoronoiAssignment(newCost, cur.seedIdx, newHops, previous.cost, previous.seedIdx, previous.hops) {
			continue
		}
		if !seen {
			search.discoveryOrder = append(search.discoveryOrder, neighbor)
		}
		search.assignments[neighbor] = voronoiAssignment{cost: newCost, seedIdx: cur.seedIdx, hops: newHops}
		heap.Push(&search.queue, &voronoiItem{coord: neighbor, seedIdx: cur.seedIdx, cost: newCost, hops: newHops})
	}
	return nil
}

func (search *voronoiSearch) penalty(coord Coord) (float64, error) {
	if penalty, ok := search.weights[coord]; ok {
		return penalty, nil
	}
	var penalty float64
	if search.weightFn != nil {
		penalty = search.weightFn(coord)
	}
	if penalty < 0 || math.IsNaN(penalty) || math.IsInf(penalty, 0) {
		return 0, fmt.Errorf("%w at coordinate %+v: %v", ErrInvalidWeight, coord, penalty)
	}
	search.weights[coord] = penalty
	return penalty, nil
}

func (search *voronoiSearch) result() (cells []VoronoiCell, owner map[Coord]int, err error) {
	owner = make(map[Coord]int, len(search.assignments))
	cells = make([]VoronoiCell, 0, len(search.assignments))
	for _, coord := range search.discoveryOrder {
		assigned := search.assignments[coord]
		owner[coord] = assigned.seedIdx
		cells = append(cells, VoronoiCell{
			Coord:    coord,
			SeedIdx:  assigned.seedIdx,
			Distance: assigned.hops,
		})
	}
	return cells, owner, nil
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

// voronoiItem is a priority queue entry for weighted Voronoi.
type voronoiItem struct {
	coord   Coord
	seedIdx int
	cost    float64
	hops    int
	index   int
}

// voronoiPQ implements heap.Interface (min-heap by cost, tie-break by seedIdx).
type voronoiPQ []*voronoiItem

func (pq voronoiPQ) Len() int      { return len(pq) }
func (pq voronoiPQ) Swap(i, j int) { pq[i], pq[j] = pq[j], pq[i]; pq[i].index = i; pq[j].index = j }
func (pq voronoiPQ) Less(i, j int) bool {
	if pq[i].cost != pq[j].cost {
		return pq[i].cost < pq[j].cost
	}
	return pq[i].seedIdx < pq[j].seedIdx
}
func (pq *voronoiPQ) Push(x any) {
	item := x.(*voronoiItem)
	item.index = len(*pq)
	*pq = append(*pq, item)
}
func (pq *voronoiPQ) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[:n-1]
	return item
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
