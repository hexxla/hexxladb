package engine

import (
	"bytes"
	"slices"
)

// Delete removes key from the B+ tree. Missing keys are ignored.
func (t *BTree) Delete(key []byte) error {
	if len(key) > maxKeyBytes {
		return ErrKeyTooLarge
	}
	hdr, err := t.eng.ReadHeader()
	if err != nil || hdr.BTreeRoot == 0 {
		return err
	}
	path, leafID, keyIndex, found, err := t.findLeafWithKey(hdr.BTreeRoot, key)
	if err != nil || !found {
		return err
	}
	leaf, err := t.readLeaf(leafID)
	if err != nil {
		return err
	}
	if err := t.releaseDeletedOverflow(leaf.vals[keyIndex]); err != nil {
		return err
	}
	leaf.keys = slices.Delete(leaf.keys, keyIndex, keyIndex+1)
	leaf.vals = slices.Delete(leaf.vals, keyIndex, keyIndex+1)

	if len(leaf.keys) == 0 {
		return t.deleteEmptyLeaf(path, leafID, leaf.next)
	}
	// Separators are lower fences. Deleting a minimum can leave the old fence
	// in place because keys on the left are still strictly below it.
	if len(path.parentPIDs) > 0 {
		merged, err := t.mergeUnderfullLeaf(path, leafID, leaf)
		if err != nil || merged {
			return err
		}
	}
	return t.writeLeaf(leafID, leaf)
}

func (t *BTree) deleteEmptyLeaf(path leafPath, leafID, nextID uint64) error {
	if len(path.parentPIDs) == 0 {
		if err := t.eng.UpdateHeader(func(h *Header) { h.BTreeRoot = 0 }); err != nil {
			return err
		}
	} else {
		if err := t.unlinkLeaf(path, nextID); err != nil {
			return err
		}
		if err := t.removeInternalChild(path, path.childIdxs[len(path.childIdxs)-1]); err != nil {
			return err
		}
	}
	return t.eng.releasePageID(leafID)
}

// unlinkLeaf finds the predecessor through the tree and bypasses the removed
// page before it can be reused. The predecessor can live under another parent.
func (t *BTree) unlinkLeaf(path leafPath, nextID uint64) error {
	for level := range slices.Backward(path.parentPIDs) {
		child := path.childIdxs[level]
		if child == 0 {
			continue
		}
		parent, err := t.readInternal(path.parentPIDs[level])
		if err != nil {
			return err
		}
		predecessor := parent.ptrs[child-1]
		for range len(path.parentPIDs) - level - 1 {
			node, err := t.readInternal(predecessor)
			if err != nil {
				return err
			}
			predecessor = node.ptrs[len(node.ptrs)-1]
		}
		leaf, err := t.readLeaf(predecessor)
		if err != nil {
			return err
		}
		leaf.next = nextID
		return t.writeLeaf(predecessor, leaf)
	}
	return nil
}

func (t *BTree) readLeaf(id uint64) (*leafData, error) {
	page, release, err := t.eng.readPagePooled(id)
	if err != nil {
		return nil, err
	}
	defer release()
	return parseLeafPage(page)
}

func (t *BTree) readInternal(id uint64) (*internalData, error) {
	page, release, err := t.eng.readPagePooled(id)
	if err != nil {
		return nil, err
	}
	defer release()
	return parseInternalPage(page)
}

func (t *BTree) writeLeaf(id uint64, leaf *leafData) error {
	page, err := buildLeafPage(t.pageSize(), leaf.parent, leaf.next, leaf.keys, leaf.vals)
	if err != nil {
		return err
	}
	return t.eng.WritePage(id, page)
}

func (t *BTree) releaseDeletedOverflow(value []byte) error {
	if !isOverflowStub(value) {
		return nil
	}
	length, firstPage := decodeOverflowStub(value)
	return t.freeOverflowChain(firstPage, length)
}

// leafPath is the internal-node path to a leaf: parentPIDs[k] chooses child childIdxs[k].
type leafPath struct {
	parentPIDs []uint64
	childIdxs  []int
}

