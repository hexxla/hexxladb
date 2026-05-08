package hexxladb

import (
	"context"
	"errors"
	"time"

	"github.com/hexxla/hexxladb/internal/views"
)

// ── Type aliases ──────────────────────────────────────────────────────────────
// All view types live in internal/views. These aliases re-export them under the
// stable public API without duplicating definitions.

// FacetView is a read-oriented facet projection aligned with docs/hexxladb/HEXXLA.md.
type FacetView = views.FacetView

// EdgeView summarises one directed edge from a cell.
type EdgeView = views.EdgeView

// SeamRef is a lightweight seam handle for embedding in [CellView].
type SeamRef = views.SeamRef

// CellView aggregates decoded cell content with optional facets, edges, and seam refs.
type CellView = views.CellView

// ContextPackStats records assembly statistics for debugging and observability.
type ContextPackStats = views.ContextPackStats

// CellExplanation records why a cell was included or excluded during context assembly.
type CellExplanation = views.CellExplanation

// ContextPack matches the neighbourhood summary shape described in HEXXLA.md.
type ContextPack = views.ContextPack

// TokenBudgeter counts tokens for budgeting (e.g. approximate LLM tokens).
// Inject domain-specific logic; the default is [ByteLenBudgeter].
type TokenBudgeter = views.TokenBudgeter

// ByteLenBudgeter counts tokens as UTF-8 byte length (cheap default; not tokenizer-accurate).
type ByteLenBudgeter = views.ByteLenBudgeter

// AssembleCellViewOpts configures [Tx.AssembleCellView].
type AssembleCellViewOpts = views.AssembleCellViewOpts

// LoadContextBudgetConfig configures [Tx.LoadContextWithBudgeting].
type LoadContextBudgetConfig = views.LoadContextBudgetConfig

// CellViewPredicate selects [CellView] rows when filtering slices.
type CellViewPredicate = views.CellViewPredicate

// ── Re-exported constructors / free functions ─────────────────────────────────

// DefaultAssembleCellViewOpts returns defaults: facets on; edges and seams off.
func DefaultAssembleCellViewOpts() AssembleCellViewOpts {
	return views.DefaultAssembleCellViewOpts()
}

// FilterCellViews returns only views for which pred reports true.
// If pred is nil, returns a copy of the input slice.
func FilterCellViews(vs []CellView, pred CellViewPredicate) []CellView {
	return views.FilterCellViews(vs, pred)
}

// TruncateCellViewsToTokenBudget returns a prefix of views (order preserved)
// such that the summed token count is at most maxTokens.
func TruncateCellViewsToTokenBudget(vs []CellView, budgeter TokenBudgeter, maxTokens int, includeFacetText bool) (out []CellView, total int) {
	return views.TruncateCellViewsToTokenBudget(vs, budgeter, maxTokens, includeFacetText)
}

// ── *Tx wrapper methods ───────────────────────────────────────────────────────

// AssembleCellView builds a [CellView] for coord using the transaction snapshot.
func (tx *Tx) AssembleCellView(ctx context.Context, coord Coord, asOf *time.Time, opts AssembleCellViewOpts) (CellView, error) {
	if tx == nil || tx.db == nil {
		return CellView{}, ErrClosed
	}
	v, err := views.AssembleCellView(ctx, tx, coord, asOf, opts)
	if errors.Is(err, views.ErrCellNotFound) {
		return CellView{}, ErrCellNotFound
	}
	if errors.Is(err, views.ErrInvalidArgument) {
		return CellView{}, ErrInvalidArgument
	}
	return v, err
}

// LoadContextWithBudgeting walks rings from center, builds [CellView] values,
// then applies HEXXLA.md-style eviction: drop lowest-confidence cells from the
// outermost ring first until within maxTokens (or no progress).
//
// Deprecated: use [Tx.LoadContext] with a [LoadContextConfig] instead.
func (tx *Tx) LoadContextWithBudgeting(ctx context.Context, center Coord, maxR, maxTokens int, budgeter TokenBudgeter, cfg LoadContextBudgetConfig) (ContextPack, error) {
	if tx == nil || tx.db == nil {
		return ContextPack{}, ErrClosed
	}
	pack, err := views.LoadContextWithBudgeting(ctx, tx, center, maxR, maxTokens, budgeter, cfg)
	if errors.Is(err, views.ErrInvalidArgument) {
		return ContextPack{}, ErrInvalidArgument
	}
	return pack, err
}

// LoadContextPack matches HEXXLA.md naming for token-capped neighbourhoods;
// it forwards to [Tx.LoadContextWithBudgeting].
//
// Deprecated: use [Tx.LoadContext] with a [LoadContextConfig] instead.
func (tx *Tx) LoadContextPack(ctx context.Context, center Coord, maxR, maxTokens int, budgeter TokenBudgeter, cfg LoadContextBudgetConfig) (ContextPack, error) {
	return tx.LoadContextWithBudgeting(ctx, center, maxR, maxTokens, budgeter, cfg)
}

// LoadContextPackFrom is a unified entry point for one or many seed coordinates.
//
// Deprecated: use [Tx.LoadContext] with a [LoadContextConfig] instead; pass multiple
// coords via [LoadContextConfig.Seeds].
func (tx *Tx) LoadContextPackFrom(ctx context.Context, maxR, maxTokens int, budgeter TokenBudgeter, cfg LoadContextBudgetConfig, centers ...Coord) (ContextPack, error) {
	switch len(centers) {
	case 0:
		return ContextPack{}, nil
	case 1:
		return tx.LoadContextPack(ctx, centers[0], maxR, maxTokens, budgeter, cfg)
	default:
		return tx.LoadMultiContextPack(ctx, MultiContextConfig{
			Centers:           centers,
			MaxR:              maxR,
			MaxTokens:         maxTokens,
			Budgeter:          budgeter,
			AssemblyConfig:    cfg,
			DeduplicateCoords: true,
		})
	}
}
