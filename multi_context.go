package hexxladb

import (
	"context"

	"github.com/hexxla/hexxladb/internal/views"
)

// MultiContextConfig controls how [Tx.LoadMultiContextPack] assembles a merged
// [ContextPack] from multiple seed coordinates.
//
// Deprecated: use [LoadContextConfig] with multiple [LoadContextConfig.Seeds] instead.
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
// Deprecated: use [Tx.LoadContext] with [LoadContextConfig.Seeds] set to multiple
// coordinates instead — the DB handles deduplication and concurrent assembly automatically.
func (tx *Tx) LoadMultiContextPack(ctx context.Context, cfg MultiContextConfig) (ContextPack, error) {
	if tx == nil || tx.db == nil {
		return ContextPack{}, ErrClosed
	}
	return views.LoadMultiContextPack(ctx, tx, cfg.Centers, cfg.MaxR, cfg.MaxTokens, cfg.Budgeter, cfg.AssemblyConfig, cfg.DeduplicateCoords)
}
