package hexxladb

import (
	"context"
	"errors"
	"time"

	"github.com/hexxla/hexxladb/internal/views"
)

// LoadContextConfig is the unified configuration for all context loading strategies.
// Pass it to [Tx.LoadContext]; the DB selects the optimal internal algorithm
// automatically based on the provided fields — callers do not need to know about
// LOD, multi-seed deduplication, or graph BFS internals.
type LoadContextConfig struct {
	// Seeds is one or more seed coordinates (required).
	// Multiple seeds are expanded concurrently and merged round-robin in seed order.
	Seeds []Coord

	// MaxRing is the maximum ring radius to expand around each seed (default 5).
	// The DB automatically switches to Level-of-Detail coarsening when MaxRing >= 10
	// and a single seed is provided, reducing lookups for large radii.
	MaxRing int

	// MaxCells is the maximum number of cells returned across all seeds (default 256).
	// This is an operational result bound, not an LLM token budget. Applications
	// own prompt rendering and model-specific token accounting.
	MaxCells int

	// EdgeFilter, when non-empty, switches to graph BFS traversal instead of ring walk.
	// Value is a comma-separated list of relation types to follow (e.g. "requires,causes").
	// Leave empty for ring-based loading.
	EdgeFilter string

	// MaxHops limits BFS depth when EdgeFilter is set (default 5).
	MaxHops int

	// AsOf applies a validity-window filter to the assembled CellViews.
	// Nil means no validity filter (uses current snapshot).
	AsOf *time.Time

	// Assembly controls CellView enrichment: facets, edges, seams, supersession.
	Assembly ContextAssemblyConfig
}

// LoadContext is the unified context loading entry point.
//
// The DB selects the optimal algorithm automatically:
//   - EdgeFilter non-empty → graph BFS traversal (naturally excludes cells with no edge connections)
//   - MaxRing >= 10 and single seed → Level-of-Detail coarsening (efficient for large radii)
//   - Multiple seeds → concurrent deduped ring walks merged round-robin
//   - Otherwise → deterministic nearest-first ring walk
//
// Always returns [ContextPack] with assembled [CellView] values regardless of the
// internal algorithm chosen.
func (tx *Tx) LoadContext(ctx context.Context, cfg LoadContextConfig) (ContextPack, error) {
	if tx == nil || tx.db == nil {
		return ContextPack{}, ErrClosed
	}
	if len(cfg.Seeds) == 0 {
		return ContextPack{}, nil
	}

	p := views.LoadContextParams{
		Seeds:      cfg.Seeds,
		MaxRing:    cfg.MaxRing,
		MaxCells:   cfg.MaxCells,
		EdgeFilter: cfg.EdgeFilter,
		MaxHops:    cfg.MaxHops,
		AsOf:       cfg.AsOf,
		Assembly:   cfg.Assembly,
	}

	pack, err := views.LoadContext(ctx, tx, p)
	if err != nil {
		if errors.Is(err, views.ErrInvalidArgument) {
			return ContextPack{}, ErrInvalidArgument
		}
		return ContextPack{}, err
	}
	return pack, nil
}
