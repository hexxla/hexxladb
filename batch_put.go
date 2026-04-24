package hexxladb

import (
	"context"

	"github.com/hexxla/hexxladb/internal/record"
)

// BatchPutCellOptions configures [DB.BatchPutCells].
type BatchPutCellOptions struct {
	// BatchSize controls how many cells are written per transaction. Default 128.
	BatchSize int
	// OnProgress is called after each successfully written cell with the 0-based index.
	OnProgress func(index int)
	// ContinueOnError, when true, skips cells that fail validation or encoding
	// and continues with the remaining cells. Errors are collected and returned
	// at the end via [BatchPutCellResult.Errors].
	ContinueOnError bool
}

// BatchPutCellError pairs a cell index with the error that caused it to fail.
type BatchPutCellError struct {
	Index int
	Err   error
}

func (e BatchPutCellError) Error() string { return e.Err.Error() }
func (e BatchPutCellError) Unwrap() error { return e.Err }

// BatchPutCellResult summarizes one [DB.BatchPutCells] invocation.
type BatchPutCellResult struct {
	Written int                 // cells successfully committed
	Errors  []BatchPutCellError // per-cell errors (only when ContinueOnError is true)
}

// BatchPutCells writes multiple cells in batched transactions with optional progress.
// Each batch commits BatchSize cells in a single [DB.Update] call.
func (db *DB) BatchPutCells(ctx context.Context, cells []record.CellRecord, opts *BatchPutCellOptions) (BatchPutCellResult, error) {
	batchSize := 128
	var onProgress func(int)
	continueOnError := false
	if opts != nil {
		if opts.BatchSize > 0 {
			batchSize = opts.BatchSize
		}
		onProgress = opts.OnProgress
		continueOnError = opts.ContinueOnError
	}

	var result BatchPutCellResult
	for start := 0; start < len(cells); start += batchSize {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		end := min(start+batchSize, len(cells))
		batch := cells[start:end]
		err := db.Update(func(tx *Tx) error {
			for i, rec := range batch {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := tx.PutCell(ctx, rec); err != nil {
					if continueOnError {
						result.Errors = append(result.Errors, BatchPutCellError{
							Index: start + i,
							Err:   err,
						})
						continue
					}
					return err
				}
				result.Written++
				if onProgress != nil {
					onProgress(start + i)
				}
			}
			return nil
		})
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
