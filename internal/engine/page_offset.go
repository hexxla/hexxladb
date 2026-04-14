package engine

import (
	"math"
	"math/bits"
)

// pageByteOffset returns the file offset for page pageID, or ErrBadPageID on overflow.
func pageByteOffset(pageID uint64) (int64, error) {
	hi, lo := bits.Mul64(pageID, uint64(PageSize))
	if hi != 0 || lo > uint64(math.MaxInt64) {
		return 0, ErrBadPageID
	}
	return int64(lo), nil
}
