package engine

import (
	"bytes"
	"encoding/binary"
	"slices"
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

// OpenBTree returns a handle for the on-disk B+ tree rooted in the file header.
func OpenBTree(e *Engine) *BTree {
	return &BTree{eng: e}
}

// allocPageID returns a reusable authenticated page when possible, otherwise
// reserves the next page id (see ENGINE_FORMAT).
func (t *BTree) allocPageID() (uint64, error) {
	if pageID, ok := t.eng.allocateReusablePageID(); ok {
		return pageID, nil
	}
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return 0, err
	}
	if t.eng.wtxn != nil && hdr.FormatVersion == formatVersionV3 {
		t.eng.wtxn.hdr.NextPageID++
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
			v, ok, err := lookupLeafValue(page, key)
			if ok {
				v = bytes.Clone(v)
			}
			release()
			if err != nil {
				return nil, false, err
			}
			if ok {
				if isOverflowStub(v) {
					logLen, firstPage := decodeOverflowStub(v)
					v, err = t.readOverflowChain(firstPage, logLen)
					if err != nil {
						return nil, false, err
					}
				}
				if isCompressedValue(v) {
					v, err = t.eng.decompressValue(v)
					if err != nil {
						return nil, false, err
					}
				}
				return v, true, nil
			}
			return nil, false, nil
		case btreeKindInternal:
			child, err := lookupInternalChild(page, key)
			release()
			if err != nil {
				return nil, false, err
			}
			pid = child
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
	oldOverflowPage, hadOldOverflow, err := t.storedOverflowPage(hdr.BTreeRoot, key)
	if err != nil {
		return err
	}
	refs, err := t.insertAt(hdr.BTreeRoot, key, leafVal)
	if err != nil {
		return err
	}
	if len(refs) != 0 {
		if err := t.growRoot(hdr.BTreeRoot, refs); err != nil {
			return err
		}
	}
	if hadOldOverflow {
		return t.freeOverflowChain(oldOverflowPage)
	}
	return nil
}

// storedOverflowPage returns the first overflow page for an existing raw leaf
// value. It intentionally does not decode the value like GetUsingRoot.
func (t *BTree) storedOverflowPage(root uint64, key []byte) (uint64, bool, error) {
	pid := root
	for {
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			return 0, false, err
		}
		switch page[5] {
		case btreeKindLeaf:
			value, ok, lookupErr := lookupLeafValue(page, key)
			if lookupErr != nil || !ok || !isOverflowStub(value) {
				release()
				return 0, false, lookupErr
			}
			_, firstPage := decodeOverflowStub(value)
			release()
			return firstPage, true, nil
		case btreeKindInternal:
			child, lookupErr := lookupInternalChild(page, key)
			release()
			if lookupErr != nil {
				return 0, false, lookupErr
			}
			pid = child
		default:
			release()
			return 0, false, ErrCorruptTree
		}
	}
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
	return t.AscendRangeSelected(root, from, to, nil, fn)
}

// AscendRangeSelected applies selectKey to each key in the range before
// resolving its value. The predicate may only borrow the key until it returns.
func (t *BTree) AscendRangeSelected(root uint64, from, to []byte, selectKey func([]byte) (bool, error), fn func(k, v []byte) bool) error {
	if root == 0 {
		return nil
	}
	pid, err := t.seekLeaf(root, from, false)
	if err != nil {
		return err
	}
	for pid != 0 {
		next, done, err := t.ascendLeaf(pid, from, to, selectKey, fn)
		if err != nil || done {
			return err
		}
		pid, from = next, nil
	}
	return nil
}

