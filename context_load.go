package hexxladb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hexxla/hexxladb/internal/views"
)

const (
	// MaxContextRadius bounds radial candidate enumeration for [Tx.LoadContext].
	MaxContextRadius = 128
	// MaxContextSeeds bounds multi-seed fan-out for [Tx.LoadContext].
	MaxContextSeeds = 32
	// MaxContextResults bounds one assembled context result.
	MaxContextResults = 10_000
	// MaxContextHops bounds graph traversal depth for [Tx.LoadContext].
	MaxContextHops = 128
	// MaxContextCandidateProbes bounds radial and regional seam coordinate work
	// during validation and the number of candidates visited in graph mode.
	MaxContextCandidateProbes = 200_000
	// MaxContextEdgeRows bounds outgoing edge rows scanned for traversal and
	// enrichment across a single context load, including nonmatching relations.
	MaxContextEdgeRows = 200_000
	// MaxContextSeamWork bounds seam coordinate probes and index rows across a
	// single context load, including regional seams and supersession.
	MaxContextSeamWork = 200_000
)

// LoadContextConfig controls seed expansion and enrichment for [Tx.LoadContext].
type LoadContextConfig struct {
	// Seeds is one or more seed coordinates (required).
	// Multiple seeds are expanded concurrently and merged round-robin in seed order.
	Seeds []Coord

	// MaxRing is the maximum ring radius to expand around each seed (default 5,
	// maximum [MaxContextRadius]).
	MaxRing int

	// MaxCells is the maximum number of cells returned across all seeds (default 256,
	// maximum [MaxContextResults]).
	// This is an operational result bound, not an LLM token budget. Applications
	// own prompt rendering and model-specific token accounting.
	MaxCells int

	// EdgeFilter, when non-empty, switches to graph BFS traversal instead of ring walk.
	// Value is a comma-separated list of relation types to follow (e.g. "requires,causes").
	// Leave empty for ring-based loading.
	EdgeFilter string

	// MaxHops limits BFS depth when EdgeFilter is set (default 5, maximum
	// [MaxContextHops]).
	MaxHops int

	// AsOf applies a validity-window filter to the assembled CellViews.
	// Nil means no validity filter (uses current snapshot).
	AsOf *time.Time

	// Assembly controls CellView enrichment: facets, edges, seams, supersession.
	Assembly ContextAssemblyConfig
}

// LoadContext is the unified context loading entry point.
//
// Expansion follows these rules:
//   - EdgeFilter non-empty → breadth-first graph traversal starting at each seed
//   - Multiple seeds → concurrent deduped ring walks merged round-robin
//   - Otherwise → deterministic nearest-first ring walk
//
// Always returns [ContextPack] with assembled [CellView] values regardless of the
// internal algorithm chosen. Exhausted work budgets return [ErrSpatialScanLimit]
// without a partial pack. Historical views cannot traverse or include current edges.
func (tx *Tx) LoadContext(ctx context.Context, cfg LoadContextConfig) (ContextPack, error) {
	if tx == nil || tx.db == nil {
		return ContextPack{}, ErrClosed
	}
	if len(cfg.Seeds) == 0 {
		return ContextPack{}, nil
	}
	if ctx == nil {
		return ContextPack{}, fmt.Errorf("%w: nil context", ErrInvalidArgument)
	}
	if err := validateLoadContextConfig(cfg); err != nil {
		return ContextPack{}, err
	}

	if cfg.EdgeFilter != "" || cfg.Assembly.Assemble.IncludeEdges {
		if err := tx.checkCurrentData(); err != nil {
			return ContextPack{}, err
		}
	}
	reader := &contextReader{Tx: tx, ctx: ctx}
	p := views.LoadContextParams{
		Seeds:    cfg.Seeds,
		MaxRing:  cfg.MaxRing,
		MaxCells: cfg.MaxCells,
		MaxHops:  cfg.MaxHops,
		AsOf:     cfg.AsOf,
		Assembly: cfg.Assembly,
	}
	if cfg.EdgeFilter != "" {
		matches := matchContextRelations(cfg.EdgeFilter)
		p.MaxGraphCandidates = MaxContextCandidateProbes
		p.WalkEdges = func(ctx context.Context, start Coord, maxHops int, visit func(Coord) bool) error {
			return walkEdgeCoords(ctx, start, maxHops, matches, reader.AscendEdgesFrom, visit)
		}
	}

	pack, err := views.LoadContext(ctx, reader, p)
	if err != nil {
		if errors.Is(err, views.ErrGraphCandidateLimit) {
			return ContextPack{}, fmt.Errorf("%w: %w", ErrSpatialScanLimit, err)
		}
		if errors.Is(err, views.ErrInvalidArgument) {
			return ContextPack{}, ErrInvalidArgument
		}
		return ContextPack{}, err
	}
	return pack, nil
}

