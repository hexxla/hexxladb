package views

import (
	"context"
	"sync"
	"time"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// LoadContextParams is the unified parameter set for all context loading strategies.
// The implementation selects the best internal algorithm based on the provided fields.
type LoadContextParams struct {
	// Seeds is one or more seed coordinates (required).
	// Multiple seeds → concurrent deduped multi-seed ring walk.
	Seeds []lattice.Coord

	// MaxRing is the maximum ring radius to expand (default 5).
	MaxRing int

	// MaxCells is the maximum number of cells returned (default 256).
	MaxCells int

	// EdgeFilter, when non-empty, switches to graph BFS traversal.
	// Comma-separated list of relation types to follow (e.g. "requires,causes").
	// Requires the TxReader to satisfy TxEdgeWalker.
	EdgeFilter string

	// MaxHops limits BFS depth when EdgeFilter is set (default 5).
	MaxHops int

	// AsOf pins context assembly to a specific time (validity filter only).
	// Nil = no validity filter (current snapshot).
	AsOf *time.Time

	// Assembly controls CellView enrichment (facets, edges, seams, supersession).
	Assembly ContextAssemblyConfig
}

// TxEdgeWalker extends TxReader with edge-walk capability needed for graph-based loading.
// *Tx satisfies this structurally; callers that don't need graph traversal don't need it.
type TxEdgeWalker interface {
	TxReader
	// WalkEdgeCoords performs BFS from start, following edges matching filter,
	// up to maxHops depth and maxCoords total. Returns visited coordinates.
	WalkEdgeCoords(ctx context.Context, start lattice.Coord, filter string, maxHops, maxCoords int) ([]lattice.Coord, error)
}

// LoadContext is the unified context loading entry point. It selects the optimal
// algorithm automatically:
//   - EdgeFilter non-empty  → graph BFS traversal (requires TxEdgeWalker)
//   - Multiple seeds → concurrent deduped multi-seed ring walk
//   - Otherwise → deterministic nearest-first ring walk
//
// Always returns ContextPack regardless of internal algorithm chosen.
func LoadContext(ctx context.Context, tx TxReader, p LoadContextParams) (ContextPack, error) {
	if err := ctx.Err(); err != nil {
		return ContextPack{}, err
	}
	if p.MaxCells < 0 {
		return ContextPack{}, ErrInvalidArgument
	}
	p = normalizeParams(p)

	switch {
	case p.EdgeFilter != "":
		walker, ok := tx.(TxEdgeWalker)
		if !ok {
			return ContextPack{}, ErrEdgeWalkerRequired
		}
		return loadContextByEdges(ctx, walker, p)

	case len(p.Seeds) > 1:
		return loadContextMultiSeedConcurrent(ctx, tx, p)

	default:
		return loadContextByRings(ctx, tx, p.Seeds[0], p.MaxRing, p.MaxCells, p.AsOf, p.Assembly)
	}
}

// normalizeParams fills defaults for zero-value fields.
func normalizeParams(p LoadContextParams) LoadContextParams {
	if p.MaxRing <= 0 {
		p.MaxRing = 5
	}
	if p.MaxCells <= 0 {
		p.MaxCells = 256
	}
	if p.MaxHops <= 0 && p.EdgeFilter != "" {
		p.MaxHops = 5
	}
	if p.Assembly.Assemble == (AssembleCellViewOpts{}) {
		p.Assembly.Assemble = DefaultAssembleCellViewOpts()
	}
	return p
}

// loadContextByEdges walks edges from each seed via BFS, assembles CellViews, wraps into ContextPack.
func loadContextByEdges(ctx context.Context, tx TxEdgeWalker, p LoadContextParams) (ContextPack, error) {
	maxCoords := p.MaxCells

	seen := make(map[lattice.Coord]struct{})
	var allCoords []lattice.Coord

	for _, seed := range p.Seeds {
		coords, err := tx.WalkEdgeCoords(ctx, seed, p.EdgeFilter, p.MaxHops, maxCoords)
		if err != nil {
			return ContextPack{}, err
		}
		for _, c := range coords {
			if len(allCoords) >= maxCoords {
				break
			}
			if _, dup := seen[c]; !dup {
				seen[c] = struct{}{}
				allCoords = append(allCoords, c)
			}
		}
		if len(allCoords) >= maxCoords {
			break
		}
	}

	return assembleCoordsIntoContextPack(ctx, tx, allCoords, p.Seeds, p.MaxCells, p.MaxRing, p.AsOf, p.Assembly)
}

// seedResult holds the per-seed ContextPack or an error from concurrent assembly.
type seedResult struct {
	pack ContextPack
	err  error
}

// loadContextMultiSeedConcurrent assembles per-seed ContextPacks concurrently.
// Results are merged round-robin in original seed order so no seed is allowed to
// monopolise the shared result limit and deduplication remains deterministic.
func loadContextMultiSeedConcurrent(ctx context.Context, tx TxReader, p LoadContextParams) (ContextPack, error) {
	results := make([]seedResult, len(p.Seeds))
	var wg sync.WaitGroup

	for i, seed := range p.Seeds {
		wg.Go(func() {
			pack, err := loadContextByRings(ctx, tx, seed, p.MaxRing, p.MaxCells, p.AsOf, p.Assembly)
			results[i] = seedResult{pack: pack, err: err}
		})
	}
	wg.Wait()

	return mergeSeeds(results, p)
}

// mergeSeeds merges per-seed results into a single result-bounded ContextPack.
func mergeSeeds(results []seedResult, p LoadContextParams) (ContextPack, error) {
	seen := make(map[lattice.Coord]struct{})
	merged := make([]CellView, 0, min(p.MaxCells, 256))
	totalStats := ContextPackStats{}

	for i := range results {
		if results[i].err != nil {
			return ContextPack{}, results[i].err
		}
		pack := results[i].pack
		totalStats.CandidatesScanned += pack.Stats.CandidatesScanned
		if pack.Stats.MaxRingUsed > totalStats.MaxRingUsed {
			totalStats.MaxRingUsed = pack.Stats.MaxRingUsed
		}
	}

	for candidateIndex := 0; len(merged) < p.MaxCells; candidateIndex++ {
		advanced := false
		for i := range results {
			if candidateIndex >= len(results[i].pack.Cells) {
				continue
			}
			advanced = true
			candidate := results[i].pack.Cells[candidateIndex]
			if _, duplicate := seen[candidate.Coord]; duplicate {
				continue
			}
			seen[candidate.Coord] = struct{}{}
			merged = append(merged, candidate)
			if len(merged) == p.MaxCells {
				break
			}
		}
		if !advanced {
			break
		}
	}
	totalStats.ResultLimitReached = len(merged) == p.MaxCells

	return ContextPack{
		Cells:        merged,
		Seams:        mergeSeedSeams(results),
		Explanations: mergeSeedExplanations(results, merged, p.Assembly.Explain),
		Stats:        totalStats,
	}, nil
}

func mergeSeedSeams(results []seedResult) []record.SeamRecord {
	seen := make(map[string]struct{})
	var merged []record.SeamRecord
	for i := range results {
		for _, seam := range results[i].pack.Seams {
			if _, duplicate := seen[seam.ID]; duplicate {
				continue
			}
			seen[seam.ID] = struct{}{}
			merged = append(merged, seam)
		}
	}
	return merged
}

func mergeSeedExplanations(results []seedResult, cells []CellView, explain bool) []CellExplanation {
	if !explain {
		return nil
	}
	included := make(map[lattice.Coord]struct{}, len(cells))
	for _, cell := range cells {
		included[cell.Coord] = struct{}{}
	}
	seenIncluded := make(map[lattice.Coord]struct{}, len(cells))
	var merged []CellExplanation
	for i := range results {
		for _, explanation := range results[i].pack.Explanations {
			if explanation.Reason != "included" {
				merged = append(merged, explanation)
				continue
			}
			if _, kept := included[explanation.Coord]; !kept {
				continue
			}
			if _, duplicate := seenIncluded[explanation.Coord]; duplicate {
				continue
			}
			seenIncluded[explanation.Coord] = struct{}{}
			merged = append(merged, explanation)
		}
	}
	return merged
}

// ErrEdgeWalkerRequired is returned when EdgeFilter is set but the TxReader
// does not implement TxEdgeWalker.
var ErrEdgeWalkerRequired = errEdgeWalkerRequired{}

type errEdgeWalkerRequired struct{}

func (errEdgeWalkerRequired) Error() string {
	return "views: EdgeFilter requires TxEdgeWalker; the transaction does not support edge walks"
}
