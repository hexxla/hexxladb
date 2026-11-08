package hexxladb

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// ── Batch write ───────────────────────────────────────────────────────────────

// BatchPutCellOptions configures [DB.BatchPutCells].
type BatchPutCellOptions struct {
	// BatchSize controls how many cells are written per transaction. Default 128.
	BatchSize int
	// OnProgress is called after each durably committed cell with the 0-based index.
	OnProgress func(index int)
	// ContinueOnError, when true, skips cells rejected before any cell changes are staged.
	// Storage and post-write hook failures abort the current batch. Errors are collected and returned
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
func (db *DB) BatchPutCells(ctx context.Context, cells []CellRecord, opts *BatchPutCellOptions) (BatchPutCellResult, error) {
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
		batchWritten := 0
		readyToCommit := false
		progressIndices := make([]int, 0, len(batch))
		err := db.Update(func(tx *Tx) error {
			for i, rec := range batch {
				if err := ctx.Err(); err != nil {
					return err
				}
				if rejected, err := tx.putCell(ctx, rec); err != nil {
					if continueOnError && rejected {
						result.Errors = append(result.Errors, BatchPutCellError{
							Index: start + i,
							Err:   err,
						})
						continue
					}
					return err
				}
				batchWritten++
				progressIndices = append(progressIndices, start+i)
			}
			if err := cmp.Or(ctx.Err(), tx.cellWriteErr); err != nil {
				return err
			}
			readyToCommit = true
			return nil
		})
		if err != nil && (!readyToCommit || !errors.Is(err, ErrCommitDurable)) {
			return result, err
		}
		result.Written += batchWritten
		if onProgress != nil {
			for _, index := range progressIndices {
				onProgress(index)
			}
		}
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

// ── Bulk JSON import / export ─────────────────────────────────────────────────

// ExportCellsJSON writes all visible cells as a JSON array to w.
// Each element is a [CellRecord] encoded as JSON.
func (tx *Tx) ExportCellsJSON(ctx context.Context, center Coord, maxR int, w io.Writer) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if tx == nil || tx.db == nil {
		return 0, ErrClosed
	}
	if err := validatePackedRadius(center, maxR); err != nil {
		return 0, err
	}
	enc := json.NewEncoder(w)
	if _, err := w.Write([]byte("[\n")); err != nil {
		return 0, err
	}
	n := 0
	for cp := range lattice.WalkRingsPackedSeq(center, maxR) {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		rec, ok, err := tx.GetCell(cp.Packed)
		if err != nil {
			return n, err
		}
		if !ok {
			continue
		}
		if n > 0 {
			if _, err := w.Write([]byte(",\n")); err != nil {
				return n, err
			}
		}
		if err := enc.Encode(rec); err != nil {
			return n, err
		}
		n++
	}
	if _, err := w.Write([]byte("]\n")); err != nil {
		return n, err
	}
	return n, nil
}

// ImportCellsJSON reads a JSON array of [CellRecord] values from r and writes them via [Tx.PutCell].
// Returns the number of cells successfully imported.
func (db *DB) ImportCellsJSON(ctx context.Context, r io.Reader) (int, error) {
	if db == nil {
		return 0, ErrDatabaseClosed
	}
	dec := json.NewDecoder(r)
	token, err := dec.Token()
	if err != nil {
		return 0, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '[' {
		return 0, fmt.Errorf("%w: cell import must be a JSON array", ErrInvalidArgument)
	}

	const batchSize = 128
	written := 0
	batch := make([]record.CellRecord, 0, batchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		result, err := db.BatchPutCells(ctx, batch, &BatchPutCellOptions{BatchSize: batchSize})
		written += result.Written
		batch = batch[:0]
		return err
	}
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		var cell record.CellRecord
		if err := dec.Decode(&cell); err != nil {
			return written, err
		}
		batch = append(batch, cell)
		if len(batch) == batchSize {
			if err := flush(); err != nil {
				return written, err
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return written, err
	}
	if err := flush(); err != nil {
		return written, err
	}
	if token, err := dec.Token(); err != io.EOF {
		if err != nil {
			return written, err
		}
		return written, fmt.Errorf("%w: unexpected trailing JSON token %v", ErrInvalidArgument, token)
	}
	return written, nil
}
