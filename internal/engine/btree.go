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
				return ld.vals[i], true, nil
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
func (t *BTree) Put(key, val []byte) error {
	if len(key) > maxKeyBytes {
		return ErrKeyTooLarge
	}
	if uint32(len(val)) > t.eng.maxValueBytes { //nolint:gosec // G115: len() is always non-negative; conversion is safe
		return ErrValueTooLarge
	}
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return err
	}
	if hdr.BTreeRoot == 0 {
		return t.putFirst(key, val)
	}
	split, nr, sep, err := t.insertAt(hdr.BTreeRoot, key, val)
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
	page, err := buildInternalPage(0, ptrs, keys)
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
	page, err := buildLeafPage(0, 0, [][]byte{append([]byte(nil), key...)}, [][]byte{append([]byte(nil), val...)})
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
		vals[idx] = append([]byte(nil), val...)
		pg, err := buildLeafPage(ld.parent, ld.next, keys, vals)
		if err != nil {
			return false, 0, nil, err
		}
		if err := t.eng.WritePage(pid, pg); err != nil {
			return false, 0, nil, err
		}
		return false, 0, nil, nil
	}
	keys = append(keys[:idx], append([][]byte{append([]byte(nil), key...)}, keys[idx:]...)...)
	vals = append(vals[:idx], append([][]byte{append([]byte(nil), val...)}, vals[idx:]...)...)
	if len(keys) <= maxLeafEntries {
		pg, err := buildLeafPage(ld.parent, ld.next, keys, vals)
		if err != nil {
			return false, 0, nil, err
		}
		if err := t.eng.WritePage(pid, pg); err != nil {
			return false, 0, nil, err
		}
		return false, 0, nil, nil
	}
	mid := len(keys) / 2
	leftK, rightK := keys[:mid], keys[mid:]
	leftV, rightV := vals[:mid], vals[mid:]
	sepKey := append([]byte(nil), rightK[0]...)
	rid, err := t.allocPageID()
	if err != nil {
		return false, 0, nil, err
	}
	leftPg, err := buildLeafPage(ld.parent, rid, leftK, leftV)
	if err != nil {
		return false, 0, nil, err
	}
	rightPg, err := buildLeafPage(ld.parent, ld.next, rightK, rightV)
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
	if len(newKeys) <= maxInternalChildren-1 {
		pg, err := buildInternalPage(in.parent, newPtrs, newKeys)
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
	lp, err := buildInternalPage(parent, leftPtrs, leftKeys)
	if err != nil {
		return false, 0, nil, err
	}
	rp, err := buildInternalPage(parent, rightPtrs, rightKeys)
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
			if !fn(k, ld.vals[i]) {
				return nil
			}
		}
		pid = ld.next
	}
	return nil
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
