package hexxladb

import (
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/changelog"
	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// MaxCellPlacementRadius is the largest radius accepted by
// [Tx.FindFreeCellPlacement]. A full search inspects at most 49,537 coordinates.
const MaxCellPlacementRadius = 128

// CellPlacement identifies a free coordinate selected around a semantic anchor.
// Probes counts coordinates skipped before Coord and Key.
type CellPlacement struct {
	Coord  Coord
	Key    PackedCoord
	Probes int
}

// FindFreeCellPlacement finds the first coordinate without a visible cell in
// deterministic ring order around anchor, up to and including maxRadius.
// It is writable-transaction-only so callers can pass the returned Key directly
// to [Tx.PutCell] before the [DB.Update] callback returns without another writer
// claiming it first. The result is not reserved inside the transaction: call
// PutCell before searching again when distinct placements are required.
//
// A tombstoned MVCC coordinate has no visible cell and may be selected; writing
// there creates a new version while historical snapshots retain the old value.
// Use [Tx.FindUnusedCellPlacement] to avoid reusing recorded tombstones.
// The complete search disk must fit within the packable coordinate range.
func (tx *Tx) FindFreeCellPlacement(ctx context.Context, anchor Coord, maxRadius int) (CellPlacement, error) {
	return tx.findCellPlacement(ctx, anchor, maxRadius, false)
}

// FindUnusedCellPlacement is like [Tx.FindFreeCellPlacement] but skips coordinates
// with a recorded MVCC cell or tombstone, including deletions in the current update.
// Use it for new logical identities that must stay distinct from deleted cells.
// Returns [ErrMVCCRequired] on single-version databases. Only recorded cell
// history is checked; callers own references to coordinates that never had a cell.
func (tx *Tx) FindUnusedCellPlacement(ctx context.Context, anchor Coord, maxRadius int) (CellPlacement, error) {
	return tx.findCellPlacement(ctx, anchor, maxRadius, true)
}

func (tx *Tx) findCellPlacement(ctx context.Context, anchor Coord, maxRadius int, skipTombstones bool) (CellPlacement, error) {
	if err := ctx.Err(); err != nil {
		return CellPlacement{}, err
	}
	if err := tx.requireWritable(); err != nil {
		return CellPlacement{}, err
	}
	if skipTombstones && !tx.db.useMVCC {
		return CellPlacement{}, ErrMVCCRequired
	}
	if maxRadius < 0 || maxRadius > MaxCellPlacementRadius {
		return CellPlacement{}, fmt.Errorf(
			"%w: cell placement radius must be between 0 and %d",
			ErrInvalidArgument,
			MaxCellPlacementRadius,
		)
	}
	if err := validatePackedRadius(anchor, maxRadius); err != nil {
		return CellPlacement{}, err
	}

	probes := 0
	for candidate := range lattice.WalkRingsPackedSeq(anchor, maxRadius) {
		if err := ctx.Err(); err != nil {
			return CellPlacement{}, err
		}
		raw, seq, live, err := tx.getCellVisibleRaw(candidate.Packed)
		occupied := live || (skipTombstones && (seq != 0 || tx.cellDeleted[candidate.Packed]))
		if err != nil {
			return CellPlacement{}, err
		}
		if live {
			if _, err := record.DecodeCell(raw); err != nil {
				return CellPlacement{}, err
			}
		}
		if !occupied {
			return CellPlacement{
				Coord:  candidate.Coord,
				Key:    candidate.Packed,
				Probes: probes,
			}, nil
		}
		probes++
	}
	return CellPlacement{}, fmt.Errorf(
		"%w: anchor=(%d,%d) radius=%d",
		ErrNoFreeCellPlacement,
		anchor.Q,
		anchor.R,
		maxRadius,
	)
}

// PutCell writes a cell record at cell/<packed> for rec.Key (v1) or version-suffixed keys (MVCC).
// It maintains source/, time/, and tag/ secondary indexes (see [Tx.AscendCellsBySource],
// [Tx.AscendCellsInTimeBucket], [Tx.AscendCellsByTag]).
// Only allowed inside [DB.Update]. A storage or post-write hook error aborts
// the update even if its callback returns nil.
func (tx *Tx) PutCell(ctx context.Context, rec CellRecord) error {
	_, err := tx.putCell(ctx, rec)
	return err
}

// putCell reports whether a rejection occurred before any cell changes were staged.
func (tx *Tx) putCell(ctx context.Context, rec CellRecord) (rejected bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := tx.requireWritable(); err != nil {
		return false, err
	}
	if tx.stagedWriteErr != nil {
		return false, tx.stagedWriteErr
	}
	if err := invalidTypedRecord(validateCellInvariants(rec)); err != nil {
		return true, err
	}
	if v := tx.db.cellValidator; v != nil {
		if err := v.ValidateCell(rec); err != nil {
			return true, err
		}
	}
	data, err := record.EncodeCell(rec)
	if err != nil {
		return true, err
	}
	if uint64(len(data)) > uint64(tx.db.eng.MaxValueBytes()) {
		return true, invalidTypedRecord(engine.ErrValueTooLarge)
	}
	var secondary [][]byte
	if err := tx.visitCellSecondaryKeys(rec, tx.writeSeq, func(key []byte) error {
		secondary = append(secondary, key)
		return nil
	}); err != nil {
		return true, err
	}

	err = tx.stageCell(ctx, rec, data, secondary)
	if err != nil {
		tx.stagedWriteErr = err
	}
	return false, err
}

func (tx *Tx) stageCell(ctx context.Context, rec CellRecord, data []byte, secondary [][]byte) error {
	old, oldSeq, had, err := tx.visibleCellAndSeq(rec.Key)
	if err != nil {
		return err
	}
	logicalKey := index.CellKey(rec.Key)
	key := logicalKey
	if tx.db.useMVCC {
		key = index.CellKeyWithVersion(rec.Key, tx.writeSeq)
	}
	if err := tx.putDirect(key, data); err != nil {
		return err
	}
	// Replace same-commit index entries. Earlier MVCC commits retain their indexes.
	if had && oldSeq == tx.writeSeq {
		if err := tx.removeCellSecondaryIndex(old, oldSeq); err != nil {
			return err
		}
	}
	for _, key := range secondary {
		if err := tx.putDirect(key, emptySecondaryVal); err != nil {
			return err
		}
	}
	if tx.db.useMVCC {
		tx.cellOverlay[rec.Key] = data
		delete(tx.cellDeleted, rec.Key)
	}
	tx.noteChangelog(changelog.OpPutCell, logicalKey, data)
	if h := tx.db.afterPutCell; h != nil {
		return h.AfterPutCell(ctx, rec)
	}
	return nil
}

// GetCell returns the decoded cell visible at the transaction's snapshot, or (zero, false, nil) if missing.
func (tx *Tx) GetCell(key PackedCoord) (CellRecord, bool, error) {
	if tx == nil || tx.db == nil {
		return record.CellRecord{}, false, ErrClosed
	}
	raw, _, ok, err := tx.getCellVisibleRaw(key)
	if err != nil || !ok {
		return record.CellRecord{}, ok, err
	}
	rec, err := record.DecodeCell(raw)
	if err != nil {
		return record.CellRecord{}, false, err
	}
	return rec, true, nil
}