// One reader shares work limits across traversal, enrichment, and parallel seeds.
type contextReader struct {
	*Tx
	ctx      context.Context
	edgeRows atomic.Int64
	seamWork atomic.Int64
}

func (r *contextReader) AscendEdgesFrom(from PackedCoord, fn func(EdgeWalkRecord) bool) error {
	return r.ascendEdgesFrom(from, func() error {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if r.edgeRows.Add(1) > MaxContextEdgeRows {
			return fmt.Errorf("%w: context edge rows exceed %d", ErrSpatialScanLimit, MaxContextEdgeRows)
		}
		return nil
	}, fn)
}

func (r *contextReader) FindSeams(ctx context.Context, center Coord, radius int, unresolvedOnly bool) ([]SeamRecord, error) {
	return r.findSeamsWithBudget(ctx, center, radius, unresolvedOnly, nil, seamSearchBudget{
		charge: func() error {
			if r.seamWork.Add(1) > MaxContextSeamWork {
				return fmt.Errorf("%w: context seam work exceeds %d", ErrSpatialScanLimit, MaxContextSeamWork)
			}
			return nil
		},
	})
}

func matchContextRelations(filter string) func(string) bool {
	if !strings.Contains(filter, ",") {
		return matchExactRelation(filter)
	}
	allowed := make(map[string]struct{})
	for relation := range strings.SplitSeq(filter, ",") {
		if relation = strings.TrimSpace(relation); relation != "" {
			allowed[relation] = struct{}{}
		}
	}
	return func(relation string) bool {
		_, ok := allowed[relation]
		return ok
	}
}

func validateLoadContextConfig(cfg LoadContextConfig) error {
	if len(cfg.Seeds) > MaxContextSeeds {
		return fmt.Errorf("%w: LoadContext supports at most %d seeds", ErrInvalidArgument, MaxContextSeeds)
	}
	if cfg.MaxRing < 0 || cfg.MaxRing > MaxContextRadius {
		return fmt.Errorf("%w: LoadContext MaxRing must be between 0 and %d", ErrInvalidArgument, MaxContextRadius)
	}
	if cfg.MaxCells < 0 || cfg.MaxCells > MaxContextResults {
		return fmt.Errorf("%w: LoadContext MaxCells must be between 0 and %d", ErrInvalidArgument, MaxContextResults)
	}
	if cfg.MaxHops < 0 || cfg.MaxHops > MaxContextHops {
		return fmt.Errorf("%w: LoadContext MaxHops must be between 0 and %d", ErrInvalidArgument, MaxContextHops)
	}
	if cfg.Assembly.SeamRadius < 0 || cfg.Assembly.SeamRadius > MaxSeamSearchRadius {
		return fmt.Errorf("%w: LoadContext seam radius must be between 0 and %d", ErrInvalidArgument, MaxSeamSearchRadius)
	}

	maxRing := cfg.MaxRing
	if maxRing == 0 {
		maxRing = 5
	}
	var work int64
	if cfg.EdgeFilter == "" {
		work = int64(len(cfg.Seeds)) * int64(hexDiskCellCount(maxRing))
	}
	for _, seed := range cfg.Seeds {
		validationRadius := 0
		if cfg.EdgeFilter == "" {
			validationRadius = maxRing
		}
		if err := validatePackedRadius(seed, validationRadius); err != nil {
			return err
		}
	}
	if cfg.Assembly.IncludeSeams {
		seamRadius := cfg.Assembly.SeamRadius
		if seamRadius == 0 {
			seamRadius = maxRing
		}
		if seamRadius > MaxSeamSearchRadius {
			return fmt.Errorf("%w: LoadContext seam radius must not exceed %d", ErrInvalidArgument, MaxSeamSearchRadius)
		}
		work += int64(len(cfg.Seeds)) * int64(hexDiskCellCount(seamRadius))
		for _, seed := range cfg.Seeds {
			if err := validatePackedRadius(seed, seamRadius); err != nil {
				return err
			}
		}
	}
	if work > MaxContextCandidateProbes {
		return fmt.Errorf("%w: LoadContext candidate work %d exceeds limit %d", ErrInvalidArgument, work, MaxContextCandidateProbes)
	}
	return nil
}
