package hexxladb

import (
	"bytes"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// MVCCStats summarizes current MVCC storage shape (cell versions only).
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

// StatsMVCC returns MVCC counters for versioned cell rows.
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
	for i := range toDelete {
		if err := db.btree.Delete(toDelete[i]); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

// PruneCellVersionsByProfile applies one prune pass using profile-driven defaults.
// It returns the number of removed rows from this single pass.
func (db *DB) PruneCellVersionsByProfile(beforeSeq uint64, profile MVCCPruneProfile) (int, error) {
	maxDelete := 2048
	switch profile {
	case "", MVCCPruneBalanced:
		maxDelete = 2048
	case MVCCPruneLowLatency:
		maxDelete = 512
	case MVCCPruneLongHistory:
		maxDelete = 256
	default:
		return 0, ErrInvalidArgument
	}
	return db.PruneCellVersions(beforeSeq, maxDelete)
}
