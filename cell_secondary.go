package hexxladb

// This file contains secondary-index builders and scan methods for cells. These
// Tx receiver methods access unexported transaction state and therefore remain in
// the root package; pure key encoding lives in internal/index.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

var emptySecondaryVal = []byte{}

// cellSourceSecondaryKey returns the source/ index key for rec at commitSeq, or (nil, nil) when no source row.
func (tx *Tx) cellSourceSecondaryKey(rec record.CellRecord, commitSeq uint64) ([]byte, error) {
	sid := strings.TrimSpace(rec.Provenance.SourceID)
	if sid == "" {
		return nil, nil
	}
	if tx.db.useMVCC {
		return index.SourceKeyWithVersion(sid, rec.Key, commitSeq)
	}
	return index.SourceKey(sid, rec.Key)
}

// cellTimeSecondaryKey returns the time/ index key when validity maps to a week bucket.
func (tx *Tx) cellTimeSecondaryKey(rec record.CellRecord, commitSeq uint64) ([]byte, bool) {
	b, ok := index.WeekBucketFromValidity(rec.Validity)
	if !ok {
		return nil, false
	}
	if tx.db.useMVCC {
		return index.TimeKeyWithVersion(b, rec.Key, commitSeq), true
	}
	return index.TimeKey(b, rec.Key), true
}

func (tx *Tx) cellTagSecondaryKey(tag string, key lattice.PackedCoord, commitSeq uint64) ([]byte, error) {
	if tx.db.useMVCC {
		return index.TagKeyWithVersion(tag, key, commitSeq)
	}
	return index.TagKey(tag, key)
}

