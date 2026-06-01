# Cascading Leaf Splits Implementation Plan

## Problem Statement

The current B+ tree leaf split logic assumes that after a single split, both the left and right halves will fit within a single page. With highly variable entry sizes (e.g., payloads ranging from 1 byte to 10KB), the right side may still overflow the page size (4096 bytes).

**Error observed:**
```
leaf page overflow after split: right side has 4 entries totaling 4155 bytes, pageSize=4096
```

**Root cause:** `leafSplitIndex()` returns a single split point, but when entry sizes vary significantly, one split is insufficient.

## Solution: Cascading Leaf Splits

Implement recursive splitting where if the right side still overflows after an initial split, it gets split again (and again) until all pages fit.

## Implementation Phases

### Phase 1: Data Structures (Day 1)

**1.1 Define split result type**

```go
// leafSplitResult represents the result of splitting a leaf node.
// A single split may produce 2+ pages if the right side cascades.
type leafSplitResult struct {
	pages []splitPage // Ordered left-to-right
}

// splitPage represents one page from a split
type splitPage struct {
	pid     uint64   // New page ID (0 for left/original page)
	keys    [][]byte // Keys for this page
	vals    [][]byte // Values for this page
	sepKey  []byte   // Separator key (first key of this page)
}
```

**1.2 Modify leaf split algorithm**

Replace `leafSplitIndex()` with `cascadingLeafSplit()`:

```go
// cascadingLeafSplit splits keys/vals into multiple pages if needed.
// Returns 2+ pages when variance requires cascading splits.
func cascadingLeafSplit(keys, vals [][]byte, pageSize int) (*leafSplitResult, error) {
	var pages []splitPage
	remainingKeys := keys
	remainingVals := vals

	for len(remainingKeys) > 0 {
		// Find best split point for remaining entries
		splitIdx := findSplitPoint(remainingKeys, remainingVals, pageSize)

		// Extract left page content
		leftKeys := remainingKeys[:splitIdx]
		leftVals := remainingVals[:splitIdx]

		// Add left page to result
		pages = append(pages, splitPage{
			keys:   leftKeys,
			vals:   leftVals,
			sepKey: remainingKeys[0], // First key of this page
		})

		// Move to remaining entries for next iteration
		remainingKeys = remainingKeys[splitIdx:]
		remainingVals = remainingVals[splitIdx:]

		// If remaining fits in one page, we're done
		if calcSize(remainingKeys, remainingVals) <= pageSize {
			pages = append(pages, splitPage{
				keys:   remainingKeys,
				vals:   remainingVals,
				sepKey: remainingKeys[0],
			})
			break
		}
		// Otherwise loop again (cascading split)
	}

	return &leafSplitResult{pages: pages}, nil
}
```

### Phase 2: Core B-Tree Modifications (Days 1-2)

**2.1 Modify `insertIntoLeaf()`**

Current signature returns single page ID:
```go
func (t *BTree) insertIntoLeaf(pid uint64, ...)
    (split bool, newPID uint64, sepKey []byte, err error)
```

New signature returns multiple pages:
```go
func (t *BTree) insertIntoLeaf(pid uint64, ...)
    (split bool, newPages []uint64, sepKeys [][]byte, err error)
```

**Implementation steps:**
1. Build leaf data (keys/vals) from page
2. Insert new key/value at correct position
3. Check if page overflows
4. If overflow, call `cascadingLeafSplit()`
5. Write all new pages to disk
6. Return all new page IDs and separator keys

**2.2 Modify `insertIntoInternal()`**

Internal nodes must handle receiving **multiple** new children from a leaf split:

```go
// Current: receives one new child
// New: receives slice of new children
func (t *BTree) insertIntoInternal(pid uint64, idx int,
    newPIDs []uint64, sepKeys [][]byte, ...)
```

**Implementation:**
- Insert multiple separator keys and child pointers
- Check if internal node overflows
- If overflow, split internal node (may cascade up to root)

