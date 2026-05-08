package views

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// lodAutoThreshold is the MaxRing value at or above which LoadContextDispatch
// automatically uses Level-of-Detail coarsening for single-seed loads.
// At ring 10+, coarsening reduces outer-ring lookups from O(6k) to O(6k/CoarseFactor²).
const lodAutoThreshold = 10

// lodDefaultFineRadius is the inner radius loaded at full resolution when LOD is auto-selected.
const lodDefaultFineRadius = 3

// lodDefaultCoarseFactor is the subdivision factor applied beyond the fine radius.
const lodDefaultCoarseFactor = 2

// LoadContextParams is the unified parameter set for all context loading strategies.
// The implementation selects the best internal algorithm based on the provided fields.
type LoadContextParams struct {
	// Seeds is one or more seed coordinates (required).
	// Multiple seeds → concurrent deduped multi-seed ring walk.
	Seeds []lattice.Coord

	// MaxRing is the maximum ring radius to expand (default 5).
	// Automatically switches to LOD coarsening when MaxRing >= lodAutoThreshold (10)
	// and len(Seeds) == 1.
	MaxRing int

	// MaxTokens is the byte/token budget (default 4096).
	MaxTokens int

	// Budgeter counts tokens. Nil defaults to ByteLenBudgeter.
	Budgeter TokenBudgeter

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
	Assembly LoadContextBudgetConfig
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
//   - MaxRing >= lodAutoThreshold AND single seed → LOD coarsened ring walk
//   - Multiple seeds → concurrent deduped multi-seed ring walk
//   - Otherwise → standard ring walk with budgeting
//
// Always returns ContextPack regardless of internal algorithm chosen.
func LoadContext(ctx context.Context, tx TxReader, p LoadContextParams) (ContextPack, error) {
	if err := ctx.Err(); err != nil {
		return ContextPack{}, err
	}
	p = normalizeParams(p)

	switch {
	case p.EdgeFilter != "" || p.MaxHops > 0:
		walker, ok := tx.(TxEdgeWalker)
		if !ok {
			return ContextPack{}, ErrEdgeWalkerRequired
		}
		return loadContextByEdges(ctx, walker, p)

	case len(p.Seeds) == 1 && p.MaxRing >= lodAutoThreshold:
		return loadContextLOD(ctx, tx, p)

	case len(p.Seeds) > 1:
		return loadContextMultiSeedConcurrent(ctx, tx, p)

	default:
		return LoadContextWithBudgeting(ctx, tx, p.Seeds[0], p.MaxRing, p.MaxTokens, p.Budgeter, p.Assembly)
	}
}

// normalizeParams fills defaults for zero-value fields.
func normalizeParams(p LoadContextParams) LoadContextParams {
	if p.MaxRing <= 0 {
		p.MaxRing = 5
	}
	if p.MaxTokens <= 0 {
		p.MaxTokens = 4096
	}
	if p.Budgeter == nil {
		p.Budgeter = ByteLenBudgeter{}
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
	maxCoords := p.Assembly.MaxCandidateCells
	if maxCoords <= 0 {
		maxCoords = 256
	}

	seen := make(map[lattice.Coord]struct{})
	var allCoords []lattice.Coord

	for _, seed := range p.Seeds {
		coords, err := tx.WalkEdgeCoords(ctx, seed, p.EdgeFilter, p.MaxHops, maxCoords)
		if err != nil {
			return ContextPack{}, err
		}
		for _, c := range coords {
			if _, dup := seen[c]; !dup {
				seen[c] = struct{}{}
				allCoords = append(allCoords, c)
			}
		}
	}

	return assembleCoordsIntoContextPack(ctx, tx, allCoords, p)
}

// loadContextLOD loads cells using level-of-detail coarsening: inner rings at full
// resolution, outer rings at CoarseFactor² reduced resolution.
func loadContextLOD(ctx context.Context, tx TxReader, p LoadContextParams) (ContextPack, error) {
	center := p.Seeds[0]
	maxCoords := p.Assembly.MaxCandidateCells
	if maxCoords <= 0 {
		maxCoords = 256
	}

	fineR := min(lodDefaultFineRadius, p.MaxRing)
	coords, err := collectLODCoords(ctx, tx, center, fineR, p.MaxRing, lodDefaultCoarseFactor, maxCoords)
	if err != nil {
		return ContextPack{}, err
	}
	return assembleCoordsIntoContextPack(ctx, tx, coords, p)
}

// collectLODCoords gathers coordinates using LOD strategy: full resolution for
// inner rings, coarsened for outer rings.
func collectLODCoords(ctx context.Context, tx TxReader, center lattice.Coord, fineR, maxR, coarseFactor, maxCoords int) ([]lattice.Coord, error) {
	packed := lattice.WalkRingsPacked(center, fineR)
	out := make([]lattice.Coord, 0, min(len(packed), maxCoords))

	for _, p := range packed {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) >= maxCoords {
			break
		}
		_, ok, err := tx.GetCell(p)
		if err != nil {
			return nil, err
		}
		if ok {
			c, err := lattice.Unpack(p)
			if err != nil {
				continue
			}
			out = append(out, c)
		}
	}

	if fineR >= maxR || len(out) >= maxCoords {
		return out, nil
	}

	seen := make(map[lattice.PackedCoord]struct{}, len(out))
	for _, p := range packed {
		seen[p] = struct{}{}
	}

	ringBuf := make([]lattice.Coord, 0, 6*maxR+1)
	for ring := fineR + 1; ring <= maxR; ring++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ringBuf = lattice.RingInto(ringBuf[:0], center, ring)
		for _, c := range ringBuf {
			if len(out) >= maxCoords {
				return out, nil
			}
			coarse, err := lattice.CoarsenCoord(c, coarseFactor)
			if err != nil {
				continue
			}
			cp, err := lattice.Pack(coarse)
			if err != nil {
				continue
			}
			if _, dup := seen[cp]; dup {
				continue
			}
			seen[cp] = struct{}{}
			_, ok, err := tx.GetCell(cp)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, coarse)
			}
		}
	}
	return out, nil
}

