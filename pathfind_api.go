package hexxladb

import (
	"context"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/pathfind"
	"github.com/hexxla/hexxladb/internal/record"
)

// FindEdgePathConfig configures [Tx.FindEdgePath].
type FindEdgePathConfig struct {
	// Filter restricts traversal to edges whose relationType matches this value.
	// Empty string traverses all edges.
	Filter string
	// MaxExpand limits the number of nodes expanded by A* (0 = unlimited).
	MaxExpand int
	// CostFunc overrides edge-weight-based traversal cost.
	// Receives the from and to coordinates; return a negative value to mark an
	// edge impassable. Nil uses the edge's recorded Weight (1.0 when Weight <= 0).
	CostFunc func(from, to Coord) float64
}

// FindEdgePath returns the shortest path from start to goal, following edges.
// Only edges whose relationType matches [FindEdgePathConfig.Filter] are traversed
// (empty = all edges). Edge weights are used as traversal costs unless
// [FindEdgePathConfig.CostFunc] is set. Returns nil if no path exists.
func (tx *Tx) FindEdgePath(ctx context.Context, start, goal Coord, cfg FindEdgePathConfig) ([]Coord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if start == goal {
		return []Coord{start}, nil
	}

	neighbors := tx.edgeNeighborFunc(ctx, cfg.Filter)
	var cost pathfind.CostFunc
	if cfg.CostFunc != nil {
		cost = cfg.CostFunc
	} else {
		cost = tx.edgeCostFunc(cfg.Filter)
	}

	path := pathfind.AStar(start, goal, neighbors, cost, pathfind.EuclideanHeuristic, cfg.MaxExpand)
	if path == nil {
		return nil, nil
	}
	return path, nil
}

// WalkEdges performs a breadth-first traversal from start following edges,
// returning all reachable coordinates up to maxHops hops and maxNodes total.
// Only edges whose relationType matches filter are followed (empty = all).
func (tx *Tx) WalkEdges(ctx context.Context, start Coord, filter string, maxHops, maxNodes int) ([]Coord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	neighbors := tx.edgeNeighborFunc(ctx, filter)
	result := pathfind.BFS(start, neighbors, maxHops, maxNodes)
	return result, nil
}

// WalkEdgeCoords performs BFS from start following edges matching filter,
// up to maxHops depth and maxCoords total. It satisfies [views.TxEdgeWalker]
// so *Tx can be passed to [views.LoadContext] when EdgeFilter is set.
func (tx *Tx) WalkEdgeCoords(ctx context.Context, start Coord, filter string, maxHops, maxCoords int) ([]Coord, error) {
	return tx.WalkEdges(ctx, start, filter, maxHops, maxCoords)
}

// edgeNeighborFunc returns a NeighborFunc that resolves neighbors via edge records.
func (tx *Tx) edgeNeighborFunc(ctx context.Context, filter string) pathfind.NeighborFunc {
	return func(c lattice.Coord) []lattice.Coord {
		if ctx.Err() != nil {
			return nil
		}
		p, err := lattice.Pack(c)
		if err != nil {
			return nil
		}
		var neighbors []lattice.Coord
		_ = tx.AscendEdgesFrom(p, func(edge record.EdgeRecord) bool {
			if filter != "" && edge.RelationType != filter {
				return true
			}
			coord, err := lattice.Unpack(edge.To)
			if err != nil {
				return true
			}
			neighbors = append(neighbors, coord)
			return true
		})
		return neighbors
	}
}

// edgeCostFunc returns a CostFunc that uses edge weights.
func (tx *Tx) edgeCostFunc(filter string) pathfind.CostFunc {
	return func(from, to lattice.Coord) float64 {
		pf, err := lattice.Pack(from)
		if err != nil {
			return -1
		}
		pt, err := lattice.Pack(to)
		if err != nil {
			return -1
		}
		var weight float64
		found := false
		_ = tx.AscendEdgesFrom(pf, func(edge record.EdgeRecord) bool {
			if edge.To != pt {
				return true
			}
			if filter != "" && edge.RelationType != filter {
				return true
			}
			weight = edge.Weight
			if weight <= 0 {
				weight = 1.0
			}
			found = true
			return false
		})
		if !found {
			return -1
		}
		return weight
	}
}
