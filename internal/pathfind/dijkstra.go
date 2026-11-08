package pathfind

import "github.com/hexxla/hexxladb/internal/lattice"

// Dijkstra finds the shortest path from start to goal using uniform-cost search.
// Equivalent to A* with a zero heuristic. Suitable when no good distance estimate
// is available (e.g. traversal over edges with varying weights).
// Returns ErrCostOverflow if all paths to a reachable goal overflow float64.
//
// maxExpand limits the number of nodes expanded (0 = unlimited).
func Dijkstra(start, goal lattice.Coord, neighbors NeighborFunc, cost CostFunc, maxExpand int) (Path, error) {
	return shortestPath(start, goal, neighbors, cost, zeroHeuristic, maxExpand)
}

func zeroHeuristic(_, _ lattice.Coord) float64 { return 0 }

// BFS performs a breadth-first traversal from start, visiting all reachable
// nodes up to maxDepth hops. Returns all visited coordinates in visit order.
// Useful for edge-connected context gathering without a specific goal.
//
// maxNodes caps the total visited set (0 = unlimited).
func BFS(start lattice.Coord, neighbors NeighborFunc, maxDepth, maxNodes int) []lattice.Coord {
	var result []lattice.Coord
	_ = VisitBFS(start, func(c lattice.Coord, yield func(lattice.Coord) bool) error {
		for _, neighbor := range neighbors(c) {
			if !yield(neighbor) {
				break
			}
		}
		return nil
	}, maxDepth, func(c lattice.Coord) bool {
		result = append(result, c)
		return maxNodes <= 0 || len(result) < maxNodes
	})
	return result
}

// VisitBFS streams discovered coordinates in breadth-first order. Returning
// false from visit stops immediately, including during an adjacency scan.
func VisitBFS(start lattice.Coord, neighbors func(lattice.Coord, func(lattice.Coord) bool) error, maxDepth int, visit func(lattice.Coord) bool) error {
	type entry struct {
		coord lattice.Coord
		depth int
	}
	if !visit(start) {
		return nil
	}

	visited := map[lattice.Coord]struct{}{start: {}}
	queue := []entry{{coord: start}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if maxDepth > 0 && cur.depth >= maxDepth {
			continue
		}

		stopped := false
		err := neighbors(cur.coord, func(nb lattice.Coord) bool {
			if _, seen := visited[nb]; seen {
				return true
			}
			visited[nb] = struct{}{}
			if !visit(nb) {
				stopped = true
				return false
			}
			queue = append(queue, entry{coord: nb, depth: cur.depth + 1})
			return true
		})
		if err != nil || stopped {
			return err
		}
	}
	return nil
}