// seedResult holds the per-seed ContextPack or an error from concurrent assembly.
type seedResult struct {
	pack ContextPack
	err  error
}

// loadContextMultiSeedConcurrent assembles per-seed ContextPacks concurrently then
// merges them under a shared budget. Each seed runs in its own goroutine; results are
// collected in original order so deduplication is deterministic.
func loadContextMultiSeedConcurrent(ctx context.Context, tx TxReader, p LoadContextParams) (ContextPack, error) {
	results := make([]seedResult, len(p.Seeds))
	var wg sync.WaitGroup
	wg.Add(len(p.Seeds))

	for i, seed := range p.Seeds {
		go func() {
			defer wg.Done()
			pack, err := LoadContextWithBudgeting(ctx, tx, seed, p.MaxRing, p.MaxTokens, p.Budgeter, p.Assembly)
			results[i] = seedResult{pack: pack, err: err}
		}()
	}
	wg.Wait()

	return mergeSeeds(results, p)
}

// mergeSeeds merges per-seed results into a single budget-bounded ContextPack.
func mergeSeeds(results []seedResult, p LoadContextParams) (ContextPack, error) {
	seen := make(map[lattice.Coord]struct{})
	var merged []CellView
	var allExplanations []CellExplanation
	totalStats := ContextPackStats{}

	for i := range results {
		if results[i].err != nil {
			return ContextPack{}, results[i].err
		}
		pack := results[i].pack
		totalStats.CandidatesScanned += pack.Stats.CandidatesScanned
		totalStats.CellsEvicted += pack.Stats.CellsEvicted
		if pack.Stats.MaxRingUsed > totalStats.MaxRingUsed {
			totalStats.MaxRingUsed = pack.Stats.MaxRingUsed
		}
		for _, cv := range pack.Cells {
			if _, dup := seen[cv.Coord]; dup {
				continue
			}
			seen[cv.Coord] = struct{}{}
			merged = append(merged, cv)
		}
		allExplanations = append(allExplanations, pack.Explanations...)
	}

	// Re-rank by Confidence descending for fair cross-seed budget eviction.
	slices.SortFunc(merged, func(a, b CellView) int {
		switch {
		case a.Provenance.Confidence > b.Provenance.Confidence:
			return -1
		case a.Provenance.Confidence < b.Provenance.Confidence:
			return 1
		default:
			return 0
		}
	})

	budgeter := p.Budgeter
	used := 0
	kept := merged[:0]
	for _, cv := range merged {
		tokens := budgeter.CountTokens(cv.RawContent)
		if used+tokens > p.MaxTokens {
			totalStats.CellsEvicted++
			continue
		}
		used += tokens
		kept = append(kept, cv)
	}

	return ContextPack{
		Cells:        kept,
		Seams:        []record.SeamRecord{},
		Explanations: allExplanations,
		Stats:        totalStats,
		TotalTokens:  used,
	}, nil
}

