package hexxladb

import (
	"bytes"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

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
	hdr, err := db.eng.ReadHeader()
	if err != nil {
		return 0, false, err
	}
	c := db.mvccRetention.RetainCommitsBehindHead
	if hdr.CommitSeq > c {
		return hdr.CommitSeq - c, true, nil
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
	hdr, err := db.eng.ReadHeader()
	if err != nil {
		return MVCCStats{}, err
	}
	if !db.useMVCC {
		return MVCCStats{CommitSeq: hdr.CommitSeq}, nil
	}
	seen := make(map[lattice.PackedCoord]struct{})
	var rows int64
	err = db.btree.AscendRange([]byte(index.CellPrefix), nil, func(k, _ []byte) bool {
		if !bytes.HasPrefix(k, []byte(index.CellPrefix)) {
			return false
		}
		p, _, err := index.ParseCellVersionKey(k)
		if err != nil {
			return true
		}
		rows++
		seen[p] = struct{}{}
		return true
	})
	if err != nil {
		return MVCCStats{}, err
	}
	return MVCCStats{
		CommitSeq:     hdr.CommitSeq,
		VersionedRows: rows,
		LogicalCells:  int64(len(seen)),
	}, nil
}

// PruneCellVersions removes up to maxDelete stale versioned rows with commit_seq < beforeSeq.
// It always keeps the latest version for each logical cell key.
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
	latest := make(map[lattice.PackedCoord]uint64)
	if err := db.btree.AscendRange([]byte(index.CellPrefix), nil, func(k, _ []byte) bool {
		if !bytes.HasPrefix(k, []byte(index.CellPrefix)) {
			return false
		}
		p, seq, err := index.ParseCellVersionKey(k)
		if err != nil {
			return true
		}
		if cur, ok := latest[p]; !ok || seq > cur {
			latest[p] = seq
		}
		return true
	}); err != nil {
		return 0, err
	}
	toDelete := make([][]byte, 0, maxDelete)
	if err := db.btree.AscendRange([]byte(index.CellPrefix), nil, func(k, _ []byte) bool {
		if !bytes.HasPrefix(k, []byte(index.CellPrefix)) {
			return false
		}
		p, seq, err := index.ParseCellVersionKey(k)
		if err != nil {
			return true
		}
		if seq < beforeSeq && seq != latest[p] {
			toDelete = append(toDelete, append([]byte(nil), k...))
			if len(toDelete) >= maxDelete {
				return false
			}
		}
		return true
	}); err != nil {
		return 0, err
	}
	if len(toDelete) == 0 {
		return 0, nil
	}
	// Route deletes through the same engine write-transaction path as [DB.Update]. Otherwise
	// [Engine.WritePage] uses immediate WAL/primary persistence while [Engine.readPagePooled] still
	// honors group-WAL overlay — rebalance can see a mixed view and corrupt the B+ tree.
	if err := db.eng.BeginWriteTxn(); err != nil {
		return 0, err
	}
	for i := range toDelete {
		if err := db.btree.Delete(toDelete[i]); err != nil {
			db.eng.AbortWriteTxn()
			return deleted, err
		}
		deleted++
	}
	if err := db.eng.CommitWriteTxn(); err != nil {
		db.eng.AbortWriteTxn()
		return deleted, err
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
