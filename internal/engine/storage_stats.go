package engine

import (
	"encoding/binary"
	"fmt"
)

// StorageStats reports physical page allocation and reachability for an open engine.
// Page counts include the header page (page 0).
type StorageStats struct {
	PageSize         uint64
	PrimaryBytes     uint64
	WALBytes         uint64
	AllocatedPages   uint64
	ReachablePages   uint64
	LiveBytes        uint64
	ReclaimableBytes uint64
}

// StorageStats walks the current B+ tree and its overflow chains to distinguish
// reachable pages from allocated pages. It does not modify the database.
func (t *BTree) StorageStats() (StorageStats, error) {
	if t == nil || t.eng == nil || t.eng.db == nil || t.eng.wal == nil {
		return StorageStats{}, fmt.Errorf("engine: closed")
	}
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return StorageStats{}, err
	}
	primaryInfo, err := t.eng.db.Stat()
	if err != nil {
		return StorageStats{}, fmt.Errorf("engine: stat primary: %w", err)
	}
	walInfo, err := t.eng.wal.Stat()
	if err != nil {
		return StorageStats{}, fmt.Errorf("engine: stat WAL: %w", err)
	}
	if primaryInfo.Size() < 0 || walInfo.Size() < 0 {
		return StorageStats{}, fmt.Errorf("%w: negative file size", ErrCorruptTree)
	}

	pageSize := uint64(t.pageSize())           //nolint:gosec // page size is one of the supported positive values.
	primaryBytes := uint64(primaryInfo.Size()) //nolint:gosec // size was checked non-negative above.
	if primaryBytes%pageSize != 0 {
		return StorageStats{}, fmt.Errorf("%w: primary size %d is not page aligned", ErrCorruptTree, primaryBytes)
	}
	allocatedPages := primaryBytes / pageSize
	if allocatedPages == 0 || hdr.NextPageID == 0 || hdr.NextPageID > allocatedPages {
		return StorageStats{}, fmt.Errorf(
			"%w: allocator next page %d outside %d physical pages",
			ErrCorruptTree,
			hdr.NextPageID,
			allocatedPages,
		)
	}

	visited := map[uint64]struct{}{0: {}}
	if hdr.BTreeRoot != 0 {
		if err := t.walkReachableTreePage(hdr.BTreeRoot, hdr.NextPageID, visited); err != nil {
			return StorageStats{}, err
		}
	}
	reachablePages := uint64(len(visited)) //nolint:gosec // cannot exceed the validated physical page count.
	if reachablePages > allocatedPages {
		return StorageStats{}, fmt.Errorf("%w: %d reachable pages exceed %d allocated pages", ErrCorruptTree, reachablePages, allocatedPages)
	}

	return StorageStats{
		PageSize:         pageSize,
		PrimaryBytes:     primaryBytes,
		WALBytes:         uint64(walInfo.Size()), //nolint:gosec // size was checked non-negative above.
		AllocatedPages:   allocatedPages,
		ReachablePages:   reachablePages,
		LiveBytes:        reachablePages * pageSize,
		ReclaimableBytes: (allocatedPages - reachablePages) * pageSize,
	}, nil
}

func (t *BTree) walkReachableTreePage(pageID, nextPageID uint64, visited map[uint64]struct{}) error {
	if err := markReachablePage(pageID, nextPageID, visited); err != nil {
		return err
	}
	page, release, err := t.eng.readPagePooled(pageID)
	if err != nil {
		return err
	}
	kind := page[5]
	switch kind {
	case btreeKindLeaf:
		leaf, parseErr := parseLeafPage(page)
		release()
		if parseErr != nil {
			return parseErr
		}
		for _, value := range leaf.vals {
			if !isOverflowStub(value) {
				continue
			}
			logicalLen, firstPageID := decodeOverflowStub(value)
			if err := t.walkReachableOverflow(firstPageID, logicalLen, nextPageID, visited); err != nil {
				return err
			}
		}
		return nil
	case btreeKindInternal:
		internal, parseErr := parseInternalPage(page)
		release()
		if parseErr != nil {
			return parseErr
		}
		for _, child := range internal.ptrs {
			if err := t.walkReachableTreePage(child, nextPageID, visited); err != nil {
				return err
			}
		}
		return nil
	default:
		release()
		return fmt.Errorf("%w: page %d has unknown B+ tree kind %d", ErrCorruptTree, pageID, kind)
	}
}

func (t *BTree) walkReachableOverflow(pageID uint64, logicalLen uint32, nextPageID uint64, visited map[uint64]struct{}) error {
	remaining := uint64(logicalLen)
	payloadBytes := uint64(overflowPayloadPerPage(t.pageSize())) //nolint:gosec // page size exceeds the pointer prefix.
	for pageID != 0 {
		if remaining == 0 {
			return fmt.Errorf("%w: overflow chain exceeds logical length", ErrCorruptTree)
		}
		if err := markReachablePage(pageID, nextPageID, visited); err != nil {
			return err
		}
		page, release, err := t.eng.readPagePooled(pageID)
		if err != nil {
			return err
		}
		pageID = binary.BigEndian.Uint64(page[:overflowPtrSize])
		release()
		remaining -= min(remaining, payloadBytes)
	}
	if remaining != 0 {
		return fmt.Errorf("%w: overflow chain is %d bytes short", ErrCorruptTree, remaining)
	}
	return nil
}

func markReachablePage(pageID, nextPageID uint64, visited map[uint64]struct{}) error {
	if pageID == 0 || pageID >= nextPageID {
		return fmt.Errorf("%w: page %d outside allocator range [1,%d)", ErrCorruptTree, pageID, nextPageID)
	}
	if _, exists := visited[pageID]; exists {
		return fmt.Errorf("%w: page %d is reachable more than once", ErrCorruptTree, pageID)
	}
	visited[pageID] = struct{}{}
	return nil
}
