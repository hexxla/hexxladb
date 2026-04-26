package views

import (
	"context"
	"errors"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// SeamTypeSupersedes is the canonical seam type written by MarkSupersedes.
// Defined here so budget.go can walk supersession chains without importing
// the root hexxladb package.
const SeamTypeSupersedes = "supersedes"

const maxSupersessionDepth = 16

// LoadContextBudgetConfig configures [LoadContextWithBudgeting].
type LoadContextBudgetConfig struct {
	Assemble          AssembleCellViewOpts
	MaxCandidateCells int // upper bound on cells considered before eviction; default 256
	IncludeFacetText  bool
	IncludeSeams      bool
	SeamRadius        int
	Explain           bool // when true, populate ContextPack.Explanations
	// FilterSuperseded walks [SeamTypeSupersedes] chains for each candidate cell
	// and replaces superseded cells with their current-truth successor (if the
	// successor exists in the DB). Cells with no live successor are excluded.
	// Cycles are detected and broken after maxSupersessionDepth hops (default 16).
	FilterSuperseded bool
}

// scoredCandidate pairs a CellView with the ring it was found in, used during
// context budgeting eviction.
type scoredCandidate struct {
	ring          int
	view          CellView
	originalCoord *lattice.Coord // set when this candidate replaced a superseded cell
}

// resolveSupersession walks SeamTypeSupersedes chains from coord and returns
// the current-truth coord.
//   - Returns (coord, false, nil) when the cell is not superseded.
//   - Returns (successor, true, nil) when a live successor is found within maxSupersessionDepth.
//   - Returns (Coord{}, true, nil) when the cell is superseded but the chain
//     terminus has no live cell (caller should exclude the original).
func resolveSupersession(ctx context.Context, tx TxReader, coord lattice.Coord) (resolved lattice.Coord, wasSuperseded bool, err error) {
	visited := make(map[lattice.Coord]struct{})
	current := coord
	superseded := false
	for range maxSupersessionDepth {
		if err := ctx.Err(); err != nil {
			return coord, false, err
		}
		visited[current] = struct{}{}
		seams, err := tx.FindSeams(ctx, current, 0, false)
		if err != nil {
			return coord, false, err
		}
		var next *lattice.Coord
		for _, s := range seams {
			if s.SeamType != SeamTypeSupersedes {
				continue
			}
			cellA, errA := lattice.Unpack(s.CellA)
			if errA != nil {
				continue
			}
			if cellA != current {
				continue
			}
			cellB, errB := lattice.Unpack(s.CellB)
			if errB != nil {
				continue
			}
			if _, seen := visited[cellB]; seen {
				// Cycle: treat original as superseded with no live successor.
				return lattice.Coord{}, true, nil
			}
			next = &cellB
			superseded = true
			break
		}
		if next == nil {
			if !superseded {
				return coord, false, nil
			}
			// Chain terminus: check if current has a live cell.
			pk, packErr := lattice.Pack(current)
			if packErr != nil {
				return lattice.Coord{}, true, nil
			}
			_, ok, getErr := tx.GetCell(pk)
			if getErr != nil || !ok {
				return lattice.Coord{}, true, nil
			}
			return current, true, nil
		}
		current = *next
	}
	// Depth exceeded: treat as superseded with no live successor.
	return lattice.Coord{}, true, nil
}

// collectCandidates scans rings outward from center, assembling up to capCells
// CellView candidates. If cfg.FilterSuperseded is set, superseded cells are
// replaced by their current-truth successor (or excluded if none exists).
func collectCandidates(ctx context.Context, tx TxReader, center lattice.Coord, maxR, capCells int, opts AssembleCellViewOpts, cfg LoadContextBudgetConfig) ([]scoredCandidate, []CellExplanation, error) {
	var items []scoredCandidate
	var explanations []CellExplanation
	seen := make(map[lattice.Coord]struct{})
	for ring := range maxR + 1 {
		for _, c := range lattice.Ring(center, ring) {
			if len(items) >= capCells {
				return items, explanations, nil
			}
			target := c
			var originalCoord *lattice.Coord
			if cfg.FilterSuperseded {
				resolved, wasSuperseded, err := resolveSupersession(ctx, tx, c)
				if err != nil {
					return nil, nil, err
				}
				if wasSuperseded {
					if resolved == (lattice.Coord{}) {
						if cfg.Explain {
							explanations = append(explanations, CellExplanation{
								Coord:  c,
								Ring:   ring,
								Reason: "superseded",
							})
						}
						continue
					}
					origC := c
					originalCoord = &origC
					target = resolved
				}
			}
			if _, already := seen[target]; already {
				continue
			}
			v, err := AssembleCellView(ctx, tx, target, nil, opts)
			if err != nil {
				if errors.Is(err, ErrCellNotFound) {
					continue
				}
				return nil, nil, err
			}
			if originalCoord != nil {
				v.SupersededFrom = originalCoord
				if cfg.Explain {
					explanations = append(explanations, CellExplanation{
						Coord:        *originalCoord,
						Ring:         ring,
						Reason:       "superseded",
						SupersededBy: &target,
					})
				}
			}
			seen[target] = struct{}{}
			items = append(items, scoredCandidate{ring: ring, view: v, originalCoord: originalCoord})
		}
	}
	return items, explanations, nil
}

// LoadContextWithBudgeting walks rings from center, builds [CellView] values,
// then applies HEXXLA.md-style eviction: drop the lowest-confidence cell from
// the outermost ring first until within maxTokens (or no progress).
// Token counts sum RawContent and optionally facet text.
func LoadContextWithBudgeting(ctx context.Context, tx TxReader, center lattice.Coord, maxR, maxTokens int, budgeter TokenBudgeter, cfg LoadContextBudgetConfig) (ContextPack, error) {
	if err := ctx.Err(); err != nil {
		return ContextPack{}, err
	}
	if maxTokens <= 0 || maxR < 0 {
		return ContextPack{}, ErrInvalidArgument
	}
	if budgeter == nil {
		budgeter = ByteLenBudgeter{}
	}
	capCells := cfg.MaxCandidateCells
	if capCells <= 0 {
		capCells = 256
	}
	assembleOpts := cfg.Assemble
	if assembleOpts == (AssembleCellViewOpts{}) {
		assembleOpts = DefaultAssembleCellViewOpts()
	}

	items, supersededExplanations, err := collectCandidates(ctx, tx, center, maxR, capCells, assembleOpts, cfg)
	if err != nil {
		return ContextPack{}, err
	}
	if len(items) == 0 {
		return ContextPack{}, nil
	}

	candidatesScanned := len(items)
	evicted := 0
	explanations := supersededExplanations

	total := 0
	for i := range items {
		total += CellViewTokens(budgeter, items[i].view, cfg.IncludeFacetText)
	}

	for total > maxTokens && len(items) > 0 {
		maxRing := -1
		for i := range items {
			if items[i].ring > maxRing {
				maxRing = items[i].ring
			}
		}
		drop := -1
		var minConf float64
		for i := range items {
			if items[i].ring != maxRing {
				continue
			}
			c := items[i].view.Provenance.Confidence
			if drop < 0 || c < minConf {
				drop = i
				minConf = c
			}
		}
		if drop < 0 {
			break
		}
		if cfg.Explain {
			explanations = append(explanations, CellExplanation{
				Coord:  items[drop].view.Coord,
				Ring:   items[drop].ring,
				Reason: "evicted_low_confidence",
				Tokens: CellViewTokens(budgeter, items[drop].view, cfg.IncludeFacetText),
			})
		}
		items = append(items[:drop], items[drop+1:]...)
		evicted++
		total = 0
		for i := range items {
			total += CellViewTokens(budgeter, items[i].view, cfg.IncludeFacetText)
		}
	}

	maxRingUsed := 0
	for i := range items {
		if items[i].ring > maxRingUsed {
			maxRingUsed = items[i].ring
		}
	}
	if cfg.Explain {
		for i := range items {
			explanations = append(explanations, CellExplanation{
				Coord:  items[i].view.Coord,
				Ring:   items[i].ring,
				Reason: "included",
				Tokens: CellViewTokens(budgeter, items[i].view, cfg.IncludeFacetText),
			})
		}
	}

	out := make([]CellView, len(items))
	for i := range items {
		out[i] = items[i].view
	}
	pack := ContextPack{
		Cells: out,
		Stats: ContextPackStats{
			CandidatesScanned: candidatesScanned,
			CellsEvicted:      evicted,
			MaxRingUsed:       maxRingUsed,
		},
		Explanations: explanations,
	}
	for i := range out {
		pack.TotalTokens += CellViewTokens(budgeter, out[i], cfg.IncludeFacetText)
	}

	if cfg.IncludeSeams {
		r := cfg.SeamRadius
		if r < 0 {
			return ContextPack{}, ErrInvalidArgument
		}
		if r == 0 {
			r = maxR
		}
		seams, err := tx.FindSeams(ctx, center, r, false)
		if err != nil {
			return ContextPack{}, err
		}
		pack.Seams = seams
	}
	return pack, nil
}

// LoadContextPack is an alias for [LoadContextWithBudgeting] matching the
// HEXXLA.md naming for token-capped neighbourhoods.
func LoadContextPack(ctx context.Context, tx TxReader, center lattice.Coord, maxR, maxTokens int, budgeter TokenBudgeter, cfg LoadContextBudgetConfig) (ContextPack, error) {
	return LoadContextWithBudgeting(ctx, tx, center, maxR, maxTokens, budgeter, cfg)
}

// LoadMultiContextPack assembles a merged [ContextPack] from multiple seed
// coordinates under a shared token budget.
func LoadMultiContextPack(ctx context.Context, tx TxReader, centers []lattice.Coord, maxR, maxTokens int, budgeter TokenBudgeter, assemblyCfg LoadContextBudgetConfig, deduplicateCoords bool) (ContextPack, error) {
	if err := ctx.Err(); err != nil {
		return ContextPack{}, err
	}
	if len(centers) == 0 {
		return ContextPack{}, nil
	}
	if budgeter == nil {
		budgeter = ByteLenBudgeter{}
	}
	if maxR <= 0 {
		maxR = 3
	}
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	seen := make(map[lattice.Coord]struct{})
	var merged []CellView
	var allExplanations []CellExplanation
	totalStats := ContextPackStats{}

	for _, center := range centers {
		if err := ctx.Err(); err != nil {
			return ContextPack{}, err
		}
		pack, err := LoadContextPack(ctx, tx, center, maxR, maxTokens, budgeter, assemblyCfg)
		if err != nil {
			return ContextPack{}, err
		}
		totalStats.CandidatesScanned += pack.Stats.CandidatesScanned
		totalStats.CellsEvicted += pack.Stats.CellsEvicted
		if pack.Stats.MaxRingUsed > totalStats.MaxRingUsed {
			totalStats.MaxRingUsed = pack.Stats.MaxRingUsed
		}
		for _, cv := range pack.Cells {
			if deduplicateCoords {
				if _, dup := seen[cv.Coord]; dup {
					continue
				}
				seen[cv.Coord] = struct{}{}
			}
			merged = append(merged, cv)
		}
		allExplanations = append(allExplanations, pack.Explanations...)
	}

	// Re-rank by Confidence descending for fair cross-seed budget eviction.
	sortByConfidenceDesc(merged)

	used := 0
	kept := merged[:0]
	for _, cv := range merged {
		tokens := budgeter.CountTokens(cv.RawContent)
		if used+tokens > maxTokens {
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
	}, nil
}

func sortByConfidenceDesc(vs []CellView) {
	n := len(vs)
	for i := 1; i < n; i++ {
		for j := i; j > 0 && vs[j].Provenance.Confidence > vs[j-1].Provenance.Confidence; j-- {
			vs[j], vs[j-1] = vs[j-1], vs[j]
		}
	}
}
