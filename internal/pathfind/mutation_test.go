package pathfind_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/pathfind"
)

// ── astar.go boundary mutants ────────────────────────────────────────────────

// Kills CONDITIONALS_BOUNDARY at astar.go:33 (maxExpand > 0 && expanded >= maxExpand).
func TestAStar_maxExpandExactBoundary(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 5, R: 0}

	// With maxExpand=1, only expand start — goal not found (too far).
	path := pathfind.AStar(start, goal, hexNeighbors, pathfind.UniformCost, pathfind.HexDistanceHeuristic, 1)
	if path != nil {
		t.Fatalf("maxExpand=1 for distance-5 goal should fail, got len=%d", len(path))
	}

	// With maxExpand=0 (unlimited), should find the path.
	path = pathfind.AStar(start, goal, hexNeighbors, pathfind.UniformCost, pathfind.HexDistanceHeuristic, 0)
	if path == nil {
		t.Fatal("maxExpand=0 (unlimited) should find path")
	}
	if len(path) != 6 {
		t.Fatalf("expected len=6, got %d", len(path))
	}
}

// Kills CONDITIONALS_BOUNDARY at astar.go:40 (edgeCost < 0 → <=).
func TestAStar_zeroCostEdge(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 1, R: 0}
	// Zero-cost edge should be traversable (not treated as impassable).
	zeroCost := func(_, _ lattice.Coord) float64 { return 0 }
	path := pathfind.AStar(start, goal, hexNeighbors, zeroCost, pathfind.HexDistanceHeuristic, 0)
	if path == nil {
		t.Fatal("zero-cost edge should be traversable")
	}
	if len(path) != 2 {
		t.Fatalf("expected len=2, got %d", len(path))
	}
}

// Kills CONDITIONALS_BOUNDARY at astar.go:44 (tentG >= prev → >).
func TestAStar_equalCostPath(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 2, R: 0}
	// With uniform cost, all shortest paths have the same cost.
	// Verify that the algorithm still finds a path of optimal length.
	path := pathfind.AStar(start, goal, hexNeighbors, pathfind.UniformCost, pathfind.HexDistanceHeuristic, 0)
	if path == nil {
		t.Fatal("should find path")
	}
	if len(path) != 3 {
		t.Fatalf("expected len=3, got %d", len(path))
	}
}

// Kills CONDITIONALS_BOUNDARY at astar.go:68 (i < j in reverse loop).
func TestAStar_pathReversalCorrectness(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 4, R: 0}
	path := pathfind.AStar(start, goal, hexNeighbors, pathfind.UniformCost, pathfind.HexDistanceHeuristic, 0)
	if len(path) < 2 {
		t.Fatal("should find multi-step path")
	}
	// Verify start comes first, goal comes last.
	if path[0] != start {
		t.Fatalf("path[0]=%v want start=%v", path[0], start)
	}
	if path[len(path)-1] != goal {
		t.Fatalf("path[-1]=%v want goal=%v", path[len(path)-1], goal)
	}
}

// ── dijkstra.go boundary mutants ─────────────────────────────────────────────

// Kills CONDITIONALS_BOUNDARY at dijkstra.go:35 (maxDepth > 0 && cur.depth >= maxDepth).
func TestBFS_maxDepthExact(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	// maxDepth=1: center + ring1 = 7.
	r1 := pathfind.BFS(start, hexNeighbors, 1, 0)
	if len(r1) != 7 {
		t.Fatalf("depth=1: expected 7, got %d", len(r1))
	}
	// maxDepth=0 with maxNodes=0 means unlimited depth — should grow large.
	rUnlimited := pathfind.BFS(start, hexNeighbors, 3, 0)
	// 3 rings: 1 + 6 + 12 + 18 = 37.
	if len(rUnlimited) != 37 {
		t.Fatalf("depth=3: expected 37, got %d", len(rUnlimited))
	}
}

// Kills CONDITIONALS_NEGATION at dijkstra.go:40 (visited check).
func TestBFS_noRevisit(t *testing.T) {
	t.Parallel()
	start := lattice.Coord{Q: 0, R: 0}
	result := pathfind.BFS(start, hexNeighbors, 2, 0)
	// Check no duplicates.
	seen := make(map[lattice.Coord]struct{}, len(result))
	for _, c := range result {
		if _, dup := seen[c]; dup {
			t.Fatalf("duplicate coord %v in BFS result", c)
		}
		seen[c] = struct{}{}
	}
}

// ── pq.go boundary mutants ───────────────────────────────────────────────────

// Kills CONDITIONALS_BOUNDARY at pq.go:17 (Less comparison: f < f → <=).
func TestAStar_priorityQueueOrdering(t *testing.T) {
	t.Parallel()
	// Test that when two paths have different costs, A* picks the cheaper one.
	start := lattice.Coord{Q: 0, R: 0}
	goal := lattice.Coord{Q: 2, R: 0}
	// Make the direct path (Q+1 each step) cheap (cost=1),
	// and diagonal paths expensive (cost=10).
	cost := func(from, to lattice.Coord) float64 {
		if to.Q > from.Q && to.R == from.R {
			return 1.0
		}
		return 10.0
	}
	path := pathfind.AStar(start, goal, hexNeighbors, cost, pathfind.HexDistanceHeuristic, 0)
	if path == nil {
		t.Fatal("should find path")
	}
	// Optimal path should be the direct Q-axis path: (0,0) → (1,0) → (2,0).
	if len(path) != 3 {
		t.Fatalf("expected len=3, got %d: %v", len(path), path)
	}
	for _, c := range path {
		if c.R != 0 {
			t.Fatalf("optimal path should stay on R=0, got %v", c)
		}
	}
}
