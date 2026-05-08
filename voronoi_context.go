package hexxladb

import (
	"context"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// VoronoiWeightFunc returns an additional traversal cost for a coordinate
// (must be >= 0). Return 0 for uniform cost, which is identical to the default
// geometric Voronoi behaviour. Nil is treated as uniform cost.
type VoronoiWeightFunc = lattice.WeightFunc

// VoronoiContextConfig configures [Tx.LoadContextVoronoi].
type VoronoiContextConfig struct {
	// MaxRadius bounds the Dijkstra expansion from each seed (default 4).
	MaxRadius int
	// MaxCellsPerSeed caps cells loaded per Voronoi region (default 64).
	MaxCellsPerSeed int
	// WeightFunc optionally adjusts the traversal cost for each coordinate.
	// Higher cost makes a coordinate harder to claim from a given seed.
	// Nil means uniform cost (geometric Voronoi by hex distance).
	WeightFunc VoronoiWeightFunc
}

func (cfg *VoronoiContextConfig) withDefaults() {
	if cfg.MaxRadius <= 0 {
		cfg.MaxRadius = 4
	}
	if cfg.MaxCellsPerSeed <= 0 {
		cfg.MaxCellsPerSeed = 64
	}
}

// LoadContextVoronoi partitions the area around multiple seeds using a Voronoi
// diagram (multi-source Dijkstra on the hex grid), then loads cells for each region.
//
// Unlike loading independent radial neighborhoods per seed (which causes overlap
// near adjacent seeds), Voronoi assigns each coordinate to exactly one seed.
// This gives each seed a fair, non-overlapping share of the context budget.
//
// When [VoronoiContextConfig.WeightFunc] is set, the Dijkstra expansion uses
// the returned cost as an additional penalty, allowing callers to steer region
// boundaries using confidence, recency, or semantic similarity scores.
//
// Returns a map from seed index to the cells in that seed's region.
func (tx *Tx) LoadContextVoronoi(ctx context.Context, seeds []Coord, cfg VoronoiContextConfig) (map[int][]record.CellRecord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	cfg.withDefaults()
	if len(seeds) == 0 {
		return nil, nil
	}

	cells, _ := lattice.Voronoi(seeds, cfg.MaxRadius, cfg.WeightFunc)
	return tx.loadVoronoiRegions(ctx, seeds, cells, cfg)
}

// loadVoronoiRegions fetches cells for each Voronoi region, respecting per-seed caps.
func (tx *Tx) loadVoronoiRegions(ctx context.Context, seeds []Coord, cells []lattice.VoronoiCell, cfg VoronoiContextConfig) (map[int][]record.CellRecord, error) {
	counts := make(map[int]int, len(seeds))
	result := make(map[int][]record.CellRecord, len(seeds))

	for _, vc := range cells {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if counts[vc.SeedIdx] >= cfg.MaxCellsPerSeed {
			continue
		}
		rec, err := tx.fetchVoronoiCell(vc)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			result[vc.SeedIdx] = append(result[vc.SeedIdx], *rec)
			counts[vc.SeedIdx]++
		}
	}
	return result, nil
}

// fetchVoronoiCell packs and fetches a single cell from the Voronoi partition.
func (tx *Tx) fetchVoronoiCell(vc lattice.VoronoiCell) (*record.CellRecord, error) {
	p, err := lattice.Pack(vc.Coord)
	if err != nil {
		return nil, nil //nolint:nilerr // out-of-range coords are silently skipped
	}
	rec, ok, err := tx.GetCell(p)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &rec, nil
}