// ascendLeaf validates the leaf before calling fn and materializes only
// entries inside the requested range that the callback actually consumes.
func (t *BTree) ascendLeaf(pid uint64, from, to []byte, selectKey func([]byte) (bool, error), fn func(k, v []byte) bool) (next uint64, done bool, err error) {
	page, release, err := t.eng.readPagePooled(pid)
	if err != nil {
		return 0, false, err
	}
	defer release()
	n, err := leafEntryCount(page)
	if err != nil {
		return 0, false, err
	}
	off, start, end := btreeHeaderSize, btreeHeaderSize, btreeHeaderSize
	for range n {
		key, _, err := readLeafEntry(page, &off)
		if err != nil {
			return 0, false, err
		}
		switch {
		case from != nil && bytes.Compare(key, from) < 0:
			start, end = off, off
		case !done && (to == nil || bytes.Compare(key, to) <= 0):
			end = off
		default:
			done = true
		}
	}
	for off := start; off < end; {
		key, value, err := readLeafEntry(page, &off)
		if err != nil {
			return 0, false, err
		}
		if selectKey != nil {
			selected, err := selectKey(key)
			if err != nil {
				return 0, false, err
			}
			if !selected {
				continue
			}
		}
		value, err = t.resolveLeafValue(value)
		if err != nil {
			return 0, false, err
		}
		if !fn(append([]byte(nil), key...), value) {
			return 0, true, nil
		}
	}
	return binary.BigEndian.Uint64(page[8:16]), done, nil
}

// AscendRange calls fn for every key in [from, to] inclusive (byte order). If from is nil, start at the smallest key.
func (t *BTree) AscendRange(from, to []byte, fn func(k, v []byte) bool) error {
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return err
	}
	return t.AscendRangeFromRoot(hdr.BTreeRoot, from, to, fn)
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
	pid, err := t.seekLeaf(root, to, true)
	if err != nil {
		return err
	}
	for pid != 0 {
		parent, done, err := t.descendLeaf(pid, from, to, fn)
		if err != nil || done {
			return err
		}
		pid, err = t.prevLeaf(pid, parent, root)
		if err != nil {
			return err
		}
	}
	return nil
}

// descendLeaf validates the entire serialized leaf, retaining only offsets.
// Callers may retain callback data, so copy only the entries they actually see.
func (t *BTree) descendLeaf(pid uint64, from, to []byte, fn func(k, v []byte) bool) (parent uint64, done bool, err error) {
	page, release, err := t.eng.readPagePooled(pid)
	if err != nil {
		return 0, false, err
	}
	defer release()
	n, err := leafEntryCount(page)
	if err != nil {
		return 0, false, err
	}
	var scratch [128]int
	offsets := scratch[:0]
	off := btreeHeaderSize
	for range n {
		entryOffset := off
		key, _, err := readLeafEntry(page, &off)
		if err != nil {
			return 0, false, err
		}
		if to == nil || bytes.Compare(key, to) <= 0 {
			offsets = append(offsets, entryOffset)
		}
	}
	for _, entryOffset := range slices.Backward(offsets) {
		key, value, err := readLeafEntry(page, &entryOffset)
		if err != nil {
			return 0, false, err
		}
		if from != nil && bytes.Compare(key, from) < 0 {
			return 0, true, nil
		}
		value, err = t.resolveLeafValue(value)
		if err != nil {
			return 0, false, err
		}
		if !fn(append([]byte(nil), key...), value) {
			return 0, true, nil
		}
	}
	return binary.BigEndian.Uint64(page[16:24]), false, nil
}

// resolveLeafValue materializes an owned value from its serialized leaf entry.
func (t *BTree) resolveLeafValue(value []byte) ([]byte, error) {
	if isOverflowStub(value) {
		logicalLength, firstPage := decodeOverflowStub(value)
		data, err := t.readOverflowChain(firstPage, logicalLength)
		if err != nil {
			return nil, err
		}
		if isCompressedValue(data) {
			return t.eng.decompressValue(data)
		}
		return data, nil
	}
	if isCompressedValue(value) {
		return t.eng.decompressValue(value)
	}
	return append([]byte(nil), value...), nil
}

// DescendRange calls fn for every key in [from, to] inclusive in descending
// (reverse) byte order. If to is nil, it starts at the largest key.
func (t *BTree) DescendRange(from, to []byte, fn func(k, v []byte) bool) error {
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return err
	}
	return t.DescendRangeFromRoot(hdr.BTreeRoot, from, to, fn)
}

// seekLeaf finds the leaf for key. With no key, select the leftmost or
// rightmost leaf according to rightmost.
func (t *BTree) seekLeaf(root uint64, key []byte, rightmost bool) (uint64, error) {
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
		pid, err = lookupInternalChildForSeek(page, key, rightmost)
		release()
		if err != nil {
			return 0, err
		}
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
			return t.seekLeaf(in.ptrs[i-1], nil, true)
		}
	}
	return 0, nil
}
