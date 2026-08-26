package engine

import (
	"fmt"

	"github.com/hexxla/hexxladb/internal/engine/crashtest"
)

// ReclaimTail removes a contiguous suffix owned by the authenticated freelist.
// The allocator header is committed before truncation, so a crash can leave
// harmless excess bytes but can never truncate a logically allocated page.
func (e *Engine) ReclaimTail() (uint64, error) {
	if e == nil || e.db == nil || e.wal == nil {
		return 0, fmt.Errorf("engine: closed")
	}
	if e.wtxn != nil {
		return 0, ErrWriteTxnActive
	}
	hdr, err := e.visibleHeader()
	if err != nil {
		return 0, err
	}
	reconciled, err := e.truncatePrimaryToAllocator(hdr.NextPageID)
	if err != nil {
		return 0, err
	}
	if hdr.FormatVersion != formatVersionV3 {
		return reconciled, nil
	}
	if err := e.BeginWriteTxn(); err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			e.AbortWriteTxn()
		}
	}()

	txn := e.wtxn
	allocatorOwned := make(map[uint64]struct{}, len(txn.freePageIDs)+len(txn.freelistPageIDs))
	for _, pageID := range txn.freePageIDs {
		allocatorOwned[pageID] = struct{}{}
	}
	for _, pageID := range txn.freelistPageIDs {
		allocatorOwned[pageID] = struct{}{}
	}
	target := txn.hdr.NextPageID
	for target > 1 {
		if _, owned := allocatorOwned[target-1]; !owned {
			break
		}
		target--
	}
	if target == txn.hdr.NextPageID {
		e.AbortWriteTxn()
		committed = true
		return reconciled, nil
	}

	txn.freePageIDs = filterPageIDsBelow(txn.freePageIDs, target)
	txn.freelistPageIDs = filterPageIDsBelow(txn.freelistPageIDs, target)
	txn.freePageSet = make(map[uint64]struct{}, len(txn.freePageIDs))
	for _, pageID := range txn.freePageIDs {
		txn.freePageSet[pageID] = struct{}{}
	}
	txn.hdr.NextPageID = target
	txn.freelistDirty = true
	oldNextPageID := hdr.NextPageID
	if err := e.CommitWriteTxn(); err != nil {
		return 0, err
	}
	committed = true
	crashtest.At("reclaim_header_committed")
	truncated, err := e.truncatePrimaryToAllocator(target)
	if err != nil {
		return 0, err
	}
	crashtest.At("reclaim_primary_truncated")
	expected := (oldNextPageID - target) * uint64(e.physicalPageSize) //nolint:gosec // physical page size is positive and bounded.
	if truncated != expected {
		return 0, fmt.Errorf("%w: reclaimed %d bytes, expected %d", ErrCorruptTree, truncated, expected)
	}
	return reconciled + truncated, nil
}

func filterPageIDsBelow(pageIDs []uint64, limit uint64) []uint64 {
	kept := pageIDs[:0]
	for _, pageID := range pageIDs {
		if pageID < limit {
			kept = append(kept, pageID)
		}
	}
	return kept
}

func (e *Engine) truncatePrimaryToAllocator(nextPageID uint64) (uint64, error) {
	target, err := pageByteOffset(nextPageID, e.pageSize, e.physicalPageSize)
	if err != nil {
		return 0, err
	}
	info, err := e.db.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() < target {
		return 0, fmt.Errorf("%w: primary size %d below allocator boundary %d", ErrCorruptTree, info.Size(), target)
	}
	if info.Size() == target {
		return 0, nil
	}
	if err := e.db.Truncate(target); err != nil {
		return 0, err
	}
	if err := e.syncPrimary(); err != nil {
		return 0, err
	}
	return uint64(info.Size() - target), nil //nolint:gosec // target is proven below non-negative file size.
}
