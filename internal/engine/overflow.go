package engine

import (
	"encoding/binary"
	"fmt"
)

// Overflow page format:
//
//	[0..7]   uint64 big-endian — next page ID (0 = last)
//	[8..pageSize-1]            — payload chunk
//
// Overflow stub stored in leaf value:
//
//	[0]      0x01 (overflow marker)
//	[1..4]   uint32 big-endian — logical value length
//	[5..12]  uint64 big-endian — first overflow page ID
//
// Total stub size: 13 bytes. Always fits inline in any leaf entry.

const (
	overflowMarker  = byte(0x01)
	overflowStubLen = 1 + 4 + 8 // marker + logicalLen + pageID
	overflowPtrSize = 8         // next-page pointer at start of each overflow page
)

// overflowPayloadPerPage returns the usable payload bytes per overflow page.
func overflowPayloadPerPage(pageSize int) int {
	return pageSize - overflowPtrSize
}

// inlineThreshold returns the maximum value size that fits inline in a leaf entry.
// Values larger than this require overflow pages.
func inlineThreshold(pageSize int) int {
	// A single leaf entry occupies: 4 (keyLen+valLen) + key + value.
	// For overflow to be worthwhile, the value must exceed what can fit in
	// roughly half a page (leaving room for the key and headers).
	// We use a conservative threshold: pageSize - btreeHeaderSize - maxKeyBytes - 4 (kv lengths).
	// This ensures a single inline entry always fits in one page.
	t := max(pageSize-btreeHeaderSize-maxKeyBytes-4, 64)
	return t
}

// isOverflowStub reports whether val is an overflow stub (starts with the marker
// and has exactly the expected length).
func isOverflowStub(val []byte) bool {
	return len(val) == overflowStubLen && val[0] == overflowMarker
}

// encodeOverflowStub builds a 13-byte leaf stub for an overflow value.
func encodeOverflowStub(logicalLen uint32, firstPageID uint64) []byte {
	buf := make([]byte, overflowStubLen)
	buf[0] = overflowMarker
	binary.BigEndian.PutUint32(buf[1:5], logicalLen)
	binary.BigEndian.PutUint64(buf[5:13], firstPageID)
	return buf
}

// decodeOverflowStub extracts logical length and first page ID from a stub.
func decodeOverflowStub(stub []byte) (logicalLen uint32, firstPageID uint64) {
	logicalLen = binary.BigEndian.Uint32(stub[1:5])
	firstPageID = binary.BigEndian.Uint64(stub[5:13])
	return
}

// writeOverflowChain writes data across one or more overflow pages and returns
// the page ID of the first page in the chain. Pages are allocated via NextPageID.
func (t *BTree) writeOverflowChain(data []byte) (uint64, error) {
	ps := t.pageSize()
	chunkSize := overflowPayloadPerPage(ps)
	if chunkSize <= 0 {
		return 0, fmt.Errorf("%w: page too small for overflow", ErrCorruptTree)
	}

	// Pre-calculate number of pages needed so we can allocate contiguously.
	nPages := (len(data) + chunkSize - 1) / chunkSize
	if nPages == 0 {
		nPages = 1
	}

	pageIDs := make([]uint64, nPages)
	for i := range nPages {
		id, err := t.allocPageID()
		if err != nil {
			return 0, err
		}
		pageIDs[i] = id
		// Bump NextPageID in header for the next allocation.
		if err := t.eng.UpdateHeader(func(h *Header) {
			h.NextPageID = id + 1
		}); err != nil {
			return 0, err
		}
	}

	off := 0
	for i, pid := range pageIDs {
		page := make([]byte, ps)

		// Next pointer: 0 for last page, otherwise the next page ID.
		var nextID uint64
		if i+1 < len(pageIDs) {
			nextID = pageIDs[i+1]
		}
		binary.BigEndian.PutUint64(page[0:8], nextID)

		// Copy payload chunk.
		end := min(off+chunkSize, len(data))
		copy(page[overflowPtrSize:], data[off:end])
		off = end

		if err := t.eng.WritePage(pid, page); err != nil {
			return 0, err
		}
	}

	return pageIDs[0], nil
}

// readOverflowChain reads an overflow chain starting at firstPageID and returns
// the reassembled value. logicalLen is used to pre-allocate the result buffer.
func (t *BTree) readOverflowChain(firstPageID uint64, logicalLen uint32) ([]byte, error) {
	ps := t.pageSize()
	chunkSize := overflowPayloadPerPage(ps)
	result := make([]byte, 0, logicalLen)

	pid := firstPageID
	for pid != 0 {
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			return nil, fmt.Errorf("overflow read page %d: %w", pid, err)
		}
		nextID := binary.BigEndian.Uint64(page[0:8])
		chunk := page[overflowPtrSize:]
		remaining := int(logicalLen) - len(result)
		if remaining < chunkSize {
			chunk = chunk[:remaining]
		}
		result = append(result, chunk...)
		release()
		pid = nextID
	}

	if len(result) != int(logicalLen) {
		return nil, fmt.Errorf("%w: overflow chain length mismatch: got %d want %d", ErrCorruptTree, len(result), logicalLen)
	}
	return result, nil
}

// freeOverflowChain walks an overflow chain and marks pages as dead.
// Since the engine is extend-only (no freelist), this is a no-op for now.
// Overflow pages become dead space reclaimed by Compact.
func (t *BTree) freeOverflowChain(_ uint64) {
	// No-op: extend-only allocator. Dead overflow pages are reclaimed by Compact.
}
