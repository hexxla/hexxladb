package hexxladb

import "github.com/hexxla/hexxladb/internal/record"

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
