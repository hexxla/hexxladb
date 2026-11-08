package hexxladb

import (
	"bytes"
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/record"
)

// AscendSeams visits the seam records visible to this transaction in ID order.
// The callback can stop the scan by returning false. MVCC scans visit one
// version per seam ID. ctx is checked between physical rows.
func (tx *Tx) AscendSeams(ctx context.Context, visit func(SeamRecord) bool) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if visit == nil {
		return ErrNilCallback
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx.db.useMVCC {
		return tx.ascendVersionedSeams(ctx, visit)
	}
	return tx.scanSeamPrimaries(ctx, func(key, value []byte) (bool, error) {
		seam, err := decodeScannedSeam(value)
		if err != nil {
			return false, err
		}
		expectedKey, err := index.SeamKey(seam.ID)
		if err != nil || !bytes.Equal(key, expectedKey) {
			return false, fmt.Errorf("%w: seam primary key does not match encoded record ID", ErrCorruptDatabase)
		}
		return visit(seam), nil
	})
}

func (tx *Tx) ascendVersionedSeams(ctx context.Context, visit func(SeamRecord) bool) error {
	readSeq := max(tx.readSeq, tx.writeSeq)
	var currentID string
	var visible []byte
	stopped := false
	flush := func() (bool, error) {
		if len(visible) == 0 {
			return true, nil
		}
		seam, err := decodeScannedSeam(visible)
		if err != nil {
			return false, err
		}
		if seam.ID != currentID {
			return false, fmt.Errorf("%w: seam primary key does not match encoded record ID", ErrCorruptDatabase)
		}
		return visit(seam), nil
	}

	err := tx.scanSeamPrimaries(ctx, func(key, value []byte) (bool, error) {
		id, seq, err := index.ParseSeamVersionKey(key)
		if err != nil {
			return false, fmt.Errorf("%w: parse seam primary key: %w", ErrCorruptDatabase, err)
		}
		if id != currentID {
			more, err := flush()
			if !more {
				stopped = true
				return false, err
			}
			currentID, visible = id, nil
		}
		// Rows for an ID are in commit order. Keep just the last visible row.
		if seq <= readSeq {
			visible = value
		}
		return true, nil
	})
	if err != nil || stopped {
		return err
	}
	_, err = flush()
	return err
}

func (tx *Tx) scanSeamPrimaries(ctx context.Context, visit func(key, value []byte) (bool, error)) error {
	var scanErr error
	prefix := []byte(index.SeamPrefix)
	err := tx.AscendRange(prefix, index.SeamScanUpperBound(), func(k, v []byte) bool {
		if !bytes.HasPrefix(k, prefix) {
			return false
		}
		if scanErr = ctx.Err(); scanErr != nil {
			return false
		}
		more, err := visit(k, v)
		scanErr = err
		return more
	})
	if err != nil {
		return err
	}
	return scanErr
}

func decodeScannedSeam(value []byte) (SeamRecord, error) {
	seam, err := record.DecodeSeam(value)
	if err != nil {
		return SeamRecord{}, fmt.Errorf("%w: decode visible seam: %w", ErrCorruptDatabase, err)
	}
	if err := validateSeamInvariants(seam); err != nil {
		return SeamRecord{}, fmt.Errorf("%w: invalid visible seam: %w", ErrCorruptDatabase, err)
	}
	return seam, nil
}
