package hexxladb

import (
	"bytes"
	"fmt"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// pruneSubBatchSize is the maximum number of B+ tree deletes per commit in PruneCellVersions.
// Keeping this small caps the WAL burst per commit and limits the time db.mu is held
// between sub-batches, allowing concurrent readers to make progress.
const pruneSubBatchSize = 64

// PruneScheduler holds defaults for operator-driven periodic pruning. It does not start
// background goroutines; invoke [PruneScheduler.Tick] from your process scheduler or timer.
type PruneScheduler struct {
	// Profile selects batch sizes consistent with [PruneCellVersionsByProfile]. Empty defaults to [MVCCPruneBalanced].
	Profile MVCCPruneProfile
}

// Tick runs one [PruneCellVersions] pass using [DB.MVCCPrunePlan]. Returns deleted row count (0 when no policy or nothing to reclaim).
func (s PruneScheduler) Tick(db *DB) (deleted int, err error) {
	if db == nil {
		return 0, ErrDatabaseClosed
	}
	p := s.Profile
	if p == "" {
		p = MVCCPruneBalanced
	}
	beforeSeq, maxDel, ok, err := db.MVCCPrunePlan(p)
	if err != nil {
		return 0, err
	}
	if !ok || maxDel <= 0 {
		return 0, nil
	}
	return db.PruneCellVersions(beforeSeq, maxDel)
}

// SuggestedPruneBeforeSeq returns beforeSeq suitable for [PruneCellVersions] based on [Options.MVCCRetention]
// captured at [Open]. ok is false when MVCC is off or RetainCommitsBehindHead is zero (operator chooses beforeSeq explicitly).
func (db *DB) SuggestedPruneBeforeSeq() (beforeSeq uint64, ok bool, err error) {
	if db == nil {
		return 0, false, ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return 0, false, ErrDatabaseClosed
	}
	if db.mvccRetention.RetainCommitsBehindHead == 0 {
		return 0, false, nil
	}
	ch := db.cachedHdr.Load()
	c := db.mvccRetention.RetainCommitsBehindHead
	if ch.commitSeq > c {
		return ch.commitSeq - c, true, nil
	}
	return 0, true, nil
}

// profileToMaxDelete maps an [MVCCPruneProfile] to a batch-size limit.
func profileToMaxDelete(profile MVCCPruneProfile) (int, error) {
	switch profile {
	case "", MVCCPruneBalanced:
		return 2048, nil
	case MVCCPruneLowLatency:
		return 512, nil
	case MVCCPruneLongHistory:
		return 256, nil
	default:
		return 0, ErrInvalidArgument
	}
}

// MVCCPrunePlan combines [SuggestedPruneBeforeSeq] with profile-driven batch sizing for one prune pass.
func (db *DB) MVCCPrunePlan(profile MVCCPruneProfile) (beforeSeq uint64, maxDelete int, ok bool, err error) {
	var bs uint64
	bs, ok, err = db.SuggestedPruneBeforeSeq()
	if err != nil || !ok {
		return 0, 0, ok, err
	}
	maxDelete, err = profileToMaxDelete(profile)
	if err != nil {
		return 0, 0, false, err
	}
	return bs, maxDelete, true, nil
}

// MVCCStats summarizes physical MVCC cell rows (keys under cell/ with version suffix).
//
// LogicalCells is the count of distinct packed coordinates that still have at least one
// stored version row — including coords whose latest version is a delete tombstone — not
// “visible live cells.” Visible counts come from [DB.HealthCheck] CellCount or [Tx.GetCell].
// VersionedRows grows with every put and every delete (tombstone writes a new version).
type MVCCStats struct {
	CommitSeq     uint64
	VersionedRows int64
	LogicalCells  int64
}

// MVCCPruneProfile defines operational defaults for stale-version pruning cadence.
type MVCCPruneProfile string

const (
	// MVCCPruneLowLatency favors short prune batches to minimize lock hold time.
	MVCCPruneLowLatency MVCCPruneProfile = "low-latency"
	// MVCCPruneBalanced is the default profile for mixed workloads.
	MVCCPruneBalanced MVCCPruneProfile = "balanced"
	// MVCCPruneLongHistory favors retention and uses smaller prune steps.
	MVCCPruneLongHistory MVCCPruneProfile = "long-history"
)

// StatsMVCC returns MVCC counters for versioned cell primary rows (see [MVCCStats]).
func (db *DB) StatsMVCC() (MVCCStats, error) {
	if db == nil {
		return MVCCStats{}, ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return MVCCStats{}, ErrDatabaseClosed
	}
	ch := db.cachedHdr.Load()
	var rows, logicalCells int64
	var prevCoord lattice.PackedCoord
	var hasPrev bool
	if err := db.btree.AscendRange([]byte(index.CellPrefix), nil, func(k, _ []byte) bool {
		if !bytes.HasPrefix(k, []byte(index.CellPrefix)) {
			return false
		}
		p, _, parseErr := index.ParseCellVersionKey(k)
		if parseErr != nil {
			return true
		}
		rows++
		if !hasPrev || p != prevCoord {
			logicalCells++
			prevCoord = p
			hasPrev = true
		}
		return true
	}); err != nil {
		return MVCCStats{}, err
	}
	return MVCCStats{
		CommitSeq:     ch.commitSeq,
		VersionedRows: rows,
		LogicalCells:  logicalCells,
	}, nil
}

// PruneCellVersions removes up to maxDelete cell versions superseded at or
// before beforeSeq. It preserves each coordinate's state at that cutoff,
// including tombstones, and clamps the cutoff to the current commit.
// The first committed deletion advances [DB.OldestSnapshotSeq] to at least
// that cutoff. Named snapshots below it expire.
func (db *DB) PruneCellVersions(beforeSeq uint64, maxDelete int) (deleted int, err error) {
	if db == nil {
		return 0, ErrDatabaseClosed
	}
	if maxDelete <= 0 {
		return 0, ErrInvalidArgument
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.activeEng() == nil {
		return 0, ErrDatabaseClosed
	}

	beforeSeq = min(beforeSeq, db.cachedHdr.Load().commitSeq)
	if beforeSeq == 0 {
		return 0, nil
	}
	// A row can be dropped only if the same coordinate has a successor
	// already visible at the cutoff. Keep the last version at or below it.

	toDelete := make([][]byte, 0, min(maxDelete, pruneSubBatchSize*4))
	var (
		prevKey   []byte
		prevSeq   uint64
		prevCoord lattice.PackedCoord
		hasPrev   bool
	)
	err = db.btree.AscendRange([]byte(index.CellPrefix), nil, func(k, _ []byte) bool {
		if !bytes.HasPrefix(k, []byte(index.CellPrefix)) {
			return false
		}
		p, seq, parseErr := index.ParseCellVersionKey(k)
		if parseErr != nil {
			return true
		}
		coordChanged := !hasPrev || p != prevCoord
		if hasPrev && !coordChanged && prevSeq < beforeSeq && seq <= beforeSeq {
			// prevKey is a non-latest version of the current coordinate group — prunable.
			toDelete = append(toDelete, bytes.Clone(prevKey))
		}
		// When coordChanged, prevKey was the latest version of the previous group — skip it.
		prevKey = append(prevKey[:0], k...)
		prevSeq = seq
		prevCoord = p
		hasPrev = true
		return len(toDelete) < maxDelete
	})
	if err != nil {
		return 0, mapEngineDataError(err)
	}
	// prevKey after the scan is always the latest version of its coordinate — never emit it.

	if len(toDelete) == 0 {
		return 0, nil
	}

	// Batched commits: commit in sub-batches to cap WAL burst and release the lock
	// between sub-batches so concurrent readers are not starved.
	// Route deletes through the same engine write-transaction path as [DB.Update].
	for len(toDelete) > 0 {
		batch := toDelete
		if len(batch) > pruneSubBatchSize {
			batch = toDelete[:pruneSubBatchSize]
		}
		toDelete = toDelete[len(batch):]

		count, err := db.pruneCellVersionBatch(batch, beforeSeq)
		deleted += count
		if err != nil {
			return deleted, err
		}
		// Release and re-acquire the lock between sub-batches to allow
		// concurrent readers to make progress.
		if len(toDelete) > 0 {
			db.mu.Unlock()
			db.mu.Lock()
			if db.activeEng() == nil {
				return deleted, ErrDatabaseClosed
			}
		}
	}
	return deleted, nil
}

// pruneCellVersionBatch removes each obsolete primary and its indexes in the
// same engine transaction. The caller holds db.mu.
func (db *DB) pruneCellVersionBatch(batch [][]byte, beforeSeq uint64) (int, error) {
	if db.recoveryRequired.Load() {
		return 0, fmt.Errorf("%w: close and reopen required", ErrCommitFinalization)
	}
	if err := db.eng.BeginWriteTxn(); err != nil {
		return 0, err
	}
	defer db.eng.AbortWriteTxn()
	tx := &Tx{db: db, writable: true}
	deleted := 0
	for _, key := range batch {
		value, found, err := tx.getDirect(key)
		if err != nil {
			return 0, err
		}
		if !found {
			continue
		}
		if len(value) != 0 {
			cell, seq, err := decodeCellVersionForIndexes(key, value)
			if err != nil {
				return 0, err
			}
			if err := tx.removeCellSecondaryIndex(cell, seq); err != nil {
				return 0, err
			}
		}
		if err := tx.deleteDirect(key); err != nil {
			return 0, err
		}
		deleted++
	}
	if deleted == 0 {
		return 0, nil
	}
	floor := max(beforeSeq, db.snapshotFloor)
	if !db.snapshotFloorStored || floor != db.snapshotFloor {
		if err := tx.stageSnapshotFloor(floor); err != nil {
			return 0, err
		}
	}
	if err := db.eng.CommitWriteTxn(); err != nil {
		db.snapshotFloorUncertain = true
		db.recoveryRequired.Store(true)
		return 0, fmt.Errorf("%w: prune cell versions: %w", ErrCommitFinalization, err)
	}
	db.snapshotFloor = floor
	db.snapshotFloorStored = true
	header, err := db.eng.ReadHeader()
	if err != nil {
		db.snapshotFloorUncertain = true
		db.recoveryRequired.Store(true)
		return deleted, durableCommitError("read header after pruning cell versions", err)
	}
	db.storeCachedHeader(header.CommitSeq, header.BTreeRoot)
	return deleted, nil
}

// PruneCellVersionsByProfile applies one prune pass using profile-driven defaults.
// It returns the number of removed rows from this single pass.
func (db *DB) PruneCellVersionsByProfile(beforeSeq uint64, profile MVCCPruneProfile) (int, error) {
	maxDelete, err := profileToMaxDelete(profile)
	if err != nil {
		return 0, err
	}
	return db.PruneCellVersions(beforeSeq, maxDelete)
}