// findLeafWithKey locates the leaf containing key.
func (t *BTree) findLeafWithKey(root uint64, key []byte) (path leafPath, leafPID uint64, keyIdx int, ok bool, err error) {
	var walk treeWalk
	pid := root
	for {
		if err := walk.visit(pid); err != nil {
			return leafPath{}, 0, 0, false, err
		}
		page, release, err := t.eng.readPagePooled(pid)
		if err != nil {
			return leafPath{}, 0, 0, false, err
		}
		if page[5] == btreeKindLeaf {
			ld, err := parseLeafPage(page)
			release()
			if err != nil {
				return leafPath{}, 0, 0, false, err
			}
			i := leafKeyIndex(ld.keys, key)
			if i >= len(ld.keys) || !bytes.Equal(ld.keys[i], key) {
				return leafPath{}, 0, 0, false, nil
			}
			return path, pid, i, true, nil
		}
		in, err := parseInternalPage(page)
		release()
		if err != nil {
			return leafPath{}, 0, 0, false, err
		}
		ci := internalPickChild(in.keys, key)
		path.parentPIDs = append(path.parentPIDs, pid)
		path.childIdxs = append(path.childIdxs, ci)
		pid = in.ptrs[ci]
	}
}

// removeInternalChild follows the original descent path when an internal page
// loses its last child. Only collapsing the root can change the leaf depth.
func (t *BTree) removeInternalChild(path leafPath, child int) error {
	level := len(path.parentPIDs) - 1
	id := path.parentPIDs[level]
	node, err := t.readInternal(id)
	if err != nil {
		return err
	}
	ptrs, keys, err := removePtrAndKey(node.ptrs, node.keys, child)
	if err != nil {
		return err
	}
	if level == 0 && len(ptrs) == 1 {
		if err := t.setParent(ptrs[0], 0); err != nil {
			return err
		}
		if err := t.eng.UpdateHeader(func(h *Header) { h.BTreeRoot = ptrs[0] }); err != nil {
			return err
		}
		return t.eng.releasePageID(id)
	}
	if len(ptrs) == 0 {
		if level == 0 {
			err = t.eng.UpdateHeader(func(h *Header) { h.BTreeRoot = 0 })
		} else {
			err = t.removeInternalChild(leafPath{path.parentPIDs[:level], path.childIdxs[:level]}, path.childIdxs[level-1])
		}
		if err != nil {
			return err
		}
		return t.eng.releasePageID(id)
	}
	page, err := buildInternalPage(t.pageSize(), node.parent, ptrs, keys)
	if err != nil {
		return err
	}
	return t.eng.WritePage(id, page)
}

// mergeUnderfullLeaf merges same-parent siblings when their serialized entries
// fit on one page. Sparse leaves remain valid when neither sibling fits.
func (t *BTree) mergeUnderfullLeaf(path leafPath, id uint64, leaf *leafData) (bool, error) {
	level := len(path.parentPIDs) - 1
	parent, err := t.readInternal(path.parentPIDs[level])
	if err != nil {
		return false, err
	}
	child := path.childIdxs[level]
	for _, sibling := range [...]int{child + 1, child - 1} {
		if sibling < 0 || sibling >= len(parent.ptrs) {
			continue
		}
		siblingID := parent.ptrs[sibling]
		other, err := t.readLeaf(siblingID)
		if err != nil {
			return false, err
		}
		if leafSerializedSize(leaf.keys, leaf.vals)+leafSerializedSize(other.keys, other.vals)-btreeHeaderSize > t.pageSize() {
			continue
		}
		leftID, left, rightID, right := id, leaf, siblingID, other
		if sibling < child {
			leftID, left, rightID, right = siblingID, other, id, leaf
		}
		left.keys = append(left.keys, right.keys...)
		left.vals = append(left.vals, right.vals...)
		left.next = right.next
		if err := t.writeLeaf(leftID, left); err != nil {
			return false, err
		}
		if err := t.removeInternalChild(path, max(child, sibling)); err != nil {
			return false, err
		}
		return true, t.eng.releasePageID(rightID)
	}
	return false, nil
}

func removePtrAndKey(ptrs []uint64, keys [][]byte, i int) (newPtrs []uint64, newKeys [][]byte, err error) {
	if i < 0 || i >= len(ptrs) {
		return nil, nil, ErrCorruptTree
	}
	newPtrs = make([]uint64, 0, len(ptrs)-1)
	newPtrs = append(newPtrs, ptrs[:i]...)
	newPtrs = append(newPtrs, ptrs[i+1:]...)
	// Thin internals may hold a single ptr with zero keys; removing that ptr collapses both slices.
	if len(newPtrs) == 0 {
		return newPtrs, nil, nil
	}
	if i == 0 {
		newKeys = append([][]byte(nil), keys[1:]...)
	} else {
		newKeys = make([][]byte, 0, len(keys)-1)
		newKeys = append(newKeys, keys[:i-1]...)
		newKeys = append(newKeys, keys[i:]...)
	}
	return newPtrs, newKeys, nil
}
