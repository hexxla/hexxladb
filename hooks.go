package hexxladb

import (
	"context"

	"github.com/hexxla/hexxladb/internal/record"
)

// ── Pre-write validation ──────────────────────────────────────────────────────

// CellValidator is an optional pre-write validation hook.
// Implementations return a non-nil error to reject the cell; the error is
// propagated as-is from [Tx.PutCell].
type CellValidator interface {
	ValidateCell(rec record.CellRecord) error
}

// CellValidatorFunc adapts a plain function to [CellValidator].
type CellValidatorFunc func(record.CellRecord) error

// ValidateCell implements [CellValidator].
func (f CellValidatorFunc) ValidateCell(rec record.CellRecord) error { return f(rec) }

// AfterPutCellHook is called synchronously after a successful [Tx.PutCell] inside the
// current [DB.Update] callback. The written record is passed for inspection.
//
// A non-nil error is returned from [Tx.PutCell] to the caller; the cell write itself has
// already committed at this point (within the transaction callback). Use this for
// side-effects such as confidence decay triggers, audit trails, or metric updates.
// For fire-and-forget usage, return nil and handle errors internally.
type AfterPutCellHook interface {
	AfterPutCell(ctx context.Context, rec record.CellRecord) error
}

// AfterPutCellHookFunc adapts a plain function to [AfterPutCellHook].
type AfterPutCellHookFunc func(ctx context.Context, rec record.CellRecord) error

// AfterPutCell implements [AfterPutCellHook].
func (f AfterPutCellHookFunc) AfterPutCell(ctx context.Context, rec record.CellRecord) error {
	return f(ctx, rec)
}

// AfterPutSeamHook is called synchronously after a successful [Tx.PutSeam] or
// [Tx.MarkConflict]/[Tx.MarkSupersedes] inside the current [DB.Update] callback.
// The written seam record is passed for inspection.
//
// A non-nil error is returned from the triggering write method. Use this to react
// to new seam detection — e.g. alerting on new conflicts, triggering review workflows,
// or updating semantic graphs.
type AfterPutSeamHook interface {
	AfterPutSeam(ctx context.Context, rec SeamRecord) error
}

// AfterPutSeamHookFunc adapts a plain function to [AfterPutSeamHook].
type AfterPutSeamHookFunc func(ctx context.Context, rec SeamRecord) error

// AfterPutSeam implements [AfterPutSeamHook].
func (f AfterPutSeamHookFunc) AfterPutSeam(ctx context.Context, rec SeamRecord) error {
	return f(ctx, rec)
}
