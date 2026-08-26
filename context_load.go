package hexxladb

import (
	"context"
	"errors"
	"fmt"
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
	// MaxContextCandidateProbes bounds the combined radial and seam coordinate
	// work accepted by one [Tx.LoadContext] call.
	MaxContextCandidateProbes = 200_000
)

// LoadContextConfig is the unified configuration for all context loading strategies.
// Pass it to [Tx.LoadContext]; the DB selects the optimal internal algorithm
// automatically based on the provided fields — callers do not need to know about
// multi-seed deduplication or graph BFS internals.
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
// The DB selects the optimal algorithm automatically:
//   - EdgeFilter non-empty → graph BFS traversal (naturally excludes cells with no edge connections)
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
	if ctx == nil {
		return ContextPack{}, fmt.Errorf("%w: nil context", ErrInvalidArgument)
	}
	if err := validateLoadContextConfig(cfg); err != nil {
		return ContextPack{}, err
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
	maxCells := cfg.MaxCells
	if maxCells == 0 {
		maxCells = 256
	}
	work := int64(maxCells)
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
