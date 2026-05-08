package pathfind

import (
	"container/heap"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// AStar finds the shortest path from start to goal using the A* algorithm.
// Returns nil if no path exists. The neighbors function determines graph
// connectivity; the cost function determines edge weights; the heuristic
// estimates remaining distance (must be admissible for optimal results).
//
// maxExpand limits the number of nodes expanded (0 = unlimited).
func AStar(start, goal lattice.Coord, neighbors NeighborFunc, cost CostFunc, heuristic HeuristicFunc, maxExpand int) Path {
	if start == goal {
		return Path{start}
	}

	s := &astarState{
		gScore:   map[lattice.Coord]float64{start: 0},
		cameFrom: map[lattice.Coord]lattice.Coord{},
	}
	heap.Push(&s.open, &pqItem{coord: start, g: 0, f: heuristic(start, goal)})
	expanded := 0

	for s.open.Len() > 0 {
		cur := heap.Pop(&s.open).(*pqItem)
		if cur.coord == goal {
			return reconstructPath(s.cameFrom, goal)
		}
		if maxExpand > 0 && expanded >= maxExpand {
			return nil
		}
		expanded++
		s.expandNode(cur, goal, neighbors, cost, heuristic)
	}
	return nil
}

// astarState holds mutable A* search state.
type astarState struct {
	open     priorityQueue
	gScore   map[lattice.Coord]float64
	cameFrom map[lattice.Coord]lattice.Coord
}

// expandNode processes all neighbors of cur, updating scores and the open set.
func (s *astarState) expandNode(cur *pqItem, goal lattice.Coord, neighbors NeighborFunc, cost CostFunc, heuristic HeuristicFunc) {
	for _, nb := range neighbors(cur.coord) {
		edgeCost := cost(cur.coord, nb)
		if edgeCost < 0 {
			continue
		}
		tentG := cur.g + edgeCost
		if prev, ok := s.gScore[nb]; ok && tentG >= prev {
			continue
		}
		s.gScore[nb] = tentG
		s.cameFrom[nb] = cur.coord
		f := tentG + heuristic(nb, goal)
		heap.Push(&s.open, &pqItem{coord: nb, g: tentG, f: f})
	}
}

func reconstructPath(cameFrom map[lattice.Coord]lattice.Coord, goal lattice.Coord) Path {
	path := Path{goal}
	cur := goal
	for {
		prev, ok := cameFrom[cur]
		if !ok {
			break
		}
		path = append(path, prev)
		cur = prev
	}
	// Reverse in place.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}
