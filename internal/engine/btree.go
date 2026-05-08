package engine

import (
	"bytes"
	"encoding/binary"
	"fmt"
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
// Strategy: advance i as long as the left half (including entry i) remains
// strictly within pageSize. Stop when adding entry i would overflow the left
// page. That entry and all remaining entries form the right half, which must
// fit because Put enforces inlineThreshold < pageSize for any single value.
//
// Invariants:
//   - i is clamped to [minKeysPerPage, len(keys)-minKeysPerPage] so each side
//     has at least minKeysPerPage entries.
//   - If no safe split exists (e.g. every entry is individually close to
//     pageSize), we fall back to len(keys)/2 — the caller's buildLeafPage will
//     return an error, which is the correct behaviour for corrupt input.
func leafSplitIndex(keys, vals [][]byte, pageSize int) int {
	n := len(keys)
	sz := btreeHeaderSize
	for i := range n {
		entry := 4 + len(keys[i]) + len(vals[i])
		if sz+entry > pageSize && i >= minKeysPerPage && i <= n-minKeysPerPage {
			return i
		}
		sz += entry
	}
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
	split, nr, sep, err := t.insertAt(hdr.BTreeRoot, key, leafVal)
	if err != nil {
		return err
	}
	if !split {
		return nil
	}
	left := hdr.BTreeRoot
	newRoot, err := t.allocPageID()
	if err != nil {
		return err
	}
	ptrs := []uint64{left, nr}
	keys := [][]byte{append([]byte(nil), sep...)}
	page, err := buildInternalPage(t.pageSize(), 0, ptrs, keys)
	if err != nil {
		return err
	}
	if err := t.eng.WritePage(newRoot, page); err != nil {
		return err
	}
	if err := t.setParent(left, newRoot); err != nil {
		return err
	}
	if err := t.setParent(nr, newRoot); err != nil {
		return err
	}
	return t.eng.UpdateHeader(func(h *Header) {
		h.BTreeRoot = newRoot
	})
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

func (t *BTree) insertAt(pid uint64, key, val []byte) (split bool, newRight uint64, sep []byte, err error) {
	page, release, err := t.eng.readPagePooled(pid)
	if err != nil {
		return false, 0, nil, err
	}
	defer release()
	switch page[5] {
	case btreeKindLeaf:
		return t.insertIntoLeaf(pid, page, key, val)
	case btreeKindInternal:
		return t.insertIntoInternal(pid, page, key, val)
	default:
		return false, 0, nil, ErrCorruptTree
	}
}

func (t *BTree) insertIntoLeaf(pid uint64, page, key, val []byte) (split bool, newRight uint64, sep []byte, err error) {
	ld, err := parseLeafPage(page)
	if err != nil {
		return false, 0, nil, err
	}
	keys := append([][]byte(nil), ld.keys...)
	vals := append([][]byte(nil), ld.vals...)
	idx := leafKeyIndex(keys, key)
	if idx < len(keys) && bytes.Equal(keys[idx], key) {
		// Free old overflow chain if the previous value was an overflow stub.
		if isOverflowStub(vals[idx]) {
			_, oldFirst := decodeOverflowStub(vals[idx])
			t.freeOverflowChain(oldFirst)
		}
		vals[idx] = append([]byte(nil), val...)
		// The replacement value may be larger than the original, causing the
		// updated page to exceed pageSize. Re-check before writing in-place.
		if leafSerializedSize(keys, vals) <= t.pageSize() {
			pg, err := buildLeafPage(t.pageSize(), ld.parent, ld.next, keys, vals)
			if err != nil {
				return false, 0, nil, err
			}
			if err := t.eng.WritePage(pid, pg); err != nil {
				return false, 0, nil, err
			}
			return false, 0, nil, nil
		}
		// Updated page overflows — fall through to the split path below.
	} else {
		keys = append(keys[:idx], append([][]byte{append([]byte(nil), key...)}, keys[idx:]...)...)
		vals = append(vals[:idx], append([][]byte{append([]byte(nil), val...)}, vals[idx:]...)...)
	}
	if leafSerializedSize(keys, vals) <= t.pageSize() {
		pg, err := buildLeafPage(t.pageSize(), ld.parent, ld.next, keys, vals)
		if err != nil {
			return false, 0, nil, err
		}
		if err := t.eng.WritePage(pid, pg); err != nil {
			return false, 0, nil, err
		}
		return false, 0, nil, nil
	}
	mid := leafSplitIndex(keys, vals, t.pageSize())
	leftK, rightK := keys[:mid], keys[mid:]
	leftV, rightV := vals[:mid], vals[mid:]
	sepKey := append([]byte(nil), rightK[0]...)
	rid, err := t.allocPageID()
	if err != nil {
		return false, 0, nil, err
	}
	leftPg, err := buildLeafPage(t.pageSize(), ld.parent, rid, leftK, leftV)
	if err != nil {
		return false, 0, nil, err
	}
	rightPg, err := buildLeafPage(t.pageSize(), ld.parent, ld.next, rightK, rightV)
	if err != nil {
		return false, 0, nil, err
	}
	if err := t.eng.WritePage(pid, leftPg); err != nil {
		return false, 0, nil, err
	}
	if err := t.eng.WritePage(rid, rightPg); err != nil {
		return false, 0, nil, err
	}
	return true, rid, sepKey, nil
}

func (t *BTree) insertIntoInternal(pid uint64, page, key, val []byte) (split bool, newRight uint64, sep []byte, err error) {
	in, err := parseInternalPage(page)
	if err != nil {
		return false, 0, nil, err
	}
	ci := internalPickChild(in.keys, key)
	child := in.ptrs[ci]
	split, nr, sep, err := t.insertAt(child, key, val)
	if err != nil {
		return false, 0, nil, err
	}
	if !split {
		return false, 0, nil, nil
	}
	newPtrs := make([]uint64, 0, len(in.ptrs)+1)
	newPtrs = append(newPtrs, in.ptrs[:ci]...)
	newPtrs = append(newPtrs, in.ptrs[ci], nr)
	newPtrs = append(newPtrs, in.ptrs[ci+1:]...)
	newKeys := make([][]byte, 0, len(in.keys)+1)
	newKeys = append(newKeys, in.keys[:ci]...)
	newKeys = append(newKeys, sep)
	newKeys = append(newKeys, in.keys[ci:]...)
	if internalSerializedSize(newKeys) <= t.pageSize() {
		pg, err := buildInternalPage(t.pageSize(), in.parent, newPtrs, newKeys)
		if err != nil {
			return false, 0, nil, err
		}
		if err := t.eng.WritePage(pid, pg); err != nil {
			return false, 0, nil, err
		}
		if err := t.setParent(nr, pid); err != nil {
			return false, 0, nil, err
		}
		return false, 0, nil, nil
	}
	return t.splitInternal(pid, in.parent, newPtrs, newKeys)
}

func (t *BTree) splitInternal(pid, parent uint64, ptrs []uint64, keys [][]byte) (split bool, rightID uint64, promote []byte, err error) {
	if len(keys) == 0 || len(ptrs) != len(keys)+1 {
		return false, 0, nil, fmt.Errorf("%w: split internal", ErrCorruptTree)
	}
	mid := len(keys) / 2
	promoted := append([]byte(nil), keys[mid]...)
	leftPtrs := append([]uint64(nil), ptrs[:mid+1]...)
	leftKeys := append([][]byte(nil), keys[:mid]...)
	rightPtrs := append([]uint64(nil), ptrs[mid+1:]...)
	rightKeys := append([][]byte(nil), keys[mid+1:]...)

	rid, err := t.allocPageID()
	if err != nil {
		return false, 0, nil, err
	}
	lp, err := buildInternalPage(t.pageSize(), parent, leftPtrs, leftKeys)
	if err != nil {
		return false, 0, nil, err
	}
	rp, err := buildInternalPage(t.pageSize(), parent, rightPtrs, rightKeys)
	if err != nil {
		return false, 0, nil, err
	}
	if err := t.eng.WritePage(pid, lp); err != nil {
		return false, 0, nil, err
	}
	if err := t.eng.WritePage(rid, rp); err != nil {
		return false, 0, nil, err
	}
	for _, p := range leftPtrs {
		if err := t.setParent(p, pid); err != nil {
			return false, 0, nil, err
		}
	}
	for _, p := range rightPtrs {
		if err := t.setParent(p, rid); err != nil {
			return false, 0, nil, err
		}
	}
	return true, rid, promoted, nil
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
