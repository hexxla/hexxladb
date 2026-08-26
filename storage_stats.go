package hexxladb

import (
	"errors"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb/internal/engine"
)

// StorageStats reports current physical file sizes and page reachability.
// AllocatedPages and ReachablePages include the header page; ReachablePages also
// includes authenticated freelist metadata. LiveBytes is the physical
// page-rounded size of reachable pages, not the encoded logical payload size.
// ReclaimableBytes counts whole pages outside the tree and allocator metadata;
// this includes ReusablePages. Compaction may reclaim additional space by
// repacking partially filled reachable pages.
type StorageStats struct {
	PageSize uint64
	// PhysicalPageSize includes any authenticated-page envelope. It equals
	// PageSize for plaintext and legacy XTS databases.
	PhysicalPageSize uint64
	PrimaryBytes     uint64
	WALBytes         uint64
	ChangelogBytes   uint64
	AllocatedPages   uint64
	ReachablePages   uint64
	// AllocatorPages counts authenticated freelist metadata pages.
	AllocatorPages uint64
	// ReusablePages counts pages available to authenticated transactions.
	ReusablePages    uint64
	LiveBytes        uint64
	ReclaimableBytes uint64
}

// StorageStats walks the current B+ tree and its overflow chains without
// changing the database. The database read lock is held for the walk, so a
// consistent result is returned and writers wait until it completes.
func (db *DB) StorageStats() (StorageStats, error) {
	if db == nil {
		return StorageStats{}, ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return StorageStats{}, ErrDatabaseClosed
	}

	engineStats, err := db.btree.StorageStats()
	if err != nil {
		if errors.Is(err, engine.ErrCorruptTree) {
			return StorageStats{}, fmt.Errorf("%w: storage stats: %w", ErrCorruptDatabase, err)
		}
		return StorageStats{}, fmt.Errorf("hexxladb: storage stats: %w", err)
	}
	var changelogBytes uint64
	if db.changelog != nil {
		info, err := os.Stat(db.changelog.Path())
		if err != nil {
			return StorageStats{}, fmt.Errorf("hexxladb: stat changelog: %w", err)
		}
		if info.Size() < 0 {
			return StorageStats{}, fmt.Errorf("hexxladb: stat changelog: negative file size")
		}
		changelogBytes = uint64(info.Size()) //nolint:gosec // size was checked non-negative above.
	}
	return StorageStats{
		PageSize:         engineStats.PageSize,
		PhysicalPageSize: engineStats.PhysicalPageSize,
		PrimaryBytes:     engineStats.PrimaryBytes,
		WALBytes:         engineStats.WALBytes,
		ChangelogBytes:   changelogBytes,
		AllocatedPages:   engineStats.AllocatedPages,
		ReachablePages:   engineStats.ReachablePages,
		AllocatorPages:   engineStats.AllocatorPages,
		ReusablePages:    engineStats.ReusablePages,
		LiveBytes:        engineStats.LiveBytes,
		ReclaimableBytes: engineStats.ReclaimableBytes,
	}, nil
}
