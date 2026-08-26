package engine

import (
	"encoding/binary"
	"fmt"
	"slices"
)

const (
	freelistPageMagic      = "HXFREE01"
	freelistPageHeaderSize = 32
)

func freelistPageCapacity(pageSize int) int {
	return (pageSize - freelistPageHeaderSize) / 8
}

func (e *Engine) validateFreelistOnOpen(hdr Header) error {
	if hdr.FormatVersion != formatVersionV3 {
		return nil
	}
	e.wtxn = &writeTxnState{hdr: hdr, dirty: make(map[uint64][]byte)}
	err := e.loadWriteTxnFreelist()
	e.wtxn = nil
	return err
}

func (e *Engine) freelistMetadataIDs(hdr Header) ([]uint64, error) {
	if hdr.FormatVersion != formatVersionV3 || hdr.FreelistHead == 0 {
		return nil, nil
	}
	remaining := hdr.FreelistCount - min(hdr.FreelistCount, uint64(HeaderInlineFreelistCapacity))
	pageID, expectedGeneration := hdr.FreelistHead, hdr.FreelistHeadGeneration
	ids := make([]uint64, 0, 1)
	seen := make(map[uint64]struct{})
	for pageID != 0 {
		if _, duplicate := seen[pageID]; duplicate {
			return nil, fmt.Errorf("%w: freelist metadata cycle at page %d", ErrCorruptTree, pageID)
		}
		seen[pageID] = struct{}{}
		if err := e.validateDataPageGeneration(pageID, expectedGeneration); err != nil {
			return nil, err
		}
		page, release, err := e.readPagePooled(pageID)
		if err != nil {
			return nil, err
		}
		nextID, nextGeneration, freeIDs, parseErr := decodeFreelistPage(page)
		release()
		if parseErr != nil {
			return nil, parseErr
		}
		if uint64(len(freeIDs)) > remaining {
			return nil, fmt.Errorf("%w: freelist metadata exceeds declared count", ErrCorruptTree)
		}
		remaining -= uint64(len(freeIDs))
		ids = append(ids, pageID)
		pageID, expectedGeneration = nextID, nextGeneration
	}
	if remaining != 0 {
		return nil, fmt.Errorf("%w: freelist is %d page ids short", ErrCorruptTree, remaining)
	}
	return ids, nil
}

// loadWriteTxnFreelist validates and loads the authenticated allocator state.
// Legacy formats intentionally retain their extend-only allocator.
func (e *Engine) loadWriteTxnFreelist() error {
	txn := e.wtxn
	if txn == nil || txn.hdr.FormatVersion != formatVersionV3 {
		return nil
	}
	txn.freePageSet = make(map[uint64]struct{})
	remaining := txn.hdr.FreelistCount
	inlineCount := min(remaining, uint64(HeaderInlineFreelistCapacity))
	for i := range inlineCount {
		if err := txn.addLoadedFreePage(txn.hdr.InlineFreelist[i]); err != nil {
			return err
		}
	}
	remaining -= inlineCount

	pageID := txn.hdr.FreelistHead
	expectedGeneration := txn.hdr.FreelistHeadGeneration
	seenMetadata := make(map[uint64]struct{})
	for pageID != 0 {
		if pageID >= txn.hdr.NextPageID || expectedGeneration == 0 {
			return fmt.Errorf("%w: invalid freelist page %d generation %d", ErrCorruptTree, pageID, expectedGeneration)
		}
		if _, exists := seenMetadata[pageID]; exists {
			return fmt.Errorf("%w: freelist metadata cycle at page %d", ErrCorruptTree, pageID)
		}
		if _, free := txn.freePageSet[pageID]; free {
			return fmt.Errorf("%w: freelist metadata page %d is also free", ErrCorruptTree, pageID)
		}
		seenMetadata[pageID] = struct{}{}
		if err := e.validateDataPageGeneration(pageID, expectedGeneration); err != nil {
			return fmt.Errorf("%w: freelist page %d generation: %w", ErrCorruptTree, pageID, err)
		}
		page, release, err := e.readPagePooled(pageID)
		if err != nil {
			return err
		}
		nextID, nextGeneration, ids, parseErr := decodeFreelistPage(page)
		release()
		if parseErr != nil {
			return parseErr
		}
		if uint64(len(ids)) > remaining {
			return fmt.Errorf("%w: freelist page %d exceeds declared count", ErrCorruptTree, pageID)
		}
		for _, freeID := range ids {
			if _, metadata := seenMetadata[freeID]; metadata {
				return fmt.Errorf("%w: freelist metadata page %d is also free", ErrCorruptTree, freeID)
			}
			if err := txn.addLoadedFreePage(freeID); err != nil {
				return err
			}
		}
		remaining -= uint64(len(ids))
		txn.freelistPageIDs = append(txn.freelistPageIDs, pageID)
		pageID, expectedGeneration = nextID, nextGeneration
	}
	if remaining != 0 {
		return fmt.Errorf("%w: freelist is %d page ids short", ErrCorruptTree, remaining)
	}
	if txn.hdr.FreelistHead == 0 && txn.hdr.FreelistHeadGeneration != 0 {
		return fmt.Errorf("%w: freelist head generation without a head", ErrCorruptTree)
	}
	slices.Sort(txn.freePageIDs)
	return nil
}

