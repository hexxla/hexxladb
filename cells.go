package hexxladb

import (
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/changelog"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// MaxCellPlacementRadius is the largest radius accepted by
// [Tx.FindFreeCellPlacement]. A full search inspects at most 49,537 coordinates.
const MaxCellPlacementRadius = 128

// CellPlacement identifies a free coordinate selected around a semantic anchor.
// Probes is the number of occupied coordinates inspected before Coord and Key.
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
// The complete search disk must fit within the packable coordinate range.
func (tx *Tx) FindFreeCellPlacement(ctx context.Context, anchor Coord, maxRadius int) (CellPlacement, error) {
	if err := ctx.Err(); err != nil {
		return CellPlacement{}, err
	}
	if err := tx.requireWritable(); err != nil {
		return CellPlacement{}, err
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
		_, occupied, err := tx.GetCell(candidate.Packed)
		if err != nil {
			return CellPlacement{}, err
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
// Only allowed inside [DB.Update].
func (tx *Tx) PutCell(ctx context.Context, rec CellRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.requireWritable(); err != nil {
		return err
	}
	if err := invalidTypedRecord(validateCellInvariants(rec)); err != nil {
		return err
	}
	if v := tx.db.cellValidator; v != nil {
		if err := v.ValidateCell(rec); err != nil {
			return err
		}
	}
	data, err := record.EncodeCell(rec)
	if err != nil {
		return err
	}
	if !tx.db.useMVCC {
		old, had, err := tx.GetCell(rec.Key)
		if err != nil {
			return err
		}
		key := index.CellKey(rec.Key)
		if err := tx.putDirect(key, data); err != nil {
			return err
		}
		if had {
			if err := tx.removeCellSecondaryIndex(old, 0); err != nil {
				return err
			}
		}
		if err := tx.putCellSecondaryIndex(rec, 0); err != nil {
			return err
		}
		tx.noteChangelog(changelog.OpPutCell, key, data)
		if h := tx.db.afterPutCell; h != nil {
			return h.AfterPutCell(ctx, rec)
		}
		return nil
	}
	old, oldSeq, had, err := tx.visibleCellAndSeq(rec.Key)
	if err != nil {
		return err
	}
	key := index.CellKeyWithVersion(rec.Key, tx.writeSeq)
	if err := tx.putDirect(key, data); err != nil {
		return err
	}
	// Replace staged index entries for this commit. Older commits keep their
	// index entries until their primary versions are pruned.
	if had && oldSeq == tx.writeSeq {
		if err := tx.removeCellSecondaryIndex(old, oldSeq); err != nil {
			return err
		}
	}
	if err := tx.putCellSecondaryIndex(rec, tx.writeSeq); err != nil {
		return err
	}
	tx.cellOverlay[rec.Key] = data
	delete(tx.cellDeleted, rec.Key) // clear same-tx delete if re-putting
	tx.noteChangelog(changelog.OpPutCell, index.CellKey(rec.Key), data)
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
