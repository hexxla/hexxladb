// Package engine provides the B+ tree storage engine for HexxlaDB.
// This file implements cascading leaf splits for handling highly variable entry sizes.
package engine

import (
	"fmt"
)

// leafSplit represents a single page produced from a split operation.
// Each page has a unique set of keys/values and a separator key for parent indexing.
type leafSplit struct {
	keys   [][]byte // Keys stored in this page
	vals   [][]byte // Values stored in this page
	sepKey []byte   // Separator key (first key of this page, used by parent)
}

// cascadingLeafSplit splits a leaf node's entries into multiple pages.
//
// When entry sizes are highly variable, a single split may leave the right side
// overflowing. This function recursively splits until all pages fit within pageSize.
//
// Hexagonal Architecture: Pure function with no side effects. All I/O (page allocation,
// writing) is handled by the caller via returned split definitions.
//
// Parameters:
//   - keys: sorted keys to distribute across pages
//   - vals: values corresponding to keys
//   - pageSize: maximum bytes per page
//
// Returns:
//   - []leafSplit: ordered list of pages (left to right), empty on error
//   - error: if entries cannot be split (e.g., single entry exceeds pageSize)
//
// Complexity: O(n*k) where n=entries, k=pages created. Typically k <= 3.
func cascadingLeafSplit(keys, vals [][]byte, pageSize int) ([]leafSplit, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	// Validate: single entry must fit (overflow pages handled separately via MaxValueBytes)
	if len(keys) == 1 {
		size := entrySize(keys[0], vals[0])
		if size > pageSize {
			return nil, fmt.Errorf("single entry size %d exceeds pageSize %d", size, pageSize)
		}
		return []leafSplit{{keys: keys, vals: vals, sepKey: keys[0]}}, nil
	}

	return splitRecursively(keys, vals, pageSize, make([]leafSplit, 0))
}

// splitRecursively splits entries into pages that each fit within pageSize.
//
// Design: greedy left-fill (bbolt-style). It packs as many leading entries as
// fit into the left page, then recurses on the remainder. This guarantees the
// crucial invariant that EVERY emitted page serializes within pageSize — unlike
// a midpoint split, which can leave either half overflowing when entry sizes are
// highly variable. The accumulator pattern keeps it allocation-light.
//
// The only error condition is a single entry larger than pageSize; callers spill
// such values to overflow pages before reaching here, so inline entries are
// always bounded below pageSize.
func splitRecursively(keys, vals [][]byte, pageSize int, acc []leafSplit) ([]leafSplit, error) {
	size := btreeHeaderSize
	i := 0
	for i < len(keys) {
		entry := entrySize(keys[i], vals[i])
		// Always place at least one entry per page; stop before overflowing.
		if i > 0 && size+entry > pageSize {
			break
		}
		size += entry
		i++
		if i == 1 && size > pageSize {
			return nil, fmt.Errorf("single entry size %d exceeds pageSize %d", entry, pageSize)
		}
	}

	leftKeys := cloneSliceOfSlices(keys[:i])
	leftVals := cloneSliceOfSlices(vals[:i])
	acc = append(acc, leafSplit{keys: leftKeys, vals: leftVals, sepKey: leftKeys[0]})

	if i == len(keys) {
		return acc, nil
	}
	return splitRecursively(keys[i:], vals[i:], pageSize, acc)
}

// entrySize calculates the serialized size of one key-value pair.
func entrySize(key, val []byte) int {
	return 4 + len(key) + len(val) // 4 bytes for length prefix + key + value
}

// calcRangeSize calculates total serialized size for a range of entries.
func calcRangeSize(keys, vals [][]byte) int {
	sz := btreeHeaderSize
	for i := range keys {
		sz += entrySize(keys[i], vals[i])
	}
	return sz
}

// cloneSliceOfSlices creates a deep copy of a [][]byte slice.
//
// Design: Defensive copy to prevent accidental mutation of shared backing arrays.
// Modern Go: Uses preallocation with make + copy for efficiency.
func cloneSliceOfSlices(src [][]byte) [][]byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([][]byte, len(src))
	for i, s := range src {
		dst[i] = make([]byte, len(s))
		copy(dst[i], s)
	}
	return dst
}
