package hexxladb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/record"
)

func (tx *Tx) removeSeamSecondaryIndex(rec record.SeamRecord, commitSeq uint64) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	sid := strings.TrimSpace(rec.Provenance.SourceID)
	if sid != "" {
		var (
			k   []byte
			err error
		)
		if tx.db.useMVCC {
			k, err = index.SeamSourceKeyWithVersion(sid, rec.ID, commitSeq)
		} else {
			k, err = index.SeamSourceKey(sid, rec.ID)
		}
		if err != nil {
			if errors.Is(err, index.ErrSourceIDTooLong) {
				return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
			}
			return err
		}
		if err := tx.db.btree.Delete(k); err != nil {
			return err
		}
	}
	if b, ok := index.WeekBucketFromValidity(rec.Validity); ok {
		var (
			k   []byte
			err error
		)
		if tx.db.useMVCC {
			k, err = index.SeamTimeKeyWithVersion(b, rec.ID, commitSeq)
		} else {
			k, err = index.SeamTimeKey(b, rec.ID)
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

func (tx *Tx) putSeamSecondaryIndex(rec record.SeamRecord, commitSeq uint64) error {
	sid := strings.TrimSpace(rec.Provenance.SourceID)
	if sid != "" {
		var (
			k   []byte
			err error
		)
		if tx.db.useMVCC {
			k, err = index.SeamSourceKeyWithVersion(sid, rec.ID, commitSeq)
		} else {
			k, err = index.SeamSourceKey(sid, rec.ID)
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
		var (
			k   []byte
			err error
		)
		if tx.db.useMVCC {
			k, err = index.SeamTimeKeyWithVersion(b, rec.ID, commitSeq)
		} else {
			k, err = index.SeamTimeKey(b, rec.ID)
		}
		if err != nil {
			return err
		}
		if err := tx.Put(k, emptySecondaryVal); err != nil {
			return err
		}
	}
	return nil
}

// AscendSeamsBySource scans the seam-source/ secondary index for sourceID and loads each seam primary by ULID.
func (tx *Tx) AscendSeamsBySource(ctx context.Context, sourceID string, fn func(record.SeamRecord) bool) error {
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
		from, to, err = index.SeamSourceRangePrefixAllVersions(sourceID)
	} else {
		from, to, err = index.SeamSourceRangePrefix(sourceID)
	}
	if err != nil {
		if errors.Is(err, index.ErrSourceIDTooLong) {
			return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
		}
		return err
	}
	seen := make(map[string]struct{})
	var scanErr error
	err = tx.AscendRange(from, to, func(k, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		_, ulidStr, err := index.ParseSeamSourceKey(k)
		if err != nil {
			return true
		}
		if _, dup := seen[ulidStr]; dup {
			return true
		}
		seen[ulidStr] = struct{}{}
		raw, _, ok, err := tx.getSeamVisibleRaw(ulidStr)
		if err != nil || !ok {
			if err != nil {
				scanErr = err
				return false
			}
			return true
		}
		rec, err := record.DecodeSeam(raw)
		if err != nil {
			return true
		}
		return fn(rec)
	})
	if err != nil {
		return err
	}
	return scanErr
}

// AscendSeamsInTimeBucket scans the seam-time/ secondary index for the UTC week bucket and loads each seam primary.
func (tx *Tx) AscendSeamsInTimeBucket(ctx context.Context, bucket int64, fn func(record.SeamRecord) bool) error {
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
		from, to = index.SeamTimeRangePrefixAllVersions(bucket)
	} else {
		from, to = index.SeamTimeRangePrefix(bucket)
	}
	seen := make(map[string]struct{})
	var scanErr error
	err := tx.AscendRange(from, to, func(k, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		_, ulidStr, err := index.ParseSeamTimeKey(k)
		if err != nil {
			return true
		}
		if _, dup := seen[ulidStr]; dup {
			return true
		}
		seen[ulidStr] = struct{}{}
		raw, _, ok, err := tx.getSeamVisibleRaw(ulidStr)
		if err != nil || !ok {
			if err != nil {
				scanErr = err
				return false
			}
			return true
		}
		rec, err := record.DecodeSeam(raw)
		if err != nil {
			return true
		}
		return fn(rec)
	})
	if err != nil {
		return err
	}
	return scanErr
}
