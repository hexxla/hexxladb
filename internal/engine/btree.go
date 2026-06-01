package engine

import (
	"bytes"
	"encoding/binary"
)

// BTree is a B+ tree stored in engine pages (see ORDERED_STORE.md).
type BTree struct {
	eng *Engine
}

// pageSize returns the engine page size.
func (t *BTree) pageSize() int { return t.eng.pageSize }

// leafSerializedSize returns the on-disk byte count for a leaf with the given entries.
func leafSerializedSize(keys, vals [][]byte) int {
	size := btreeHeaderSize
	for i := range keys {
		size += 4 + len(keys[i]) + len(vals[i]) // keyLen(2) + valLen(2) + key + val
	}
	return size
}

// internalSerializedSize returns the on-disk byte count for an internal node.
func internalSerializedSize(keys [][]byte) int {
	size := btreeHeaderSize + 8 // header + ptr0
	for _, k := range keys {
		size += 2 + len(k) + 8 // keyLen(2) + key + ptr(8)
	}
	return size
}

// leafSplitIndex finds the position i at which to split a leaf so that both
// the left half [0, i) and the right half [i, n) fit within pageSize.
//
// Strategy: Find the midpoint that ensures both halves fit. Start from the
// middle and check both sides. Use a minimum fill threshold to avoid
// degenerate splits.
//
// Invariants:
//   - i is clamped to [minKeysPerPage, len(keys)-minKeysPerPage] so each side
//     has at least minKeysPerPage entries.
//   - Both left [0:i) and right [i:n) halves must fit within pageSize.
//   - Falls back to n/2 if no valid split found (handles edge cases).
func leafSplitIndex(keys, vals [][]byte, pageSize int) int {
	n := len(keys)
	if n < 2*minKeysPerPage {
		return n / 2
	}

	// Helper to calculate size of a range
	calcSize := func(start, end int) int {
		sz := btreeHeaderSize
		for i := start; i < end && i < len(keys); i++ {
			sz += 4 + len(keys[i]) + len(vals[i])
		}
		return sz
	}

	// Start from middle and expand search
	mid := n / 2
	// Check if midpoint works
	if mid >= minKeysPerPage && mid <= n-minKeysPerPage {
		leftSize := calcSize(0, mid)
		rightSize := calcSize(mid, n)
		if leftSize <= pageSize && rightSize <= pageSize {
			return mid
		}
	}

	// Search outward from middle
	for offset := 1; offset <= n/2-minKeysPerPage; offset++ {
		// Try left of middle
		i := mid - offset
		if i >= minKeysPerPage && i <= n-minKeysPerPage {
			leftSize := calcSize(0, i)
			rightSize := calcSize(i, n)
			if leftSize <= pageSize && rightSize <= pageSize {
				return i
			}
		}
		// Try right of middle
		j := mid + offset
		if j >= minKeysPerPage && j <= n-minKeysPerPage {
			leftSize := calcSize(0, j)
			rightSize := calcSize(j, n)
			if leftSize <= pageSize && rightSize <= pageSize {
				return j
			}
		}
	}

	// No valid split found - this can happen with highly variable entry sizes
	// where no split point satisfies both size constraints. Return n/2 as
	// best-effort; caller's buildLeafPage will return error if pages overflow.
	// TODO: Implement cascading splits (see bbolt spill() approach) to handle
	// this case by splitting the right side recursively until it fits.
	return n / 2
}

// OpenBTree returns a handle for the on-disk B+ tree rooted in the file header.
func OpenBTree(e *Engine) *BTree {
	return &BTree{eng: e}
}

// allocPageID returns the next page id that WritePage will allocate (see ENGINE_FORMAT).
func (t *BTree) allocPageID() (uint64, error) {
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return 0, err
	}
	return hdr.NextPageID, nil
}

func (t *BTree) setParent(pageID, parentID uint64) error {
	page, release, err := t.eng.readPagePooled(pageID)
	if err != nil {
		return err
	}
	defer release()
	binary.BigEndian.PutUint64(page[16:24], parentID)
	return t.eng.WritePage(pageID, page)
}

