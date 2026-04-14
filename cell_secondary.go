package hexxladb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/record"
)

var emptySecondaryVal = []byte{}

func (tx *Tx) removeCellSecondaryIndex(rec record.CellRecord) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	sid := strings.TrimSpace(rec.Provenance.SourceID)
	if sid != "" {
		k, err := index.SourceKey(sid, rec.Key)
		if err != nil {
			return err
		}
		if err := tx.db.btree.Delete(k); err != nil {
			return err
		}
	}
	if b, ok := index.WeekBucketFromValidity(rec.Validity); ok {
		k := index.TimeKey(b, rec.Key)
		if err := tx.db.btree.Delete(k); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Tx) putCellSecondaryIndex(rec record.CellRecord) error {
	sid := strings.TrimSpace(rec.Provenance.SourceID)
	if sid != "" {
		k, err := index.SourceKey(sid, rec.Key)
		if err != nil {
			if errors.Is(err, index.ErrSourceIDTooLong) {
				return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
			}
			return err
		}
		if err := tx.Put(k, emptySecondaryVal); err != nil {
			return err
		}
	}
	if b, ok := index.WeekBucketFromValidity(rec.Validity); ok {
		k := index.TimeKey(b, rec.Key)
		if err := tx.Put(k, emptySecondaryVal); err != nil {
			return err
		}
	}
	return nil
}

// AscendCellsBySource scans the source/ secondary index for sourceID and calls fn with each
// decoded cell at that source (same PackedCoord as in the index key). ctx is checked between entries.
func (tx *Tx) AscendCellsBySource(ctx context.Context, sourceID string, fn func(record.CellRecord) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	from, to, err := index.SourceRangePrefix(sourceID)
	if err != nil {
		if errors.Is(err, index.ErrSourceIDTooLong) {
			return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
		}
		return err
	}
	return tx.AscendRange(from, to, func(k, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			return false
		}
		_, p, err := index.ParseSourceKey(k)
		if err != nil {
			return true
		}
		rec, ok, err := tx.GetCell(p)
		if err != nil || !ok {
			return true
		}
		return fn(rec)
	})
}

// AscendCellsInTimeBucket scans the time/ secondary index for the UTC week bucket (see [index.WeekBucketFromValidity])
// and calls fn with each decoded cell in that bucket.
func (tx *Tx) AscendCellsInTimeBucket(ctx context.Context, bucket int64, fn func(record.CellRecord) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	from, to := index.TimeRangePrefix(bucket)
	return tx.AscendRange(from, to, func(k, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			return false
		}
		_, p, err := index.ParseTimeKey(k)
		if err != nil {
			return true
		}
		rec, ok, err := tx.GetCell(p)
		if err != nil || !ok {
			return true
		}
		return fn(rec)
	})
}
