package engine

import (
	"math"
	"math/bits"
)

// pageByteOffset returns the file offset for page pageID. Page 0 occupies one
// logical page; data pages may use a larger authenticated physical stride.
func pageByteOffset(pageID uint64, pageSize, physicalPageSize int) (int64, error) {
	if pageID == 0 {
		return 0, nil
	}
	hi, lo := bits.Mul64(pageID-1, uint64(physicalPageSize)) //nolint:gosec // physicalPageSize is validated positive.
	if hi != 0 || lo > uint64(math.MaxInt64) {
		return 0, ErrBadPageID
	}
	logicalHeaderBytes := uint64(pageSize) //nolint:gosec // pageSize is validated positive.
	if lo > uint64(math.MaxInt64)-logicalHeaderBytes {
		return 0, ErrBadPageID
	}
	return int64(lo + logicalHeaderBytes), nil //nolint:gosec // the preceding bound proves this fits int64.
}