func (tx *Tx) removeCellSecondaryIndex(rec record.CellRecord, commitSeq uint64) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	k, err := tx.cellSourceSecondaryKey(rec, commitSeq)
	if err != nil {
		return err
	}
	if k != nil {
		if err := tx.deleteDirect(k); err != nil {
			return err
		}
	}
	if tk, ok := tx.cellTimeSecondaryKey(rec, commitSeq); ok {
		if err := tx.deleteDirect(tk); err != nil {
			return err
		}
	}
	for _, tag := range record.UniqueSortedTags(rec.Tags) {
		k, err := tx.cellTagSecondaryKey(tag, rec.Key, commitSeq)
		if err != nil {
			return err
		}
		if err := tx.deleteDirect(k); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Tx) putCellSecondaryIndex(rec record.CellRecord, commitSeq uint64) error {
	k, err := tx.cellSourceSecondaryKey(rec, commitSeq)
	if err != nil {
		if errors.Is(err, index.ErrSourceIDTooLong) {
			return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
		}
		return err
	}
	if k != nil {
		if err := tx.putDirect(k, emptySecondaryVal); err != nil {
			return err
		}
	}
	if tk, ok := tx.cellTimeSecondaryKey(rec, commitSeq); ok {
		if err := tx.putDirect(tk, emptySecondaryVal); err != nil {
			return err
		}
	}
	for _, tag := range record.UniqueSortedTags(rec.Tags) {
		k, err := tx.cellTagSecondaryKey(tag, rec.Key, commitSeq)
		if err != nil {
			if errors.Is(err, index.ErrTagTooLong) {
				return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
			}
			return err
		}
		if err := tx.putDirect(k, emptySecondaryVal); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Tx) cellSourceScanBounds(sourceID string) (from, to []byte, err error) {
	if tx.db.useMVCC {
		return index.SourceRangePrefixAllVersions(sourceID)
	}
	return index.SourceRangePrefix(sourceID)
}

func (tx *Tx) cellTimeScanBounds(bucket int64) (from, to []byte) {
	if tx.db.useMVCC {
		return index.TimeRangePrefixAllVersions(bucket)
	}
	return index.TimeRangePrefix(bucket)
}

func (tx *Tx) cellTagScanBounds(tag string) (from, to []byte, err error) {
	if tx.db.useMVCC {
		return index.TagRangePrefixAllVersions(tag)
	}
	return index.TagRangePrefix(tag)
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
	from, to, err := tx.cellSourceScanBounds(sourceID)
	if err != nil {
		if errors.Is(err, index.ErrSourceIDTooLong) {
			return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
		}
		return err
	}
	seen := make(map[lattice.PackedCoord]struct{})
	var scanErr error
	err = tx.AscendRange(from, to, func(k, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		_, p, err := index.ParseSourceKey(k)
		if err != nil {
			return true
		}
		if _, dup := seen[p]; dup {
			return true
		}
		seen[p] = struct{}{}
		rec, ok, err := tx.GetCell(p)
		if err != nil || !ok {
			if err != nil {
				scanErr = err
				return false
			}
			return true
		}
		return fn(rec)
	})
	if err != nil {
		return err
	}
	return scanErr
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
	from, to := tx.cellTimeScanBounds(bucket)
	seen := make(map[lattice.PackedCoord]struct{})
	var scanErr error
	err := tx.AscendRange(from, to, func(k, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		_, p, err := index.ParseTimeKey(k)
		if err != nil {
			return true
		}
		if _, dup := seen[p]; dup {
			return true
		}
		seen[p] = struct{}{}
		rec, ok, err := tx.GetCell(p)
		if err != nil || !ok {
			if err != nil {
				scanErr = err
				return false
			}
			return true
		}
		return fn(rec)
	})
	if err != nil {
		return err
	}
	return scanErr
}

// AscendCellsByTag scans the tag/ secondary index for tag and calls fn with each decoded cell
// that lists that tag. ctx is checked between entries.
func (tx *Tx) AscendCellsByTag(ctx context.Context, tag string, fn func(record.CellRecord) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	from, to, err := tx.cellTagScanBounds(tag)
	if err != nil {
		if errors.Is(err, index.ErrTagTooLong) {
			return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
		}
		return err
	}
	seen := make(map[lattice.PackedCoord]struct{})
	var scanErr error
	err = tx.AscendRange(from, to, func(k, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		_, p, err := index.ParseTagKey(k)
		if err != nil {
			return true
		}
		if _, dup := seen[p]; dup {
			return true
		}
		seen[p] = struct{}{}
		rec, ok, err := tx.GetCell(p)
		if err != nil || !ok {
			if err != nil {
				scanErr = err
				return false
			}
			return true
		}
		return fn(rec)
	})
	if err != nil {
		return err
	}
	return scanErr
}

// AscendDistinctTags scans the tag/ secondary index and invokes fn once per distinct tag string
// that appears on the snapshot-visible cell for that index entry ([Tx.GetCell]).
// Physical secondary rows may survive past edits or across MVCC versions until prune; entries whose
// tag no longer appears on the visible cell are skipped. ctx is checked between calls to fn.
func (tx *Tx) AscendDistinctTags(ctx context.Context, fn func(tag string) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if fn == nil {
		return ErrNilCallback
	}
	from, to := index.TagFamilyScanBounds()
	type tagCoord struct {
		tag string
		p   lattice.PackedCoord
	}
	processed := make(map[tagCoord]struct{})
	emitted := make(map[string]struct{})
	var scanErr error
	err := tx.AscendRange(from, to, func(k, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		tag, p, err := index.ParseTagKey(k)
		if err != nil {
			return true
		}
		tc := tagCoord{tag: tag, p: p}
		if _, dup := processed[tc]; dup {
			return true
		}
		processed[tc] = struct{}{}
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			scanErr = err
			return false
		}
		if !ok || !slices.Contains(rec.Tags, tag) {
			return true
		}
		if _, dup := emitted[tag]; dup {
			return true
		}
		emitted[tag] = struct{}{}
		return fn(tag)
	})
	if err != nil {
		return err
	}
	return scanErr
}

// ListExistingTopics returns distinct tag strings from visible cells (sorted).
// Topics are stored as [record.CellRecord.Tags]; implementation uses the tag/ index plus
// [Tx.GetCell] to respect MVCC snapshots and stale secondaries.
func (tx *Tx) ListExistingTopics(ctx context.Context) ([]string, error) {
	var out []string
	err := tx.AscendDistinctTags(ctx, func(tag string) bool {
		out = append(out, tag)
		return true
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