// GetUsingRoot traverses from root without reading the database header on-disk.
// Caller must supply a btree root captured at snapshot open when consistency requires it.
func (t *BTree) GetUsingRoot(root uint64, key []byte) (val []byte, ok bool, err error) {
	if root == 0 {
		return nil, false, nil
	}
	pid := root
	for {
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			return nil, false, err
		}
		switch page[5] {
		case btreeKindLeaf:
			ld, err := parseLeafPage(page)
			release()
			if err != nil {
				return nil, false, err
			}
			i := leafKeyIndex(ld.keys, key)
			if i < len(ld.keys) && bytes.Equal(ld.keys[i], key) {
				v := ld.vals[i]
				if isOverflowStub(v) {
					logLen, firstPage := decodeOverflowStub(v)
					v, err = t.readOverflowChain(firstPage, logLen)
					if err != nil {
						return nil, false, err
					}
				}
				if isCompressedValue(v) {
					v, err = decompressValue(v)
					if err != nil {
						return nil, false, err
					}
				}
				return v, true, nil
			}
			return nil, false, nil
		case btreeKindInternal:
			in, err := parseInternalPage(page)
			release()
			if err != nil {
				return nil, false, err
			}
			ci := internalPickChild(in.keys, key)
			pid = in.ptrs[ci]
		default:
			release()
			return nil, false, ErrCorruptTree
		}
	}
}

// Get returns the value for key, or (nil, false, nil) if missing.
func (t *BTree) Get(key []byte) (val []byte, ok bool, err error) {
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return nil, false, err
	}
	return t.GetUsingRoot(hdr.BTreeRoot, key)
}

// Put inserts or replaces a key/value pair.
// Values larger than inlineThreshold are stored in overflow pages; a 14-byte
// stub is placed in the leaf entry.
func (t *BTree) Put(key, val []byte) error {
	if len(key) > maxKeyBytes {
		return ErrKeyTooLarge
	}
	if uint32(len(val)) > t.eng.maxValueBytes { //nolint:gosec // G115: len() is always non-negative; conversion is safe
		return ErrValueTooLarge
	}

	// Compress the value. Compression runs before the overflow check so
	// that compressible values may fit inline.
	storeVal := compressValue(val)

	// Spill to overflow pages if value exceeds the inline threshold.
	leafVal := storeVal
	if len(storeVal) > inlineThreshold(t.pageSize()) {
		firstPage, err := t.writeOverflowChain(storeVal)
		if err != nil {
			return err
		}
		leafVal = encodeOverflowStub(uint32(len(storeVal)), firstPage) //nolint:gosec // len(storeVal) bounded by maxValueBytes
	}

	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return err
	}
	if hdr.BTreeRoot == 0 {
		return t.putFirst(key, leafVal)
	}
	refs, err := t.insertAt(hdr.BTreeRoot, key, leafVal)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	return t.growRoot(hdr.BTreeRoot, refs)
}

// growRoot installs one or more new internal levels above oldRoot after a split
// propagated promotions to the top of the tree. If the promoted children do not
// all fit in a single new root page, the new level is itself spilled and the
// process repeats, increasing tree height by more than one if necessary.
func (t *BTree) growRoot(oldRoot uint64, refs []childRef) error {
	ptrs := make([]uint64, 0, len(refs)+1)
	ptrs = append(ptrs, oldRoot)
	keys := make([][]byte, 0, len(refs))
	for _, r := range refs {
		keys = append(keys, r.sepKey)
		ptrs = append(ptrs, r.pageID)
	}
	for {
		newRoot, err := t.allocPageID()
		if err != nil {
			return err
		}
		moreRefs, err := t.spillInternal(newRoot, 0, ptrs, keys)
		if err != nil {
			return err
		}
		if len(moreRefs) == 0 {
			return t.eng.UpdateHeader(func(h *Header) { h.BTreeRoot = newRoot })
		}
		// The new root level itself overflowed; build the next level up.
		ptrs = make([]uint64, 0, len(moreRefs)+1)
		ptrs = append(ptrs, newRoot)
		keys = make([][]byte, 0, len(moreRefs))
		for _, r := range moreRefs {
			keys = append(keys, r.sepKey)
			ptrs = append(ptrs, r.pageID)
		}
	}
}

func (t *BTree) putFirst(key, val []byte) error {
	id, err := t.allocPageID()
	if err != nil {
		return err
	}
	page, err := buildLeafPage(t.pageSize(), 0, 0, [][]byte{append([]byte(nil), key...)}, [][]byte{append([]byte(nil), val...)})
	if err != nil {
		return err
	}
	if err := t.eng.WritePage(id, page); err != nil {
		return err
	}
	return t.eng.UpdateHeader(func(h *Header) {
		h.BTreeRoot = id
	})
}

