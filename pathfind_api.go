package hexxladb

import (
	"context"
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
	// CostFunc overrides edge-weight-based traversal cost.
	// Receives the from and to coordinates; return a negative value to mark an
	// edge impassable. Nil uses the edge's recorded Weight (1.0 when Weight <= 0).
	CostFunc func(from, to Coord) float64
}

// FindEdgePath returns the shortest path from start to goal, following edges.
// Only edges whose relationType matches [FindEdgePathConfig.Filter] are traversed
// (empty = all edges). Edge weights are used as traversal costs unless
// [FindEdgePathConfig.CostFunc] is set. Returns nil if no path exists.
// Returns [ErrInvalidArgument] if a reachable goal has only paths whose
// accumulated costs overflow float64.
func (tx *Tx) FindEdgePath(ctx context.Context, start, goal Coord, cfg FindEdgePathConfig) ([]Coord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if start == goal {
		return []Coord{start}, nil
	}

	traversal := newEdgeTraversal(ctx, tx, matchExactRelation(cfg.Filter), cfg.CostFunc)
	path, pathErr := pathfind.Dijkstra(start, goal, traversal.neighbors, traversal.cost, cfg.MaxExpand)
	if traversal.err != nil {
		return nil, traversal.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pathErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, pathErr)
	}
	return path, nil
}

// WalkEdges performs a breadth-first traversal from start following edges,
// returning all reachable coordinates up to maxHops hops and maxNodes total.
// Only edges whose relationType matches filter are followed (empty = all).
func (tx *Tx) WalkEdges(ctx context.Context, start Coord, filter string, maxHops, maxNodes int) ([]Coord, error) {
	return tx.walkEdges(ctx, start, matchExactRelation(filter), maxHops, maxNodes)
}

func (tx *Tx) walkEdges(ctx context.Context, start Coord, matches func(string) bool, maxHops, maxNodes int) ([]Coord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	traversal := newEdgeTraversal(ctx, tx, matches, nil)
	result := pathfind.BFS(start, traversal.neighbors, maxHops, maxNodes)
	if traversal.err != nil {
		return result, traversal.err
	}
	return result, ctx.Err()
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
	tx       *Tx
	ctx      context.Context
	matches  func(string) bool
	override func(from, to Coord) float64
	from     lattice.Coord
	loaded   bool
	weights  map[lattice.Coord]float64
	order    []lattice.Coord
	err      error
}

func newEdgeTraversal(ctx context.Context, tx *Tx, matches func(string) bool, override func(from, to Coord) float64) *edgeTraversal {
	return &edgeTraversal{
		tx:       tx,
		ctx:      ctx,
		matches:  matches,
		override: override,
		weights:  make(map[lattice.Coord]float64),
	}
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
	err = t.tx.AscendEdgesFrom(packed, func(edge record.EdgeRecord) bool {
		if t.matches != nil && !t.matches(edge.RelationType) {
			return true
		}
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
