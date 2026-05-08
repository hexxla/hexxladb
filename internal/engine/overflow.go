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
//	[0..1]   0xFF 0x4F (overflow magic; 0xFF cannot be the first byte of any
//	          record envelope ('H'=0x48) or empty secondary index value)
//	[2..5]   uint32 big-endian — logical value length
//	[6..13]  uint64 big-endian — first overflow page ID
//
// Total stub size: 14 bytes. Always fits inline in any leaf entry.

const (
	overflowMagic0  = byte(0xFF) // first byte — cannot be first byte of any record envelope
	overflowMagic1  = byte(0x4F) // second byte — 'O' for overflow
	overflowStubLen = 2 + 4 + 8  // magic(2) + logicalLen(4) + pageID(8)
	overflowPtrSize = 8          // next-page pointer at start of each overflow page
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
	return len(val) == overflowStubLen && val[0] == overflowMagic0 && val[1] == overflowMagic1
}

// encodeOverflowStub builds a 14-byte leaf stub for an overflow value.
func encodeOverflowStub(logicalLen uint32, firstPageID uint64) []byte {
	buf := make([]byte, overflowStubLen)
	buf[0] = overflowMagic0
	buf[1] = overflowMagic1
	binary.BigEndian.PutUint32(buf[2:6], logicalLen)
	binary.BigEndian.PutUint64(buf[6:14], firstPageID)
	return buf
}

// decodeOverflowStub extracts logical length and first page ID from a stub.
func decodeOverflowStub(stub []byte) (logicalLen uint32, firstPageID uint64) {
	logicalLen = binary.BigEndian.Uint32(stub[2:6])
	firstPageID = binary.BigEndian.Uint64(stub[6:14])
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

	// Allocate all page IDs in one header update (O(1) instead of O(N)).
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return 0, err
	}
	firstID := hdr.NextPageID
	pageIDs := make([]uint64, nPages)
	for i := range nPages {
		pageIDs[i] = firstID + uint64(i)
	}
	if err := t.eng.UpdateHeader(func(h *Header) {
		h.NextPageID = firstID + uint64(nPages) //nolint:gosec // G115: nPages derived from len(); always non-negative
	}); err != nil {
		return 0, err
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

// freeOverflowChain walks an overflow chain and accounts for the wasted bytes.
// Pages are not physically reclaimed (no freelist — dead space is reclaimed by Compact),
// but the cumulative waste is tracked in [Engine.wastedBytes] for operator observability.
func (t *BTree) freeOverflowChain(firstPageID uint64) {
	ps := t.pageSize()
	payload := uint64(overflowPayloadPerPage(ps)) //nolint:gosec // G115: ps >= overflowPtrSize always
	pid := firstPageID
	for pid != 0 {
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			release()
			return
		}
		nextID := binary.BigEndian.Uint64(page[0:8])
		release()
		t.eng.wastedBytes.Add(payload)
		pid = nextID
	}
}
