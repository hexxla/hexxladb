// Package engine provides the B+ tree storage engine for HexxlaDB.
// This file integrates cascading splits with the B-tree insertion path.
package engine

import (
	"bytes"
	"fmt"
	"slices"
)

// insertIntoLeafCascade inserts a key/value into a leaf page using cascading splits.
//
// Hexagonal Architecture: This is the primary insertion point for leaf pages.
// It delegates to cascadingLeafSplit for domain logic, then handles I/O (page allocation,
// writing) via the BTree's engine interface.
//
// Parameters:
//   - pid: page ID of the leaf to insert into
//   - page: raw page bytes
//   - key: key to insert
//   - val: value to insert
//
// Returns:
//   - didSplit: true if page was split (1+ new pages created)
//   - result: cascade split result with all pages and separator keys
//   - err: any error during operation
//
// Cyclomatic complexity: O(n) where n = number of entries in page.
// Modern Go: Uses structured result type, explicit error handling.
func (t *BTree) insertIntoLeafCascade(pid uint64, page, key, val []byte) (didSplit bool, result *cascadeSplitResult, err error) {
	ld, err := parseLeafPage(page)
	if err != nil {
		return false, nil, err
	}

	// Insert new key/value in sorted position
	newKeys, newVals, err := insertSorted(ld.keys, ld.vals, key, val)
	if err != nil {
		return false, nil, err
	}

	// Check if page still fits
	newSize := leafSerializedSize(newKeys, newVals)
	if newSize <= t.pageSize() {
		// No split needed - update in place
		pg, err := buildLeafPage(t.pageSize(), ld.parent, ld.next, newKeys, newVals)
		if err != nil {
			return false, nil, err
		}
		if err := t.eng.WritePage(pid, pg); err != nil {
			return false, nil, err
		}
		return false, nil, nil
	}

	// Page overflow - use cascading split
	splits, err := cascadingLeafSplit(newKeys, newVals, t.pageSize())
	if err != nil {
		return false, nil, err
	}

	// Build cascade result
	result = &cascadeSplitResult{
		pages:   make([]cascadePage, len(splits)),
		sepKeys: make([][]byte, len(splits)-1),
	}

	for i, sp := range splits {
		cp := cascadePage{
			keys: sp.keys,
			vals: sp.vals,
		}

		if i == 0 {
			// First page reuses original page ID
			cp.isNew = false
			cp.pageID = pid
		} else {
			// Subsequent pages need new IDs
			cp.isNew = true
		}

		result.pages[i] = cp

		// Set separator key (first key of each page after the first)
		if i > 0 {
			result.sepKeys[i-1] = append([]byte(nil), sp.sepKey...)
		}
	}

	// Write all pages
	nextPageID := ld.next // Rightmost page inherits original's next pointer
	for i := range slices.Backward(result.pages) {
		cp := &result.pages[i]

		// Allocate new ID if needed
		if cp.isNew {
			newID, err := t.allocPageID()
			if err != nil {
				return false, nil, err
			}
			cp.pageID = newID
		}

		// Build and write page
		pg, err := buildLeafPage(t.pageSize(), ld.parent, nextPageID, cp.keys, cp.vals)
		if err != nil {
			// Debug: log the situation
			pageSize := calcRangeSize(cp.keys, cp.vals)
			return false, nil, fmt.Errorf("buildLeafPage failed for page %d (keys=%d, size=%d, pageSize=%d): %w",
				cp.pageID, len(cp.keys), pageSize, t.pageSize(), err)
		}
		if err := t.eng.WritePage(cp.pageID, pg); err != nil {
			return false, nil, err
		}

		// Update next pointer for left page
		nextPageID = cp.pageID
	}

	return true, result, nil
}

// insertSorted inserts a key/value into sorted position.
// Returns new slices (does not modify input).
// Complexity: O(n) for find position + O(n) for copy = O(n).
func insertSorted(keys, vals [][]byte, newKey, newVal []byte) (outKeys, outVals [][]byte, err error) {
	n := len(keys)

	// Find insertion position
	pos := 0
	for pos < n && bytes.Compare(keys[pos], newKey) < 0 {
		pos++
	}

	// Check for duplicate
	if pos < n && bytes.Equal(keys[pos], newKey) {
		// Replace existing value
		newKeys := cloneSliceOfSlices(keys)
		newVals := cloneSliceOfSlices(vals)
		newVals[pos] = append([]byte(nil), newVal...)
		return newKeys, newVals, nil
	}

	// Insert at position
	newKeys := make([][]byte, n+1)
	newVals := make([][]byte, n+1)

	copy(newKeys[:pos], keys[:pos])
	copy(newVals[:pos], vals[:pos])

	newKeys[pos] = append([]byte(nil), newKey...)
	newVals[pos] = append([]byte(nil), newVal...)

	copy(newKeys[pos+1:], keys[pos:])
	copy(newVals[pos+1:], vals[pos:])

	return newKeys, newVals, nil
}
