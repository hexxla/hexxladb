package hexxladb

import (
	"context"
	"errors"
	"time"

	"github.com/hexxla/hexxladb/internal/views"
)

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

// ── *Tx wrapper methods ───────────────────────────────────────────────────────

// AssembleCellView builds a [CellView] for coord using the transaction snapshot.
// Historical views cannot include current edges. Edge and seam work use the
// same limits as [Tx.LoadContext].
func (tx *Tx) AssembleCellView(ctx context.Context, coord Coord, asOf *time.Time, opts AssembleCellViewOpts) (CellView, error) {
	if tx == nil || tx.db == nil {
		return CellView{}, ErrClosed
	}
	if opts.IncludeEdges {
		if err := tx.checkCurrentData(); err != nil {
			return CellView{}, err
		}
	}
	reader := &contextReader{Tx: tx, ctx: ctx}
	v, err := views.AssembleCellView(ctx, reader, coord, asOf, opts)
	if errors.Is(err, views.ErrCellNotFound) {
		return CellView{}, ErrCellNotFound
	}
	if errors.Is(err, views.ErrInvalidArgument) {
		return CellView{}, ErrInvalidArgument
	}
	return v, err
}
