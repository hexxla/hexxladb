package hexxladb

import (
	"context"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/pathfind"
	"github.com/hexxla/hexxladb/internal/record"
)

// FindEdgePath returns the shortest path from start to goal, following edges.
// Only edges whose relationType matches filter are traversed (empty filter = all edges).
// Edge weights are used as traversal costs. maxExpand limits node expansion (0 = unlimited).
//
// Returns nil if no path exists between start and goal via edges.
func (tx *Tx) FindEdgePath(ctx context.Context, start, goal Coord, filter string, maxExpand int) ([]Coord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if start == goal {
		return []Coord{start}, nil
	}

	neighbors := tx.edgeNeighborFunc(ctx, filter)
	cost := tx.edgeCostFunc(filter)

	path := pathfind.AStar(start, goal, neighbors, cost, pathfind.HexDistanceHeuristic, maxExpand)
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

// LoadContextByEdges walks outward from center following edges (BFS), then
// builds CellRecords for the visited coordinates. Combines graph connectivity
// with spatial locality — cells reachable via edges are preferred over
// blind spatial expansion.
//
// maxHops is the maximum edge traversal depth; maxCells caps the result set.
func (tx *Tx) LoadContextByEdges(ctx context.Context, center Coord, filter string, maxHops, maxCells int) ([]CellRecord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if maxCells <= 0 || maxHops < 0 {
		return nil, ErrInvalidArgument
	}

	coords, err := tx.WalkEdges(ctx, center, filter, maxHops, maxCells)
	if err != nil {
		return nil, err
	}

	out := make([]CellRecord, 0, len(coords))
	for _, c := range coords {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) >= maxCells {
			break
		}
		p, err := lattice.Pack(c)
		if err != nil {
			continue
		}
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, rec)
		}
	}
	return out, nil
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
