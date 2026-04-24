package hexxladb

import (
	"context"
	"encoding/json"
	"io"

	"github.com/hexxla/hexxladb/internal/record"
)

// ExportCellsJSON writes all visible cells as a JSON array to w.
// Each element is a record.CellRecord encoded as JSON.
func (tx *Tx) ExportCellsJSON(ctx context.Context, center Coord, maxR int, w io.Writer) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if tx == nil || tx.db == nil {
		return 0, ErrClosed
	}
	enc := json.NewEncoder(w)
	if _, err := w.Write([]byte("[\n")); err != nil {
		return 0, err
	}
	coords := WalkRings(nil, center, maxR)
	n := 0
	for _, c := range coords {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		p := mustPack(c)
		rec, ok, err := tx.GetCell(p)
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

// ImportCellsJSON reads a JSON array of record.CellRecord from r and writes them via PutCell.
// Returns the number of cells successfully imported.
func (db *DB) ImportCellsJSON(ctx context.Context, r io.Reader) (int, error) {
	if db == nil {
		return 0, ErrDatabaseClosed
	}
	var cells []record.CellRecord
	if err := json.NewDecoder(r).Decode(&cells); err != nil {
		return 0, err
	}
	result, err := db.BatchPutCells(ctx, cells, &BatchPutCellOptions{BatchSize: 128})
	if err != nil {
		return result.Written, err
	}
	return result.Written, nil
}
