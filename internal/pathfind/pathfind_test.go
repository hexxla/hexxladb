package pathfind_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/pathfind"
)

func hexNeighbors(c lattice.Coord) []lattice.Coord {
	return lattice.Ring(c, 1)
}

func TestAStar_sameStartGoal(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	path := pathfind.AStar(start, start, hexNeighbors, pathfind.UniformCost, pathfind.HexDistanceHeuristic, 0)
	if len(path) != 1 || path[0] != start {
		t.Fatalf("same start/goal: got %v", path)
	}
}

func TestAStar_adjacentCells(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 1, R: 0}
	path := pathfind.AStar(start, goal, hexNeighbors, pathfind.UniformCost, pathfind.HexDistanceHeuristic, 0)
	if len(path) != 2 {
		t.Fatalf("adjacent: expected len=2, got %d: %v", len(path), path)
	}
	if path[0] != start || path[1] != goal {
		t.Fatalf("adjacent: got %v", path)
	}
}

func TestAStar_multiHop(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 3, R: 0}
	path := pathfind.AStar(start, goal, hexNeighbors, pathfind.UniformCost, pathfind.HexDistanceHeuristic, 0)
	if len(path) != 4 {
		t.Fatalf("3-hop: expected len=4, got %d: %v", len(path), path)
	}
	if path[0] != start || path[len(path)-1] != goal {
		t.Fatalf("3-hop: endpoints wrong: %v", path)
	}
	// Verify each step is distance 1.
	for i := 1; i < len(path); i++ {
		if path[i-1].Distance(path[i]) != 1 {
			t.Fatalf("3-hop: step %d not adjacent", i)
		}
	}
}

func TestAStar_maxExpand(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 100, R: 0}
	// With a tight expansion limit, A* should fail to find the path.
	path := pathfind.AStar(start, goal, hexNeighbors, pathfind.UniformCost, pathfind.HexDistanceHeuristic, 5)
	if path != nil {
		t.Fatalf("expected nil with maxExpand=5 for distant goal, got len=%d", len(path))
	}
}

func TestAStar_impassableEdge(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 2, R: 0}
	// Block Q=1,R=0 by returning negative cost.
	blocked := lattice.Coord{Q: 1, R: 0}
	cost := func(from, to lattice.Coord) float64 {
		if to == blocked {
			return -1
		}
		return 1
	}
	path := pathfind.AStar(start, goal, hexNeighbors, cost, pathfind.HexDistanceHeuristic, 0)
	if path == nil {
		t.Fatal("should find path around blocked cell")
	}
	// Path should avoid the blocked cell.
	for _, c := range path {
		if c == blocked {
			t.Fatal("path passes through blocked cell")
		}
	}
	if path[0] != start || path[len(path)-1] != goal {
		t.Fatalf("endpoints wrong: %v", path)
	}
}

func TestDijkstra_basic(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 2, R: -1}
	path := pathfind.Dijkstra(start, goal, hexNeighbors, pathfind.UniformCost, 0)
	dist := start.Distance(goal)
	if len(path) != dist+1 {
		t.Fatalf("Dijkstra: expected len=%d, got %d", dist+1, len(path))
	}
}

func TestBFS_allNeighbors(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	result := pathfind.BFS(start, hexNeighbors, 1, 0)
	// Depth=1 should give center + 6 neighbors = 7.
	if len(result) != 7 {
		t.Fatalf("BFS depth=1: expected 7, got %d", len(result))
	}
	if result[0] != start {
		t.Fatalf("BFS: first should be start")
	}
}

func TestBFS_maxNodes(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	result := pathfind.BFS(start, hexNeighbors, 10, 5)
	if len(result) > 5 {
		t.Fatalf("BFS maxNodes=5: got %d", len(result))
	}
}

func TestBFS_maxDepth(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	result := pathfind.BFS(start, hexNeighbors, 2, 0)
	// Depth=2: center + ring1(6) + ring2(12) = 19.
	if len(result) != 19 {
		t.Fatalf("BFS depth=2: expected 19, got %d", len(result))
	}
}

func TestAStar_restrictedNeighbors(t *testing.T) {
	t.Parallel()
	// Only allow specific edges (simulating EdgeRecord graph).
	edges := map[lattice.Coord][]lattice.Coord{
		{Q: 0, R: 0}: {{Q: 1, R: 0}, {Q: 0, R: 1}},
		{Q: 1, R: 0}: {{Q: 0, R: 0}, {Q: 2, R: 0}},
		{Q: 0, R: 1}: {{Q: 0, R: 0}},
		{Q: 2, R: 0}: {{Q: 1, R: 0}},
	}
	neighbors := func(c lattice.Coord) []lattice.Coord {
		return edges[c]
	}
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 2, R: 0}
	path := pathfind.AStar(start, goal, neighbors, pathfind.UniformCost, pathfind.HexDistanceHeuristic, 0)
	if len(path) != 3 {
		t.Fatalf("restricted graph: expected len=3, got %d: %v", len(path), path)
	}

	// Unreachable goal.
	unreachable := lattice.Coord{Q: 5, R: 5}
	path = pathfind.AStar(start, unreachable, neighbors, pathfind.UniformCost, pathfind.HexDistanceHeuristic, 0)
	if path != nil {
		t.Fatalf("unreachable: expected nil, got %v", path)
	}
}
