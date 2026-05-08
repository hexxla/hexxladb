package lattice

import "container/heap"

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
	if len(seeds) == 0 || maxRadius <= 0 {
		return nil, nil
	}

	owner = make(map[Coord]int, 3*maxRadius*maxRadius+3*maxRadius+1)
	dist := make(map[Coord]float64, len(owner))

	pq := make(voronoiPQ, 0, len(seeds))
	for i, s := range seeds {
		if _, seen := owner[s]; seen {
			continue
		}
		owner[s] = i
		dist[s] = 0
		cells = append(cells, VoronoiCell{Coord: s, SeedIdx: i, Distance: 0})
		heap.Push(&pq, &voronoiItem{coord: s, seedIdx: i, cost: 0, hops: 0})
	}
	heap.Init(&pq)

	for pq.Len() > 0 {
		cur := heap.Pop(&pq).(*voronoiItem)
		if cur.cost > dist[cur.coord] {
			continue // stale entry
		}
		if cur.hops >= maxRadius {
			continue
		}
		for _, nb := range cur.coord.Neighbors() {
			penalty := 0.0
			if weightFn != nil {
				penalty = weightFn(nb)
			}
			newCost := cur.cost + 1.0 + penalty
			if d, seen := dist[nb]; seen && d <= newCost {
				continue
			}
			owner[nb] = cur.seedIdx
			dist[nb] = newCost
			newHops := cur.hops + 1
			cells = append(cells, VoronoiCell{Coord: nb, SeedIdx: cur.seedIdx, Distance: newHops})
			heap.Push(&pq, &voronoiItem{coord: nb, seedIdx: cur.seedIdx, cost: newCost, hops: newHops})
		}
	}
	return cells, owner
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
