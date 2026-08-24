package hexxladb

import (
	"sync/atomic"
	"time"
)

// WriteStats reports cumulative in-process timing for public [DB.Update] and [DB.Batch]
// calls since the database was opened. Read two snapshots to calculate interval averages.
// Durations include failed attempts that reached the corresponding phase.
type WriteStats struct {
	Calls        uint64
	Commits      uint64
	LockWait     time.Duration
	Callback     time.Duration
	Durability   time.Duration
	Finalization time.Duration
}

type writeStatsCounters struct {
	calls        atomic.Uint64
	commits      atomic.Uint64
	lockWait     atomic.Int64
	callback     atomic.Int64
	durability   atomic.Int64
	finalization atomic.Int64
}

// WriteStats returns a lock-free snapshot of cumulative write-path timings. It remains
// available after [DB.Close] so callers can collect a final sample.
func (db *DB) WriteStats() WriteStats {
	if db == nil {
		return WriteStats{}
	}
	return WriteStats{
		Calls:        db.writeStats.calls.Load(),
		Commits:      db.writeStats.commits.Load(),
		LockWait:     time.Duration(db.writeStats.lockWait.Load()),
		Callback:     time.Duration(db.writeStats.callback.Load()),
		Durability:   time.Duration(db.writeStats.durability.Load()),
		Finalization: time.Duration(db.writeStats.finalization.Load()),
	}
}
