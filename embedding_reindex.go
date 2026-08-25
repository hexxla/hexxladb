package hexxladb

import (
	"bytes"
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// EmbeddingFunc computes a vector embedding for a cell record. Implementations
// typically call an external model or service. Return nil to skip the cell.
type EmbeddingFunc func(ctx context.Context, rec record.CellRecord) ([]float32, error)

// ReindexEmbeddings recomputes embeddings for all cells by calling fn for each
// cell visible in the transaction. Existing embeddings are replaced; cells for
// which fn returns nil are skipped (their old embedding is removed).
//
// This is a bulk operation intended for model changes — it scans the entire
// cell/ keyspace. When no embedding dimension exists, the first non-nil vector
// establishes it through [Tx.PutEmbedding]. Only allowed inside [DB.Update].
func (tx *Tx) ReindexEmbeddings(ctx context.Context, fn EmbeddingFunc) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrInvalidArgument)
	}
	if fn == nil {
		return ErrNilCallback
	}

	// Collect cell coords first to avoid mutation during iteration.
	var coords []lattice.PackedCoord
	var scanErr error
	from := []byte(index.CellPrefix)
	to := cellPrefixEnd()
	err := tx.db.btree.AscendRange(from, to, func(k, _ []byte) bool {
		if !bytes.HasPrefix(k, []byte(index.CellPrefix)) {
			return false
		}
		logicalKeyLength := len(index.CellPrefix) + index.PackedCoordKeyLen
		if len(k) < logicalKeyLength {
			scanErr = fmt.Errorf("%w: malformed cell key during embedding reindex", ErrCorruptDatabase)
			return false
		}
		coord, parseErr := index.ParseCellKey(k[:logicalKeyLength])
		if parseErr != nil {
			scanErr = fmt.Errorf("%w: malformed cell key during embedding reindex: %w", ErrCorruptDatabase, parseErr)
			return false
		}
		// Deduplicate (MVCC may have multiple version-suffixed keys per logical cell).
		if len(coords) > 0 && coords[len(coords)-1] == coord {
			return true
		}
		coords = append(coords, coord)
		return true
	})
	if err != nil {
		return err
	}
	if scanErr != nil {
		return scanErr
	}

	for _, coord := range coords {
		if err := ctx.Err(); err != nil {
			return err
		}
		rec, ok, err := tx.GetCell(coord)
		if err != nil {
			return err
		}
		if !ok {
			// Cell deleted or tombstoned; remove stale embedding.
			if err := tx.deleteEmbeddingIfEnabled(coord); err != nil {
				return err
			}
			continue
		}
		vec, fnErr := fn(ctx, rec)
		if fnErr != nil {
			return fnErr
		}
		if vec == nil {
			// fn chose to skip — remove existing embedding.
			if err := tx.deleteEmbeddingIfEnabled(coord); err != nil {
				return err
			}
			continue
		}
		if err := tx.PutEmbedding(coord, vec); err != nil {
			return err
		}
	}
	return nil
}

// cellPrefixEnd returns the exclusive upper bound for a cell/ prefix scan.
func cellPrefixEnd() []byte {
	end := append([]byte(nil), index.CellPrefix...)
	end[len(end)-1]++
	return end
}