func (t *BTree) insertAt(pid uint64, key, val []byte) ([]childRef, error) {
	page, release, err := t.eng.readPagePooled(pid)
	if err != nil {
		return nil, err
	}
	defer release()
	switch page[5] {
	case btreeKindLeaf:
		return t.insertLeafRefs(pid, page, key, val)
	case btreeKindInternal:
		return t.insertIntoInternal(pid, page, key, val)
	default:
		return nil, ErrCorruptTree
	}
}

// insertLeafRefs inserts into a leaf using cascading splits and returns the
// promoted children (the new right-side pages) for the parent to splice in.
// A nil/empty result means the entry fit without splitting. The leftmost page
// always reuses pid, so it is never returned as a promotion.
func (t *BTree) insertLeafRefs(pid uint64, page, key, val []byte) ([]childRef, error) {
	didSplit, result, err := t.insertIntoLeafCascade(pid, page, key, val)
	if err != nil {
		return nil, err
	}
	if !didSplit {
		return nil, nil
	}
	refs := make([]childRef, 0, len(result.pages)-1)
	for i := 1; i < len(result.pages); i++ {
		refs = append(refs, childRef{
			sepKey: result.sepKeys[i-1],
			pageID: result.pages[i].pageID,
		})
	}
	return refs, nil
}

// insertIntoInternal descends into the correct child, then splices any promoted
// children returned from below into this node, spilling this node into multiple
// fitting pages if it overflows. Returns the promotions this node produces (if
// any) for its own parent.
func (t *BTree) insertIntoInternal(pid uint64, page, key, val []byte) ([]childRef, error) {
	in, err := parseInternalPage(page)
	if err != nil {
		return nil, err
	}
	ci := internalPickChild(in.keys, key)
	child := in.ptrs[ci]
	childRefs, err := t.insertAt(child, key, val)
	if err != nil {
		return nil, err
	}
	if len(childRefs) == 0 {
		return nil, nil
	}
	// Splice the promoted children in immediately after the original child slot.
	newPtrs := make([]uint64, 0, len(in.ptrs)+len(childRefs))
	newPtrs = append(newPtrs, in.ptrs[:ci+1]...)
	newKeys := make([][]byte, 0, len(in.keys)+len(childRefs))
	newKeys = append(newKeys, in.keys[:ci]...)
	for _, r := range childRefs {
		newKeys = append(newKeys, r.sepKey)
		newPtrs = append(newPtrs, r.pageID)
	}
	newPtrs = append(newPtrs, in.ptrs[ci+1:]...)
	newKeys = append(newKeys, in.keys[ci:]...)
	return t.spillInternal(pid, in.parent, newPtrs, newKeys)
}

// AscendRangeFromRoot calls fn for every key in [from, to] inclusive using the
// provided root page ID. Callers that already hold a snapshot root (e.g. read-only
// Tx with cachedBTreeRoot) should prefer this to avoid a ReadHeader pread.
func (t *BTree) AscendRangeFromRoot(root uint64, from, to []byte, fn func(k, v []byte) bool) error {
	if root == 0 {
		return nil
	}
	pid, err := t.leftmostLeaf(root, from)
	if err != nil {
		return err
	}
	started := false
	for pid != 0 {
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			return err
		}
		ld, err := parseLeafPage(page)
		release()
		if err != nil {
			return err
		}
		i := 0
		if !started && from != nil {
			i = leafKeyIndex(ld.keys, from)
		}
		started = true
		for ; i < len(ld.keys); i++ {
			k := ld.keys[i]
			if to != nil && bytes.Compare(k, to) > 0 {
				return nil
			}
			v := ld.vals[i]
			if isOverflowStub(v) {
				logLen, firstPage := decodeOverflowStub(v)
				v, err = t.readOverflowChain(firstPage, logLen)
				if err != nil {
					return err
				}
			}
			if isCompressedValue(v) {
				v, err = decompressValue(v)
				if err != nil {
					return err
				}
			}
			if !fn(k, v) {
				return nil
			}
		}
		pid = ld.next
	}
	return nil
}

// AscendRange calls fn for every key in [from, to] inclusive (byte order). If from is nil, start at the smallest key.
func (t *BTree) AscendRange(from, to []byte, fn func(k, v []byte) bool) error {
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return err
	}
	if hdr.BTreeRoot == 0 {
		return nil
	}
	pid, err := t.leftmostLeaf(hdr.BTreeRoot, from)
	if err != nil {
		return err
	}
	started := false
	for pid != 0 {
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			return err
		}
		ld, err := parseLeafPage(page)
		release()
		if err != nil {
			return err
		}
		i := 0
		if !started && from != nil {
			i = leafKeyIndex(ld.keys, from)
		}
		started = true
		for ; i < len(ld.keys); i++ {
			k := ld.keys[i]
			if to != nil && bytes.Compare(k, to) > 0 {
				return nil
			}
			v := ld.vals[i]
			if isOverflowStub(v) {
				logLen, firstPage := decodeOverflowStub(v)
				v, err = t.readOverflowChain(firstPage, logLen)
				if err != nil {
					return err
				}
			}
			if isCompressedValue(v) {
				v, err = decompressValue(v)
				if err != nil {
					return err
				}
			}
			if !fn(k, v) {
				return nil
			}
		}
		pid = ld.next
	}
	return nil
}

