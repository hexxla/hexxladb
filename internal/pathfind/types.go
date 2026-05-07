// Package pathfind implements graph traversal algorithms over the hex lattice.
// It supports A* and Dijkstra for edge-connected cell walks.
package pathfind

import "github.com/hexxla/hexxladb/internal/lattice"

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

// UniformCost returns 1.0 for any edge.
func UniformCost(_, _ lattice.Coord) float64 { return 1.0 }