func (txn *writeTxnState) addLoadedFreePage(pageID uint64) error {
	if pageID == 0 || pageID >= txn.hdr.NextPageID {
		return fmt.Errorf("%w: free page id %d outside allocator range", ErrCorruptTree, pageID)
	}
	if _, duplicate := txn.freePageSet[pageID]; duplicate {
		return fmt.Errorf("%w: duplicate free page id %d", ErrCorruptTree, pageID)
	}
	txn.freePageSet[pageID] = struct{}{}
	txn.freePageIDs = append(txn.freePageIDs, pageID)
	return nil
}

func (e *Engine) validateDataPageGeneration(pageID, expected uint64) error {
	off, err := pageByteOffset(pageID, e.pageSize, e.physicalPageSize)
	if err != nil {
		return err
	}
	var generation [8]byte
	if _, err := e.db.ReadAt(generation[:], off); err != nil {
		return err
	}
	if got := binary.BigEndian.Uint64(generation[:]); got != expected {
		return fmt.Errorf("expected %d, got %d", expected, got)
	}
	return nil
}

func decodeFreelistPage(page []byte) (nextID, nextGeneration uint64, ids []uint64, err error) {
	if len(page) < freelistPageHeaderSize || string(page[:8]) != freelistPageMagic {
		return 0, 0, nil, fmt.Errorf("%w: invalid freelist page", ErrCorruptTree)
	}
	count := int(binary.BigEndian.Uint16(page[8:10]))
	if count > freelistPageCapacity(len(page)) {
		return 0, 0, nil, fmt.Errorf("%w: invalid freelist page count %d", ErrCorruptTree, count)
	}
	nextID = binary.BigEndian.Uint64(page[16:24])
	nextGeneration = binary.BigEndian.Uint64(page[24:32])
	if (nextID == 0) != (nextGeneration == 0) {
		return 0, 0, nil, fmt.Errorf("%w: invalid freelist next-page generation", ErrCorruptTree)
	}
	ids = make([]uint64, count)
	for i := range count {
		off := freelistPageHeaderSize + i*8
		ids[i] = binary.BigEndian.Uint64(page[off : off+8])
	}
	return nextID, nextGeneration, ids, nil
}

func encodeFreelistPage(pageSize int, ids []uint64, nextID, nextGeneration uint64) ([]byte, error) {
	if len(ids) > freelistPageCapacity(pageSize) {
		return nil, fmt.Errorf("%w: too many freelist ids", ErrBadPageSize)
	}
	page := make([]byte, pageSize)
	copy(page[:8], freelistPageMagic)
	binary.BigEndian.PutUint16(page[8:10], uint16(len(ids))) //nolint:gosec // capacity is below uint16 for supported pages.
	binary.BigEndian.PutUint64(page[16:24], nextID)
	binary.BigEndian.PutUint64(page[24:32], nextGeneration)
	for i, pageID := range ids {
		off := freelistPageHeaderSize + i*8
		binary.BigEndian.PutUint64(page[off:off+8], pageID)
	}
	return page, nil
}

