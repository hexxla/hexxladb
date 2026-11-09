package hexxladb

import (
	"encoding/binary"
	"fmt"

	"github.com/hexxla/hexxladb/internal/index"
)

func (db *DB) loadSnapshotFloor(head uint64) error {
	value, found, err := db.btree.Get([]byte(index.MVCCSnapshotFloorKey))
	if err != nil {
		return mapEngineDataError(err)
	}
	if !found {
		// Legacy pruning left no watermark. The current view is the only
		// baseline that can be trusted when opening an unmarked database.
		db.snapshotFloor = head
		return nil
	}
	if len(value) != 8 || binary.BigEndian.Uint64(value) > head {
		return fmt.Errorf("%w: invalid MVCC snapshot floor", ErrCorruptDatabase)
	}
	db.snapshotFloor = binary.BigEndian.Uint64(value)
	db.snapshotFloorStored = true
	return nil
}

func (tx *Tx) stageSnapshotFloor(seq uint64) error {
	value := binary.BigEndian.AppendUint64(nil, seq)
	if err := tx.putDirect([]byte(index.MVCCSnapshotFloorKey), value); err != nil {
		return fmt.Errorf("%w: stage snapshot retention boundary: %w", ErrCommitFinalization, err)
	}
	return nil
}

func (db *DB) checkSnapshotFloor(seq uint64) error {
	if db.snapshotFloorUncertain {
		return fmt.Errorf("%w: reopen to recover snapshot retention", ErrCommitFinalization)
	}
	if seq < db.snapshotFloor {
		return fmt.Errorf("%w: requested %d, earliest %d", ErrSnapshotExpired, seq, db.snapshotFloor)
	}
	return nil
}

// OldestSnapshotSeq returns the earliest guaranteed point-in-time sequence.
// On a database written by an older release without a recorded retention
// boundary, the head at first open with this release is the baseline.
func (db *DB) OldestSnapshotSeq() (uint64, error) {
	if db == nil {
		return 0, ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return 0, ErrDatabaseClosed
	}
	if err := db.checkSnapshotFloor(db.snapshotFloor); err != nil {
		return 0, err
	}
	return db.snapshotFloor, nil
}
