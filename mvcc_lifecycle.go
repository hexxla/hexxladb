package hexxladb

import (
	"bytes"

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
	if !db.useMVCC || db.mvccRetention.RetainCommitsBehindHead == 0 {
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
	// WastedBytes is the cumulative logical byte size of overflow-page chains freed
	// since the database was opened. These pages are dead space on disk until the next
	// [CompactTo]. The counter is in-memory only and resets to zero on reopen.
	// A non-zero value is a signal to schedule compaction.
	//
	// Deprecated: use [DB.StorageStats] ReclaimableBytes for persistent, complete
	// whole-page reachability accounting. WastedBytes counts only overflow payload.
	WastedBytes uint64
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
	if !db.useMVCC {
		return MVCCStats{CommitSeq: ch.commitSeq}, nil
	}
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
		WastedBytes:   db.eng.WastedBytes(),
	}, nil
}

// PruneCellVersions removes up to maxDelete stale versioned rows with commit_seq < beforeSeq.
// It always keeps the latest version for each logical cell key.
//
// Implementation notes:
//   - Single-pass scan: because cell/<packed>/<seq> keys are sorted with seq ascending within
//     each coordinate group, the last row seen per coordinate is always the latest version.
//     We buffer the previous row and emit it as a delete candidate when the coordinate changes,
//     never emitting the final row of each group. This replaces the previous two-pass approach
//     and requires O(1) extra memory instead of a full latest-version map.
//   - Batched commits: deletes are committed in sub-batches of [pruneSubBatchSize] to cap WAL
//     burst size and release db.mu between sub-batches so concurrent readers are not starved.
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
	if !db.useMVCC {
		return 0, nil
	}

	// Single-pass scan: walk cell/ keys left-to-right. Within each coordinate group,
	// keys are ordered by seq ascending, so the last key seen per coordinate is the
	// latest version. We buffer prevKey/prevSeq/prevCoord and emit the buffered row
	// as a delete candidate when we advance to a new coordinate.
	toDelete := make([][]byte, 0, min(maxDelete, pruneSubBatchSize*4))
	var (
		prevKey   []byte
		prevSeq   uint64
		prevCoord lattice.PackedCoord
		hasPrev   bool
	)
	_ = db.btree.AscendRange([]byte(index.CellPrefix), nil, func(k, _ []byte) bool {
		if !bytes.HasPrefix(k, []byte(index.CellPrefix)) {
			return false
		}
		p, seq, parseErr := index.ParseCellVersionKey(k)
		if parseErr != nil {
			return true
		}
		coordChanged := !hasPrev || p != prevCoord
		if hasPrev && !coordChanged && prevSeq < beforeSeq {
			// prevKey is a non-latest version of the current coordinate group — prunable.
			toDelete = append(toDelete, prevKey)
		}
		// When coordChanged, prevKey was the latest version of the previous group — skip it.
		prevKey = append([]byte(nil), k...)
		prevSeq = seq
		prevCoord = p
		hasPrev = true
		return len(toDelete) < maxDelete
	})
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

		if err := db.eng.BeginWriteTxn(); err != nil {
			return deleted, err
		}
		for _, key := range batch {
			if err := db.btree.Delete(key); err != nil {
				db.eng.AbortWriteTxn()
				return deleted, err
			}
			deleted++
		}
		if err := db.eng.CommitWriteTxn(); err != nil {
			db.eng.AbortWriteTxn()
			return deleted, err
		}
		// Refresh the cached header after each sub-batch commit so subsequent
		// View calls see the updated BTreeRoot without a pread.
		if fhdr, fErr := db.eng.ReadHeader(); fErr == nil {
			db.storeCachedHeader(fhdr.CommitSeq, fhdr.BTreeRoot)
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

// PruneCellVersionsByProfile applies one prune pass using profile-driven defaults.
// It returns the number of removed rows from this single pass.
func (db *DB) PruneCellVersionsByProfile(beforeSeq uint64, profile MVCCPruneProfile) (int, error) {
	maxDelete, err := profileToMaxDelete(profile)
	if err != nil {
		return 0, err
	}
	return db.PruneCellVersions(beforeSeq, maxDelete)
}
