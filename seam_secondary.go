package hexxladb

// This file contains secondary-index builders and scan methods for seams. These
// Tx receiver methods access unexported transaction state and therefore remain in
// the root package; pure key encoding lives in internal/index.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/record"
)

// seamSourceSecondaryKey returns the seam-source/ index key or (nil, nil) when no provenance source.
func (tx *Tx) seamSourceSecondaryKey(rec record.SeamRecord, commitSeq uint64) ([]byte, error) {
	sid := strings.TrimSpace(rec.Provenance.SourceID)
	if sid == "" {
		return nil, nil
	}
	return index.SeamSourceKeyWithVersion(sid, rec.ID, commitSeq)
}

// seamTimeSecondaryKey returns the seam-time/ index key or (nil, nil) when validity has no week bucket.
func (tx *Tx) seamTimeSecondaryKey(rec record.SeamRecord, commitSeq uint64) ([]byte, error) {
	b, ok := index.WeekBucketFromValidity(rec.Validity)
	if !ok {
		return nil, nil
	}
	return index.SeamTimeKeyWithVersion(b, rec.ID, commitSeq)
}

func (tx *Tx) visitSeamSecondaryKeys(rec record.SeamRecord, commitSeq uint64, visit func([]byte) error) error {
	k, err := tx.seamSourceSecondaryKey(rec, commitSeq)
	if err != nil {
		if errors.Is(err, index.ErrSourceIDTooLong) {
			return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
		}
		return err
	}
	if k != nil {
		if err := visit(k); err != nil {
			return err
		}
	}
	tk, err := tx.seamTimeSecondaryKey(rec, commitSeq)
	if err != nil {
		return err
	}
	if tk != nil {
		return visit(tk)
	}
	return nil
}

func (tx *Tx) removeSeamSecondaryIndex(rec record.SeamRecord, commitSeq uint64) error {
	return tx.visitSeamSecondaryKeys(rec, commitSeq, tx.deleteDirect)
}

func (tx *Tx) putSeamSecondaryIndex(rec record.SeamRecord, commitSeq uint64) error {
	return tx.visitSeamSecondaryKeys(rec, commitSeq, func(key []byte) error {
		return tx.putDirect(key, emptySecondaryVal)
	})
}

func (tx *Tx) seamSourceScanBounds(sourceID string) (from, to []byte, err error) {
	return index.SeamSourceRangePrefixAllVersions(sourceID)
}

func (tx *Tx) seamTimeScanBounds(bucket int64) (from, to []byte) {
	return index.SeamTimeRangePrefixAllVersions(bucket)
}

// AscendSeamsBySource scans the seam-source/ secondary index for sourceID and loads each seam primary by ULID.
func (tx *Tx) AscendSeamsBySource(ctx context.Context, sourceID string, fn func(SeamRecord) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	from, to, err := tx.seamSourceScanBounds(sourceID)
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
			scanErr = fmt.Errorf("%w: decode source-indexed seam: %w", ErrCorruptDatabase, err)
			return false
		}
		if strings.TrimSpace(rec.Provenance.SourceID) != sourceID {
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
func (tx *Tx) AscendSeamsInTimeBucket(ctx context.Context, bucket int64, fn func(SeamRecord) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	from, to := tx.seamTimeScanBounds(bucket)
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
			scanErr = fmt.Errorf("%w: decode time-indexed seam: %w", ErrCorruptDatabase, err)
			return false
		}
		if visibleBucket, hasBucket := index.WeekBucketFromValidity(rec.Validity); !hasBucket || visibleBucket != bucket {
			return true
		}
		return fn(rec)
	})
	if err != nil {
		return err
	}
	return scanErr
}
