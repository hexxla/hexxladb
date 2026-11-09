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
//	[0..1]   0xFF 0x4F (overflow magic, escaped when present in raw values)
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
	// Use pageSize/3 to ensure we can fit at least 2-3 entries per page
	// even with some variance in entry sizes. This prevents the B-tree
	// split logic from failing when entries are moderately large.
	// Previous formula (pageSize - header - maxKey - overhead) was too permissive,
	// allowing entries that individually fit but cumulatively overflow pages.
	t := max(pageSize/3, 256)
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
// the page ID of the first page in the chain. Authenticated transactions reuse
// freelist pages before extending NextPageID.
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
	if t.eng.wtxn != nil && t.eng.wtxn.hdr.FormatVersion == formatVersionV3 {
		for i := range nPages {
			pageID, err := t.allocPageID()
			if err != nil {
				return 0, err
			}
			pageIDs[i] = pageID
		}
	} else {
		// Legacy formats retain the contiguous extend-only reservation.
		hdr, err := t.eng.ReadHeader()
		if err != nil {
			return 0, err
		}
		firstID := hdr.NextPageID
		for i := range nPages {
			pageIDs[i] = firstID + uint64(i)
		}
		if err := t.eng.UpdateHeader(func(h *Header) {
			h.NextPageID = firstID + uint64(nPages) //nolint:gosec // G115: nPages derived from len(); always non-negative
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

// overflowLength validates a stored length before any allocation. An escaped
// maximum-sized raw value can be larger than the logical write limit after
// DEFLATE framing. Even incompressible DEFLATE blocks fit under twice that limit.
func overflowLength(logicalLen uint32) (int, error) {
	if logicalLen == 0 || logicalLen > 2*maxSupportedValueBytes {
		return 0, fmt.Errorf("%w: invalid overflow length %d", ErrCorruptTree, logicalLen)
	}
	return int(logicalLen), nil
}

func (t *BTree) readOverflowChain(firstPageID uint64, logicalLen uint32) ([]byte, error) {
	length, err := overflowLength(logicalLen)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 0, length)
	err = t.walkOverflowChain(firstPageID, length, func(_ uint64, chunk []byte) {
		result = append(result, chunk...)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// walkOverflowChain requires the physical chain to contain exactly the number
// of pages implied by length. A cycle cannot satisfy the terminal check.
func (t *BTree) walkOverflowChain(pid uint64, length int, visit func(uint64, []byte)) error {
	chunkSize := overflowPayloadPerPage(t.pageSize())
	for length > 0 {
		if pid == 0 {
			return fmt.Errorf("%w: short overflow chain", ErrCorruptTree)
		}
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			return fmt.Errorf("overflow read page %d: %w", pid, err)
		}
		next := binary.BigEndian.Uint64(page[:overflowPtrSize])
		size := min(length, chunkSize)
		visit(pid, page[overflowPtrSize:overflowPtrSize+size])
		release()
		pid, length = next, length-size
	}
	if pid != 0 {
		return fmt.Errorf("%w: excess pages in overflow chain", ErrCorruptTree)
	}
	return nil
}

// freeOverflowChain validates the full chain before making any page reusable.
func (t *BTree) freeOverflowChain(firstPageID uint64, logicalLen uint32) error {
	length, err := overflowLength(logicalLen)
	if err != nil {
		return err
	}
	var pages []uint64
	if err := t.walkOverflowChain(firstPageID, length, func(id uint64, _ []byte) {
		pages = append(pages, id)
	}); err != nil {
		return err
	}
	for _, id := range pages {
		if err := t.eng.releasePageID(id); err != nil {
			return err
		}
	}
	return nil
}