// DescendRangeFromRoot calls fn for every key in [from, to] inclusive in descending
// (reverse) byte order using the provided root page ID. If to is nil, starts at the
// largest key. Stops when fn returns false or all keys in range are visited.
//
// This is used by [resolveReadSeqAtOrBeforeUnixNano] to find the largest commit
// timestamp ≤ asOf without scanning from the beginning of the keyspace.
func (t *BTree) DescendRangeFromRoot(root uint64, from, to []byte, fn func(k, v []byte) bool) error {
	if root == 0 {
		return nil
	}
	pid, err := t.rightmostLeaf(root, to)
	if err != nil {
		return err
	}
	for pid != 0 {
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			return err
		}
		ld, err := parseLeafPage(page)
		release()
		if err != nil {
			return err
		}
		// Determine the start index (rightmost entry ≤ to).
		iEnd := len(ld.keys) - 1
		if to != nil {
			iEnd = len(ld.keys) - 1
			for iEnd >= 0 && bytes.Compare(ld.keys[iEnd], to) > 0 {
				iEnd--
			}
		}
		for i := iEnd; i >= 0; i-- {
			k := ld.keys[i]
			if from != nil && bytes.Compare(k, from) < 0 {
				return nil
			}
			v := ld.vals[i]
			if isOverflowStub(v) {
				logLen, firstPage := decodeOverflowStub(v)
				v, err = t.readOverflowChain(firstPage, logLen)
				if err != nil {
					return err
				}
			}
			if isCompressedValue(v) {
				v, err = decompressValue(v)
				if err != nil {
					return err
				}
			}
			if !fn(k, v) {
				return nil
			}
		}
		prev, err := t.prevLeaf(pid, ld.parent, root)
		if err != nil {
			return err
		}
		pid = prev
	}
	return nil
}

// rightmostLeaf returns the page ID of the rightmost leaf whose keys are ≤ to
// (or the absolute rightmost leaf if to is nil).
func (t *BTree) rightmostLeaf(root uint64, to []byte) (uint64, error) {
	pid := root
	for {
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			return 0, err
		}
		if page[5] == btreeKindLeaf {
			release()
			return pid, nil
		}
		in, err := parseInternalPage(page)
		release()
		if err != nil {
			return 0, err
		}
		if to == nil {
			pid = in.ptrs[len(in.ptrs)-1]
			continue
		}
		ci := internalPickChild(in.keys, to)
		pid = in.ptrs[ci]
	}
}

// prevLeaf returns the page ID of the leaf immediately before leafPID in the
// linked list (left sibling), or 0 if leafPID is the leftmost leaf. It uses the
// parent pointer chain to find the left sibling without storing a prev pointer.
func (t *BTree) prevLeaf(leafPID, parentPID, root uint64) (uint64, error) {
	if parentPID == 0 || leafPID == root {
		return 0, nil
	}
	page, release, err := t.eng.readPagePooled(parentPID)
	if err != nil {
		return 0, err
	}
	in, err := parseInternalPage(page)
	parentParent := in.parent
	release()
	if err != nil {
		return 0, err
	}
	for i, ptr := range in.ptrs {
		if ptr == leafPID {
			if i == 0 {
				// No left sibling at this level — go up.
				return t.prevLeaf(parentPID, parentParent, root)
			}
			// Descend into left sibling to find its rightmost leaf.
			return t.rightmostLeaf(in.ptrs[i-1], nil)
		}
	}
	return 0, nil
}

func (t *BTree) leftmostLeaf(root uint64, from []byte) (uint64, error) {
	pid := root
	for {
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			return 0, err
		}
		if page[5] == btreeKindLeaf {
			release()
			return pid, nil
		}
		in, err := parseInternalPage(page)
		release()
		if err != nil {
			return 0, err
		}
		if from == nil {
			pid = in.ptrs[0]
			continue
		}
		ci := internalPickChild(in.keys, from)
		pid = in.ptrs[ci]
	}
}
