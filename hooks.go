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
// write is staged in the current engine transaction but is not yet durable. Propagating
// the error out of the [DB.Update] callback aborts the transaction; swallowing it allows
// the staged write to commit. Use this for synchronous validation-adjacent reactions or
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
// A non-nil error is returned from the triggering write method. As with
// [AfterPutCellHook], the write is only staged at hook time; propagating the error out
// of [DB.Update] aborts it. Use this to react to new seam detection, such as collecting
// metrics or scheduling an idempotent review workflow after commit confirmation.
type AfterPutSeamHook interface {
	AfterPutSeam(ctx context.Context, rec SeamRecord) error
}

// AfterPutSeamHookFunc adapts a plain function to [AfterPutSeamHook].
type AfterPutSeamHookFunc func(ctx context.Context, rec SeamRecord) error

// AfterPutSeam implements [AfterPutSeamHook].
func (f AfterPutSeamHookFunc) AfterPutSeam(ctx context.Context, rec SeamRecord) error {
	return f(ctx, rec)
}
