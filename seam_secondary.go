// seam_secondary.go — secondary index key builders and scan methods for seams.
//
// These are *Tx receiver methods that access unexported fields (tx.db.useMVCC,
// tx.putDirect, tx.deleteDirect, tx.getSeamVisibleRaw) and therefore must
// remain in package hexxladb. Pure key-encoding logic lives in internal/index.
package hexxladb

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
	if tx.db.useMVCC {
		return index.SeamSourceKeyWithVersion(sid, rec.ID, commitSeq)
	}
	return index.SeamSourceKey(sid, rec.ID)
}

// seamTimeSecondaryKey returns the seam-time/ index key or (nil, nil) when validity has no week bucket.
func (tx *Tx) seamTimeSecondaryKey(rec record.SeamRecord, commitSeq uint64) ([]byte, error) {
	b, ok := index.WeekBucketFromValidity(rec.Validity)
	if !ok {
		return nil, nil
	}
	if tx.db.useMVCC {
		return index.SeamTimeKeyWithVersion(b, rec.ID, commitSeq)
	}
	return index.SeamTimeKey(b, rec.ID)
}

func (tx *Tx) removeSeamSecondaryIndex(rec record.SeamRecord, commitSeq uint64) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	k, err := tx.seamSourceSecondaryKey(rec, commitSeq)
	if err != nil {
		if errors.Is(err, index.ErrSourceIDTooLong) {
			return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
		}
		return err
	}
	if k != nil {
		if err := tx.deleteDirect(k); err != nil {
			return err
		}
	}
	tk, err := tx.seamTimeSecondaryKey(rec, commitSeq)
	if err != nil {
		return err
	}
	if tk != nil {
		if err := tx.deleteDirect(tk); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Tx) putSeamSecondaryIndex(rec record.SeamRecord, commitSeq uint64) error {
	k, err := tx.seamSourceSecondaryKey(rec, commitSeq)
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
	tk, err := tx.seamTimeSecondaryKey(rec, commitSeq)
	if err != nil {
		return err
	}
	if tk != nil {
		if err := tx.putDirect(tk, emptySecondaryVal); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Tx) seamSourceScanBounds(sourceID string) (from, to []byte, err error) {
	if tx.db.useMVCC {
		return index.SeamSourceRangePrefixAllVersions(sourceID)
	}
	return index.SeamSourceRangePrefix(sourceID)
}

func (tx *Tx) seamTimeScanBounds(bucket int64) (from, to []byte) {
	if tx.db.useMVCC {
		return index.SeamTimeRangePrefixAllVersions(bucket)
	}
	return index.SeamTimeRangePrefix(bucket)
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
			return true
		}
		return fn(rec)
	})
	if err != nil {
		return err
	}
	return scanErr
}