// assembleCoordsIntoContextPack takes a pre-collected list of coordinates, assembles
// CellViews concurrently (read-only, safe), then wraps into a budget-bounded ContextPack.
func assembleCoordsIntoContextPack(ctx context.Context, tx TxReader, coords []lattice.Coord, p LoadContextParams) (ContextPack, error) {
	if len(coords) == 0 {
		return ContextPack{}, nil
	}

	type viewResult struct {
		idx  int
		view CellView
		ok   bool
		err  error
	}

	results := make([]viewResult, len(coords))
	var wg sync.WaitGroup
	wg.Add(len(coords))

	for i, c := range coords {
		go func() {
			defer wg.Done()
			v, err := AssembleCellView(ctx, tx, c, p.AsOf, p.Assembly.Assemble)
			if err != nil {
				if isNotFound(err) {
					results[i] = viewResult{idx: i, ok: false}
					return
				}
				results[i] = viewResult{idx: i, err: err}
				return
			}
			results[i] = viewResult{idx: i, view: v, ok: true}
		}()
	}
	wg.Wait()

	views := make([]CellView, 0, len(coords))
	for i := range results {
		if results[i].err != nil {
			return ContextPack{}, results[i].err
		}
		if results[i].ok {
			views = append(views, results[i].view)
		}
	}

	// Apply token budget: sort by confidence descending then truncate.
	slices.SortFunc(views, func(a, b CellView) int {
		switch {
		case a.Provenance.Confidence > b.Provenance.Confidence:
			return -1
		case a.Provenance.Confidence < b.Provenance.Confidence:
			return 1
		default:
			return 0
		}
	})

	budgeter := p.Budgeter
	used := 0
	evicted := 0
	kept := views[:0]
	for _, v := range views {
		tokens := budgeter.CountTokens(v.RawContent)
		if used+tokens > p.MaxTokens {
			evicted++
			continue
		}
		used += tokens
		kept = append(kept, v)
	}

	return ContextPack{
		Cells:       kept,
		TotalTokens: used,
		Stats: ContextPackStats{
			CandidatesScanned: len(coords),
			CellsEvicted:      evicted,
		},
	}, nil
}

// isNotFound reports whether err is the views-package cell-not-found sentinel.
func isNotFound(err error) bool {
	return err != nil && err.Error() == ErrCellNotFound.Error()
}

// ErrEdgeWalkerRequired is returned when EdgeFilter is set but the TxReader
// does not implement TxEdgeWalker.
var ErrEdgeWalkerRequired = errEdgeWalkerRequired{}

type errEdgeWalkerRequired struct{}

func (errEdgeWalkerRequired) Error() string {
	return "views: EdgeFilter requires TxEdgeWalker; the transaction does not support edge walks"
}