**2.3 Propagation to root**

If internal node splits propagate to root:
- Tree height may increase
- New root created with two children

### Phase 3: Page Building Updates (Day 2)

**3.1 Update `buildLeafPage` calls**

Instead of building 2 pages (left/right), build N pages:

```go
for i, page := range result.pages {
    var newPID uint64
    if i == 0 {
        newPID = pid // Reuse original page for first split
    } else {
        newPID, err = t.allocPageID()
        // ...
    }

    pg, err := buildLeafPage(t.pageSize(), ld.parent,
        nextPageID, page.keys, page.vals)
    // ...
}
```

**3.2 Update overflow handling**

If right side overflows, instead of error:
- Continue splitting
- Track all pages created
- Update parent with all new page references

### Phase 4: Testing Strategy (Day 3)

**4.1 Unit tests for `cascadingLeafSplit()`**

| Test Case | Description |
|-----------|-------------|
| `TestCascadingSplit_TwoPages` | Normal case: 2 pages after split |
| `TestCascadingSplit_ThreePages` | High variance: 3+ pages |
| `TestCascadingSplit_EqualSizes` | Low variance: exactly 2 pages |
| `TestCascadingSplit_MinKeys` | Edge case: minimum keys per page |

**4.2 Integration tests**

| Test Case | Description |
|-----------|-------------|
| `TestStress_concurrentReaders` | Original failing test |
| `TestStress_variablePayloadChaos` | Random payloads 1B-10KB |
| `TestStress_ascendingSizes` | Systematically increasing sizes |
| `TestStress_descendingSizes` | Systematically decreasing sizes |

**4.3 Regression tests**

- Run all existing `btree_*_test.go` tests
- Verify 500K cell test still passes
- Verify backward compatibility with existing databases

### Phase 5: Edge Cases & Validation (Day 3-4)

**5.1 Edge cases to handle**

- Single entry larger than page (should use overflow pages via MaxValueBytes)
- All entries exactly same size (uniform split)
- Empty page handling
- Minimum keys constraint violation

**5.2 Validation checks**

```go
// After split, verify:
- Each page has >= minKeysPerPage entries
- Each page total size <= pageSize
- All keys maintained in order
- No keys lost during split
- Separator keys correctly identify page boundaries
```

## Reference Implementation

**bbolt pattern (node.go:spill()):**

```go
// spill writes the node to one or more pages
func (n *node) spill() error {
    // If node is larger than page, split into multiple nodes
    if len(n.inodes) > n.bucket.tx.db.pageSize/inodeSize {
        // Split into left/right
        // If right still too large, spill() recursively
    }
    // Write each node to page
}
```

## Success Criteria

1. ✅ `TestStress_concurrentReaders` passes (1000 cells, 10KB max payload)
2. ✅ All existing tests pass (no regression)
3. ✅ 500K cell test still completes in <10 minutes
4. ✅ Space utilization > 70% (not too fragmented)
5. ✅ No panic or corruption on any input

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Performance regression | Benchmark before/after with `btree_bench_test.go` |
| Tree height explosion | Limit cascade depth, monitor with metrics |
| Backward incompatibility | Existing DBs unaffected (only affects new splits) |
| Complex bugs | Extensive testing, property-based tests |

## Timeline

| Phase | Days | Deliverable |
|-------|------|-------------|
| 1 | 1 | Data structures, cascading algorithm |
| 2 | 1-2 | Core B-tree modifications |
| 3 | 1 | Page building updates |
| 4 | 1 | Comprehensive test suite |
| 5 | 0.5-1 | Edge cases, validation |

**Total: 4.5-6 days**

## Next Steps

1. ✅ Create feature branch `feat/cascading-leaf-splits`
2. 🔄 Implement Phase 1 (data structures)
3. ⏳ Implement Phase 2 (core modifications)
4. ⏳ Implement Phase 3 (page building)
5. ⏳ Implement Phase 4-5 (testing)

Ready to begin Phase 1?
