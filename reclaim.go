package hexxladb

import "fmt"

// ReclaimTail truncates the contiguous physical-file suffix currently owned by
// the authenticated format-v3 freelist. It does not repack partially filled
// pages; use Compact for that. Legacy formats safely return zero.
//
// The returned value is the number of primary-file bytes removed. A committed
// allocator update always precedes truncation, so interruption can leave excess
// bytes for the next call to remove but cannot remove a reachable page.
func (db *DB) ReclaimTail() (uint64, error) {
	if db == nil || db.closed.Load() {
		return 0, ErrDatabaseClosed
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.activeEng() == nil {
		return 0, ErrDatabaseClosed
	}
	reclaimed, err := db.eng.ReclaimTail()
	if err != nil {
		db.recoveryRequired.Store(true)
		return 0, fmt.Errorf("hexxladb: reclaim tail: %w", mapEngineDataError(err))
	}
	return reclaimed, nil
}
