// Package pathfind implements graph traversal algorithms over the hex lattice.
// It supports A* and Dijkstra for edge-connected cell walks.
package pathfind

import (
	"math"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// CostFunc returns the traversal cost from one coordinate to another.
// Return a negative value to indicate an impassable edge.
type CostFunc func(from, to lattice.Coord) float64

// NeighborFunc returns the adjacent coordinates reachable from c.
// For lattice-only traversal this returns hex neighbors; for edge-graph
// traversal it returns coordinates connected by EdgeRecords.
type NeighborFunc func(c lattice.Coord) []lattice.Coord

// HeuristicFunc estimates the remaining cost from current to goal.
// Must be admissible (never overestimate) for A* optimality.
type HeuristicFunc func(current, goal lattice.Coord) float64

// Path is an ordered sequence of coordinates from start to goal (inclusive).
type Path []lattice.Coord

// HexDistanceHeuristic returns the hex distance between two coordinates.
// This is the standard admissible heuristic for hex grids.
func HexDistanceHeuristic(current, goal lattice.Coord) float64 {
	return float64(current.Distance(goal))
}

// EuclideanHeuristic returns the Euclidean distance in the flat axial 2D embedding.
// For pointy-top hex grids the metric is sqrt(dq² + dr² + dq·dr) which equals
// the distance in 2D space when each hex is one unit across.
// This is admissible for edge weights >= 1 and can expand fewer nodes than
// HexDistanceHeuristic on diagonal paths by providing a tighter lower bound.
func EuclideanHeuristic(current, goal lattice.Coord) float64 {
	dq := float64(goal.Q - current.Q)
	dr := float64(goal.R - current.R)
	return math.Sqrt(dq*dq + dr*dr + dq*dr)
}

// UniformCost returns 1.0 for any edge.
func UniformCost(_, _ lattice.Coord) float64 { return 1.0 }