func (e *Engine) allocateReusablePageID() (uint64, bool) {
	txn := e.wtxn
	if txn == nil || !e.pageReuseEnabled || txn.hdr.FormatVersion != formatVersionV3 || len(txn.freePageIDs) == 0 {
		return 0, false
	}
	pageID := txn.freePageIDs[0]
	txn.freePageIDs = txn.freePageIDs[1:]
	delete(txn.freePageSet, pageID)
	txn.freelistDirty = true
	return pageID, true
}

func (e *Engine) releasePageID(pageID uint64) error {
	txn := e.wtxn
	if txn == nil || !e.pageReuseEnabled || txn.hdr.FormatVersion != formatVersionV3 {
		return nil
	}
	if pageID == 0 || pageID >= txn.hdr.NextPageID {
		return fmt.Errorf("%w: cannot release page %d", ErrCorruptTree, pageID)
	}
	if _, duplicate := txn.freePageSet[pageID]; duplicate {
		return fmt.Errorf("%w: duplicate release of page %d", ErrCorruptTree, pageID)
	}
	if slices.Contains(txn.freelistPageIDs, pageID) {
		return fmt.Errorf("%w: cannot release active freelist page %d", ErrCorruptTree, pageID)
	}
	txn.freePageSet[pageID] = struct{}{}
	txn.freePageIDs = append(txn.freePageIDs, pageID)
	txn.freelistDirty = true
	return nil
}

func (e *Engine) prepareFreelistCommit() error {
	txn := e.wtxn
	if txn == nil || txn.hdr.FormatVersion != formatVersionV3 || !txn.freelistDirty {
		return nil
	}
	poolSet := make(map[uint64]struct{}, len(txn.freePageIDs)+len(txn.freelistPageIDs))
	for _, pageID := range txn.freePageIDs {
		poolSet[pageID] = struct{}{}
	}
	for _, pageID := range txn.freelistPageIDs {
		poolSet[pageID] = struct{}{}
	}
	pool := make([]uint64, 0, len(poolSet))
	for pageID := range poolSet {
		pool = append(pool, pageID)
	}
	slices.Sort(pool)

	capacity := freelistPageCapacity(e.pageSize)
	metadataCount := 0
	for len(pool)-metadataCount > HeaderInlineFreelistCapacity+metadataCount*capacity {
		metadataCount++
	}
	metadataIDs := slices.Clone(pool[:metadataCount])
	freeIDs := slices.Clone(pool[metadataCount:])

	clear(txn.hdr.InlineFreelist[:])
	inlineCount := min(len(freeIDs), HeaderInlineFreelistCapacity)
	copy(txn.hdr.InlineFreelist[:], freeIDs[:inlineCount])
	externalIDs := freeIDs[inlineCount:]

	nextID, nextGeneration := uint64(0), uint64(0)
	for i := range slices.Backward(metadataIDs) {
		start := min(i*capacity, len(externalIDs))
		end := min(start+capacity, len(externalIDs))
		page, err := encodeFreelistPage(e.pageSize, externalIDs[start:end], nextID, nextGeneration)
		if err != nil {
			return err
		}
		generation, err := e.writePageWithGeneration(metadataIDs[i], page)
		if err != nil {
			return err
		}
		nextID, nextGeneration = metadataIDs[i], generation
	}
	txn.hdr.FreelistHead = nextID
	txn.hdr.FreelistHeadGeneration = nextGeneration
	txn.hdr.FreelistCount = uint64(len(freeIDs)) //nolint:gosec // bounded by allocated page count.
	txn.freePageIDs = freeIDs
	txn.freePageSet = make(map[uint64]struct{}, len(freeIDs))
	for _, pageID := range freeIDs {
		txn.freePageSet[pageID] = struct{}{}
	}
	txn.freelistPageIDs = metadataIDs
	txn.freelistDirty = false
	return nil
}
