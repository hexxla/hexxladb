package hexxladb

import (
	"context"
)

// ── Pre-write validation ──────────────────────────────────────────────────────

// CellValidator is an optional pre-write validation hook.
// Implementations return a non-nil error to reject the cell; the error is
// propagated as-is from [Tx.PutCell].
type CellValidator interface {
	ValidateCell(rec CellRecord) error
}

// CellValidatorFunc adapts a plain function to [CellValidator].
type CellValidatorFunc func(CellRecord) error

// ValidateCell implements [CellValidator].
func (f CellValidatorFunc) ValidateCell(rec CellRecord) error { return f(rec) }

// AfterPutCellHook is called synchronously after a successful [Tx.PutCell] inside the
// current [DB.Update] callback. The written record is passed for inspection.
//
// A non-nil error is returned from [Tx.PutCell] to the caller. At hook time the logical
// write is staged in the current engine transaction. A hook error aborts [DB.Update]
// even if the callback returns nil. Use this for synchronous validation-adjacent reactions or
// metrics, and keep irreversible external side effects idempotent.
type AfterPutCellHook interface {
	AfterPutCell(ctx context.Context, rec CellRecord) error
}

// AfterPutCellHookFunc adapts a plain function to [AfterPutCellHook].
type AfterPutCellHookFunc func(ctx context.Context, rec CellRecord) error

// AfterPutCell implements [AfterPutCellHook].
func (f AfterPutCellHookFunc) AfterPutCell(ctx context.Context, rec CellRecord) error {
	return f(ctx, rec)
}

// AfterPutSeamHook is called synchronously after a successful [Tx.PutSeam] or
// [Tx.MarkConflict]/[Tx.MarkSupersedes] inside the current [DB.Update] callback.
// The written seam record is passed for inspection.
//
// A non-nil error is returned from the triggering write method. The write is
// staged at hook time. Return the error from the [DB.Update] callback to abort
// the transaction. Use this to collect metrics or to prepare work that the
// caller dispatches after commit confirmation.
type AfterPutSeamHook interface {
	AfterPutSeam(ctx context.Context, rec SeamRecord) error
}

// AfterPutSeamHookFunc adapts a plain function to [AfterPutSeamHook].
type AfterPutSeamHookFunc func(ctx context.Context, rec SeamRecord) error

// AfterPutSeam implements [AfterPutSeamHook].
func (f AfterPutSeamHookFunc) AfterPutSeam(ctx context.Context, rec SeamRecord) error {
	return f(ctx, rec)
}
