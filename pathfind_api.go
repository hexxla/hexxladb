package hexxladb

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/pathfind"
	"github.com/hexxla/hexxladb/internal/record"
)

// FindEdgePathConfig configures [Tx.FindEdgePath].
type FindEdgePathConfig struct {
	// Filter restricts traversal to edges whose relationType matches this value.
	// Empty string traverses all edges.
	Filter string
	// MaxExpand limits the number of nodes expanded by the shortest-path search
	// (0 = unlimited).
	MaxExpand int
	// MaxEdgeRows limits the total adjacency rows read across all expansions,
	// including rows excluded by Filter (0 = unlimited).
	MaxEdgeRows int
	// CostFunc overrides edge-weight-based traversal cost.
	// Receives the from and to coordinates; return a negative value to mark an
	// edge impassable. Nil uses the edge's recorded Weight (1.0 when Weight <= 0).
	CostFunc func(from, to Coord) float64
}

// FindEdgePath returns the shortest path from start to goal, following edges.
// Only edges whose relationType matches [FindEdgePathConfig.Filter] are traversed
// (empty = all edges). Edge weights are used as traversal costs unless
// [FindEdgePathConfig.CostFunc] is set. Returns nil if no path exists.
// Returns [ErrSpatialScanLimit] if the expansion or edge-row budget is exhausted.
// Returns [ErrInvalidArgument] if a reachable goal has only paths whose
// accumulated costs overflow float64. On historical snapshots it returns
// [ErrHistoricalDataUnavailable].
func (tx *Tx) FindEdgePath(ctx context.Context, start, goal Coord, cfg FindEdgePathConfig) ([]Coord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := tx.checkCurrentData(); err != nil {
		return nil, err
	}
	if cfg.MaxExpand < 0 || cfg.MaxEdgeRows < 0 {
		return nil, fmt.Errorf("%w: path budgets must be non-negative", ErrInvalidArgument)
	}
	if start == goal {
		return []Coord{start}, nil
	}

	traversal := edgeTraversal{
		tx: tx, ctx: ctx, filter: cfg.Filter, override: cfg.CostFunc,
		weights: make(map[lattice.Coord]float64), maxEdgeRows: cfg.MaxEdgeRows,
	}
	path, pathErr := pathfind.Dijkstra(start, goal, traversal.neighbors, traversal.cost, cfg.MaxExpand)
	if traversal.err != nil {
		return nil, traversal.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pathErr != nil {
		if errors.Is(pathErr, pathfind.ErrExpansionLimit) {
			return nil, fmt.Errorf("%w: path expansions exceed %d", ErrSpatialScanLimit, cfg.MaxExpand)
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, pathErr)
	}
	return path, nil
}

// WalkEdges performs a breadth-first traversal from start following edges,
// returning all reachable coordinates up to maxHops hops and maxNodes total.
// Only edges whose relationType matches filter are followed (empty = all).
// On historical snapshots it returns [ErrHistoricalDataUnavailable].
func (tx *Tx) WalkEdges(ctx context.Context, start Coord, filter string, maxHops, maxNodes int) ([]Coord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if err := tx.checkCurrentData(); err != nil {
		return nil, err
	}
	var result []Coord
	err := walkEdgeCoords(ctx, start, maxHops, matchExactRelation(filter), func(from PackedCoord, fn func(EdgeWalkRecord) bool) error {
		return tx.ascendEdgesFrom(from, "", ctx.Err, fn)
	}, func(coord Coord) bool {
		result = append(result, coord)
		return maxNodes <= 0 || len(result) < maxNodes
	})
	return result, err
}

func walkEdgeCoords(ctx context.Context, start Coord, maxHops int, matches func(string) bool,
	ascend func(PackedCoord, func(EdgeWalkRecord) bool) error, visit func(Coord) bool,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validatePackedRadius(start, 0); err != nil {
		return err
	}
	err := pathfind.VisitBFS(start, func(from Coord, yield func(Coord) bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		packed, err := lattice.Pack(from)
		if err != nil {
			return err
		}
		var coordErr error
		scanErr := ascend(packed, func(edge EdgeWalkRecord) bool {
			if matches != nil && !matches(edge.RelationType) {
				return true
			}
			to, err := lattice.Unpack(edge.To)
			if err != nil {
				coordErr = err
				return false
			}
			return yield(to)
		})
		return cmp.Or(scanErr, coordErr)
	}, maxHops, visit)
	return cmp.Or(err, ctx.Err())
}

// WalkEdgeCoords performs BFS from start following edges matching filter,
// up to maxHops depth and maxCoords total. It is the context-assembly-compatible
// spelling of [Tx.WalkEdges].
func (tx *Tx) WalkEdgeCoords(ctx context.Context, start Coord, filter string, maxHops, maxCoords int) ([]Coord, error) {
	return tx.WalkEdges(ctx, start, filter, maxHops, maxCoords)
}

func matchExactRelation(filter string) func(string) bool {
	if filter == "" {
		return nil
	}
	return func(relation string) bool { return relation == filter }
}

// edgeTraversal keeps the current expansion's weighted adjacency list.
// The search consumes all neighbor costs before expanding the next coordinate.
type edgeTraversal struct {
	tx          *Tx
	ctx         context.Context
	filter      string
	override    func(from, to Coord) float64
	from        lattice.Coord
	loaded      bool
	weights     map[lattice.Coord]float64
	order       []lattice.Coord
	err         error
	edgeRows    int
	maxEdgeRows int
}

func (t *edgeTraversal) beforeEdge() error {
	if err := t.ctx.Err(); err != nil {
		return err
	}
	if t.maxEdgeRows > 0 {
		if t.edgeRows == t.maxEdgeRows {
			return fmt.Errorf("%w: path edge rows exceed %d", ErrSpatialScanLimit, t.maxEdgeRows)
		}
		t.edgeRows++
	}
	return nil
}

func (t *edgeTraversal) neighbors(from lattice.Coord) []lattice.Coord {
	t.load(from)
	if t.err != nil {
		return nil
	}
	return t.order
}

func (t *edgeTraversal) cost(from, to lattice.Coord) float64 {
	t.load(from)
	if t.err != nil {
		return -1
	}
	weight, ok := t.weights[to]
	if !ok {
		return -1
	}
	if t.override != nil {
		value := t.override(from, to)
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.err = fmt.Errorf("%w: path cost must be finite", ErrInvalidArgument)
			return -1
		}
		return value
	}
	return weight
}

func (t *edgeTraversal) load(from lattice.Coord) {
	if t.err != nil {
		return
	}
	if t.loaded && t.from == from {
		return
	}
	if err := t.ctx.Err(); err != nil {
		t.err = err
		return
	}
	packed, err := lattice.Pack(from)
	if err != nil {
		t.err = err
		return
	}

	clear(t.weights)
	t.order = t.order[:0]
	err = t.tx.ascendEdgesFrom(packed, t.filter, t.beforeEdge, func(edge record.EdgeRecord) bool {
		to, unpackErr := lattice.Unpack(edge.To)
		if unpackErr != nil {
			t.err = unpackErr
			return false
		}
		weight := edge.Weight
		if weight <= 0 {
			weight = 1
		}
		previous, duplicate := t.weights[to]
		if !duplicate {
			t.order = append(t.order, to)
		}
		if !duplicate || weight < previous {
			t.weights[to] = weight
		}
		return true
	})
	if err != nil && t.err == nil {
		t.err = err
	}
	if t.err != nil {
		return
	}
	t.from, t.loaded = from, true
}
