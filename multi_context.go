package hexxladb

import (
	"context"
	"sort"

	"github.com/hexxla/hexxladb/internal/record"
)

// MultiContextConfig controls how [Tx.LoadMultiContextPack] assembles a merged
// [ContextPack] from multiple seed coordinates.
//
// A typical usage pattern is to pass the top-N [CellSearchResult.Cell.Coord]
// values from [Tx.SearchCells] as Centers, then let LoadMultiContextPack
// expand each seed's neighbourhood and merge into a single budget-bounded pack.
type MultiContextConfig struct {
	// Centers are the seed coordinates to expand from (e.g. from SearchCells).
	// Seeds are processed in order; earlier seeds' neighbourhoods take priority
	// when DeduplicateCoords is true.
	Centers []Coord

	// MaxR is the ring radius to expand around each seed (passed to LoadContextPack).
	MaxR int

	// MaxTokens is the shared token budget across all seeds combined.
	MaxTokens int

	// Budgeter counts tokens for budget enforcement. Defaults to ByteLenBudgeter
	// when nil.
	Budgeter TokenBudgeter

	// AssemblyConfig is forwarded to each per-seed LoadContextPack call.
	// FilterSuperseded, Explain, IncludeSeams etc. apply to every seed uniformly.
	AssemblyConfig LoadContextBudgetConfig

	// DeduplicateCoords skips cells already included by an earlier seed's pack,
	// preventing double-counting of shared neighbourhood cells.
	DeduplicateCoords bool
}

// LoadMultiContextPack assembles a merged [ContextPack] from multiple seed
// coordinates under a shared token budget.
//
// Each seed in cfg.Centers is expanded independently via [Tx.LoadContextPack].
// The resulting cell views are merged, optionally deduplicated, then re-ranked
// by Confidence descending and truncated to cfg.MaxTokens using cfg.Budgeter.
//
// The returned ContextPack.Stats reflects totals across all seeds.
// If cfg.AssemblyConfig.Explain is true, Explanations are merged from all seeds.
func (tx *Tx) LoadMultiContextPack(ctx context.Context, cfg MultiContextConfig) (ContextPack, error) {
	if err := ctx.Err(); err != nil {
		return ContextPack{}, err
	}
	if tx == nil || tx.db == nil {
		return ContextPack{}, ErrClosed
	}
	if len(cfg.Centers) == 0 {
		return ContextPack{}, nil
	}

	budgeter := cfg.Budgeter
	if budgeter == nil {
		budgeter = ByteLenBudgeter{}
	}
	maxR := cfg.MaxR
	if maxR <= 0 {
		maxR = 3
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	seen := make(map[Coord]struct{})
	var merged []CellView
	var allExplanations []CellExplanation
	totalStats := ContextPackStats{}

	for _, center := range cfg.Centers {
		if err := ctx.Err(); err != nil {
			return ContextPack{}, err
		}

		// Use a generous per-seed budget; final truncation happens below.
		pack, err := tx.LoadContextPack(ctx, center, maxR, maxTokens, budgeter, cfg.AssemblyConfig)
		if err != nil {
			return ContextPack{}, err
		}

		totalStats.CandidatesScanned += pack.Stats.CandidatesScanned
		totalStats.CellsEvicted += pack.Stats.CellsEvicted
		if pack.Stats.MaxRingUsed > totalStats.MaxRingUsed {
			totalStats.MaxRingUsed = pack.Stats.MaxRingUsed
		}

		for _, cv := range pack.Cells {
			if cfg.DeduplicateCoords {
				if _, dup := seen[cv.Coord]; dup {
					continue
				}
				seen[cv.Coord] = struct{}{}
			}
			merged = append(merged, cv)
		}

		allExplanations = append(allExplanations, pack.Explanations...)
	}

	// Re-rank by Confidence descending for fair budget eviction across seeds.
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Provenance.Confidence > merged[j].Provenance.Confidence
	})

	// Apply shared token budget: keep highest-confidence cells that fit.
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
