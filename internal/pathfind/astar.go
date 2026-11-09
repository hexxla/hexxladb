package pathfind

import (
	"container/heap"
	"errors"
	"math"
	"slices"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// ErrCostOverflow indicates that the cheapest path has no finite float64 cost.
var ErrCostOverflow = errors.New("pathfind: cumulative path cost overflows float64")

// ErrExpansionLimit indicates that further expansion would exceed the search budget.
var ErrExpansionLimit = errors.New("pathfind: expansion limit reached")

// AStar finds the shortest path from start to goal using the A* algorithm.
// Returns nil if no path with a finite accumulated cost exists. The neighbors
// function determines graph connectivity, the cost function determines edge
// weights, and the heuristic must be admissible for optimal results.
//
// maxExpand limits the number of nodes expanded (0 = unlimited).
func AStar(start, goal lattice.Coord, neighbors NeighborFunc, cost CostFunc, heuristic HeuristicFunc, maxExpand int) Path {
	path, _ := shortestPath(start, goal, neighbors, cost, heuristic, maxExpand)
	return path
}

func shortestPath(start, goal lattice.Coord, neighbors NeighborFunc, cost CostFunc, heuristic HeuristicFunc, maxExpand int) (Path, error) {
	if start == goal {
		return Path{start}, nil
	}

	s := &astarState{
		gScore:   map[lattice.Coord]float64{start: 0},
		cameFrom: map[lattice.Coord]lattice.Coord{},
	}
	heap.Push(&s.open, &pqItem{coord: start, g: 0, f: heuristic(start, goal)})
	expanded := 0

	for s.open.Len() > 0 {
		cur := heap.Pop(&s.open).(*pqItem)
		if best, ok := s.gScore[cur.coord]; !ok || cur.g != best {
			continue
		}
		if cur.coord == goal {
			if math.IsInf(cur.g, 1) {
				return nil, ErrCostOverflow
			}
			return reconstructPath(s.cameFrom, goal), nil
		}
		if maxExpand > 0 && expanded >= maxExpand {
			return nil, ErrExpansionLimit
		}
		expanded++
		s.expandNode(cur, goal, neighbors, cost, heuristic)
	}
	return nil, nil
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
		// Keep overflowed arrivals at +Inf so a reachable goal can be
		// distinguished from an unreachable one. Finite arrivals take priority.
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
	slices.Reverse(path)
	return path
}
