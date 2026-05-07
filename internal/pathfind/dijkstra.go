package pathfind

import "github.com/hexxla/hexxladb/internal/lattice"

// Dijkstra finds the shortest path from start to goal using uniform-cost search.
// Equivalent to A* with a zero heuristic. Suitable when no good distance estimate
// is available (e.g. traversal over edges with varying weights).
//
// maxExpand limits the number of nodes expanded (0 = unlimited).
func Dijkstra(start, goal lattice.Coord, neighbors NeighborFunc, cost CostFunc, maxExpand int) Path {
	return AStar(start, goal, neighbors, cost, zeroHeuristic, maxExpand)
}

func zeroHeuristic(_, _ lattice.Coord) float64 { return 0 }

// BFS performs a breadth-first traversal from start, visiting all reachable
// nodes up to maxDepth hops. Returns all visited coordinates in visit order.
// Useful for edge-connected context gathering without a specific goal.
//
// maxNodes caps the total visited set (0 = unlimited).
func BFS(start lattice.Coord, neighbors NeighborFunc, maxDepth, maxNodes int) []lattice.Coord {
	type entry struct {
		coord lattice.Coord
		depth int
	}

	visited := map[lattice.Coord]struct{}{start: {}}
	queue := []entry{{coord: start, depth: 0}}
	result := []lattice.Coord{start}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if maxDepth > 0 && cur.depth >= maxDepth {
			continue
		}

		for _, nb := range neighbors(cur.coord) {
			if maxNodes > 0 && len(result) >= maxNodes {
				return result
			}
			if _, seen := visited[nb]; seen {
				continue
			}
			visited[nb] = struct{}{}
			result = append(result, nb)
			queue = append(queue, entry{coord: nb, depth: cur.depth + 1})
		}
	}
	return result
}
