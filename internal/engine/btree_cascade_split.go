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

// cascadingLeafSplit partitions an overfull leaf into ordered, page-sized
// groups. Prefer a byte-balanced split when two pages can hold the entries.
func cascadingLeafSplit(keys, vals [][]byte, pageSize int) ([]leafSplit, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	capacity := pageSize - btreeHeaderSize
	total := 0
	for i, key := range keys {
		size := entrySize(key, vals[i])
		if size > capacity {
			return nil, fmt.Errorf("single entry size %d exceeds page capacity %d", size, capacity)
		}
		total += size
	}
	if total <= capacity {
		return []leafSplit{makeLeafSplit(keys, vals)}, nil
	}

	best, balance, left := 0, 0, 0
	for i, key := range keys[:len(keys)-1] {
		left += entrySize(key, vals[i])
		right := total - left
		if left <= capacity && right <= capacity && min(left, right) > balance {
			best, balance = i+1, min(left, right)
		}
	}
	if best != 0 {
		return []leafSplit{makeLeafSplit(keys[:best], vals[:best]), makeLeafSplit(keys[best:], vals[best:])}, nil
	}

	// Some indivisible entry sizes require more than two pages.
	var groups []leafSplit
	start, used := 0, 0
	for i, key := range keys {
		size := entrySize(key, vals[i])
		if used+size > capacity {
			groups = append(groups, makeLeafSplit(keys[start:i], vals[start:i]))
			start, used = i, 0
		}
		used += size
	}
	return append(groups, makeLeafSplit(keys[start:], vals[start:])), nil
}

func makeLeafSplit(keys, vals [][]byte) leafSplit {
	return leafSplit{keys: cloneSliceOfSlices(keys), vals: cloneSliceOfSlices(vals), sepKey: keys[0]}
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
