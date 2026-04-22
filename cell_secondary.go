package hexxladb

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

var emptySecondaryVal = []byte{}

func (tx *Tx) removeCellSecondaryIndex(rec record.CellRecord, commitSeq uint64) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	sid := strings.TrimSpace(rec.Provenance.SourceID)
	if sid != "" {
		var k []byte
		var err error
		if tx.db.useMVCC {
			k, err = index.SourceKeyWithVersion(sid, rec.Key, commitSeq)
		} else {
			k, err = index.SourceKey(sid, rec.Key)
		}
		if err != nil {
			return err
		}
		if err := tx.db.btree.Delete(k); err != nil {
			return err
		}
	}
	if b, ok := index.WeekBucketFromValidity(rec.Validity); ok {
		var k []byte
		if tx.db.useMVCC {
			k = index.TimeKeyWithVersion(b, rec.Key, commitSeq)
		} else {
			k = index.TimeKey(b, rec.Key)
		}
		if err := tx.db.btree.Delete(k); err != nil {
			return err
		}
	}
	for _, tag := range uniqueSortedTags(rec.Tags) {
		var k []byte
		var err error
		if tx.db.useMVCC {
			k, err = index.TagKeyWithVersion(tag, rec.Key, commitSeq)
		} else {
			k, err = index.TagKey(tag, rec.Key)
		}
		if err != nil {
			return err
		}
		if err := tx.db.btree.Delete(k); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Tx) putCellSecondaryIndex(rec record.CellRecord, commitSeq uint64) error {
	sid := strings.TrimSpace(rec.Provenance.SourceID)
	if sid != "" {
		var k []byte
		var err error
		if tx.db.useMVCC {
			k, err = index.SourceKeyWithVersion(sid, rec.Key, commitSeq)
		} else {
			k, err = index.SourceKey(sid, rec.Key)
		}
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
		var k []byte
		if tx.db.useMVCC {
			k = index.TimeKeyWithVersion(b, rec.Key, commitSeq)
		} else {
			k = index.TimeKey(b, rec.Key)
		}
		if err := tx.Put(k, emptySecondaryVal); err != nil {
			return err
		}
	}
	for _, tag := range uniqueSortedTags(rec.Tags) {
		var k []byte
		var err error
		if tx.db.useMVCC {
			k, err = index.TagKeyWithVersion(tag, rec.Key, commitSeq)
		} else {
			k, err = index.TagKey(tag, rec.Key)
		}
		if err != nil {
			if errors.Is(err, index.ErrTagTooLong) {
				return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
			}
			return err
		}
		if err := tx.Put(k, emptySecondaryVal); err != nil {
			return err
		}
	}
	return nil
}

// uniqueSortedTags returns deduplicated trimmed non-empty tags in stable sorted order for deterministic index writes.
func uniqueSortedTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	var tmp []string
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		tmp = append(tmp, t)
	}
	sort.Strings(tmp)
	return tmp
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
	var from, to []byte
	var err error
	if tx.db.useMVCC {
		from, to, err = index.SourceRangePrefixAllVersions(sourceID)
	} else {
		from, to, err = index.SourceRangePrefix(sourceID)
	}
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
	var from, to []byte
	if tx.db.useMVCC {
		from, to = index.TimeRangePrefixAllVersions(bucket)
	} else {
		from, to = index.TimeRangePrefix(bucket)
	}
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
	var from, to []byte
	var err error
	if tx.db.useMVCC {
		from, to, err = index.TagRangePrefixAllVersions(tag)
	} else {
		from, to, err = index.TagRangePrefix(tag)
	}
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
