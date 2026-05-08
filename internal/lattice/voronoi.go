package lattice

// VoronoiCell pairs a coordinate with its owning seed index and BFS distance.
type VoronoiCell struct {
	Coord    Coord
	SeedIdx  int
	Distance int
}

// Voronoi computes a hex-grid Voronoi diagram via multi-source BFS.
// Each hex within maxRadius of any seed is assigned to the nearest seed
// (by hop count). Ties are broken by seed order (lower index wins).
//
// Returns the partition as a slice of VoronoiCell in BFS visit order, and
// a map from each coordinate to its owning seed index for fast lookup.
//
// maxRadius bounds the BFS depth from each seed (must be > 0).
// Seeds outside the packable range are silently skipped.
func Voronoi(seeds []Coord, maxRadius int) (cells []VoronoiCell, owner map[Coord]int) {
	if len(seeds) == 0 || maxRadius <= 0 {
		return nil, nil
	}

	owner = make(map[Coord]int, 3*maxRadius*maxRadius+3*maxRadius+1)
	queue := newBFSQueue(seeds, owner)

	for _, e := range queue.entries {
		cells = append(cells, VoronoiCell{Coord: e.coord, SeedIdx: e.seedIdx, Distance: 0})
	}

	cells = processBFS(queue, owner, maxRadius, cells)
	return cells, owner
}

// bfsQueue holds BFS state for multi-source traversal.
type bfsQueue struct {
	entries []bfsEntry
}

type bfsEntry struct {
	coord   Coord
	seedIdx int
	dist    int
}

// newBFSQueue initialises the queue with deduplicated seeds.
func newBFSQueue(seeds []Coord, owner map[Coord]int) bfsQueue {
	q := bfsQueue{entries: make([]bfsEntry, 0, len(seeds))}
	for i, s := range seeds {
		if _, seen := owner[s]; seen {
			continue
		}
		owner[s] = i
		q.entries = append(q.entries, bfsEntry{coord: s, seedIdx: i, dist: 0})
	}
	return q
}

// processBFS runs the BFS loop, expanding neighbors up to maxRadius.
func processBFS(q bfsQueue, owner map[Coord]int, maxRadius int, cells []VoronoiCell) []VoronoiCell {
	head := 0
	for head < len(q.entries) {
		e := q.entries[head]
		head++
		if e.dist >= maxRadius {
			continue
		}
		for _, nb := range e.coord.Neighbors() {
			if _, seen := owner[nb]; seen {
				continue
			}
			owner[nb] = e.seedIdx
			next := bfsEntry{coord: nb, seedIdx: e.seedIdx, dist: e.dist + 1}
			q.entries = append(q.entries, next)
			cells = append(cells, VoronoiCell{Coord: nb, SeedIdx: e.seedIdx, Distance: next.dist})
		}
	}
	return cells
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
