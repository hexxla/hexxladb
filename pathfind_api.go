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

	traversal := newEdgeTraversal(ctx, tx, cfg.Filter, cfg.CostFunc)
	path := pathfind.Dijkstra(start, goal, traversal.neighbors, traversal.cost, cfg.MaxExpand)
	if traversal.err != nil {
		return nil, traversal.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	traversal := newEdgeTraversal(ctx, tx, filter, nil)
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

// edgeTraversal resolves and caches one weighted adjacency list per expanded
// coordinate. The shortest-path search asks for neighbors before asking for
// their costs, so one edge-prefix scan serves the whole expansion instead of
// rescanning once per edge.
type edgeTraversal struct {
	tx       *Tx
	ctx      context.Context
	filter   string
	override func(from, to Coord) float64
	adj      map[lattice.Coord]map[lattice.Coord]float64
	order    map[lattice.Coord][]lattice.Coord
	err      error
}

func newEdgeTraversal(ctx context.Context, tx *Tx, filter string, override func(from, to Coord) float64) *edgeTraversal {
	return &edgeTraversal{
		tx:       tx,
		ctx:      ctx,
		filter:   filter,
		override: override,
		adj:      make(map[lattice.Coord]map[lattice.Coord]float64),
		order:    make(map[lattice.Coord][]lattice.Coord),
	}
}

func (t *edgeTraversal) neighbors(from lattice.Coord) []lattice.Coord {
	t.load(from)
	return t.order[from]
}

func (t *edgeTraversal) cost(from, to lattice.Coord) float64 {
	t.load(from)
	if t.err != nil {
		return -1
	}
	if _, ok := t.adj[from][to]; !ok {
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
	return t.adj[from][to]
}

func (t *edgeTraversal) load(from lattice.Coord) {
	if t.err != nil {
		return
	}
	if _, loaded := t.adj[from]; loaded {
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

	weights := make(map[lattice.Coord]float64)
	var neighbors []lattice.Coord
	err = t.tx.AscendEdgesFrom(packed, func(edge record.EdgeRecord) bool {
		if t.filter != "" && edge.RelationType != t.filter {
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
		previous, duplicate := weights[to]
		if !duplicate {
			neighbors = append(neighbors, to)
		}
		if !duplicate || weight < previous {
			weights[to] = weight
		}
		return true
	})
	if err != nil && t.err == nil {
		t.err = err
	}
	if t.err != nil {
		return
	}
	t.adj[from] = weights
	t.order[from] = neighbors
}
