package engine

import "bytes"

// minLeafKeys is the minimum key count for a non-root leaf.
const minLeafKeys = (maxLeafEntries + 1) / 2

// Delete removes key from the B+ tree. Missing keys are ignored.
func (t *BTree) Delete(key []byte) error {
	if len(key) > maxKeyBytes {
		return ErrKeyTooLarge
	}
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return err
	}
	if hdr.BTreeRoot == 0 {
		return nil
	}
	lpath, leafPID, keyIdx, ok, err := t.findLeafWithKey(hdr.BTreeRoot, key)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	parentPIDs, childIdxs := lpath.parentPIDs, lpath.childIdxs
	page, err := t.eng.ReadPage(leafPID)
	if err != nil {
		return err
	}
	ld, err := parseLeafPage(page)
	if err != nil {
		return err
	}
	ld.keys = append(ld.keys[:keyIdx], ld.keys[keyIdx+1:]...)
	ld.vals = append(ld.vals[:keyIdx], ld.vals[keyIdx+1:]...)
	pg, err := buildLeafPage(ld.parent, ld.next, ld.keys, ld.vals)
	if err != nil {
		return err
	}
	if err := t.eng.WritePage(leafPID, pg); err != nil {
		return err
	}
	hdr, err = t.eng.ReadHeader()
	if err != nil {
		return err
	}
	if len(ld.keys) == 0 {
		if hdr.BTreeRoot == leafPID {
			return t.eng.UpdateHeader(func(h *Header) { h.BTreeRoot = 0 })
		}
		newRoot, err := t.removeChildFromParentChain(parentPIDs, childIdxs, hdr.BTreeRoot)
		if err != nil {
			return err
		}
		if newRoot != hdr.BTreeRoot {
			return t.eng.UpdateHeader(func(h *Header) { h.BTreeRoot = newRoot })
		}
		return nil
	}
	if hdr.BTreeRoot == leafPID || len(ld.keys) >= minLeafKeys {
		return nil
	}
	newRoot, err := t.rebalanceLeaf(parentPIDs, childIdxs, leafPID, ld, hdr.BTreeRoot)
	if err != nil {
		return err
	}
	if newRoot != hdr.BTreeRoot {
		return t.eng.UpdateHeader(func(h *Header) { h.BTreeRoot = newRoot })
	}
	return nil
}

// leafPath is the internal-node path to a leaf: parentPIDs[k] chooses child childIdxs[k].
type leafPath struct {
	parentPIDs []uint64
	childIdxs  []int
}

// findLeafWithKey locates the leaf containing key.
func (t *BTree) findLeafWithKey(root uint64, key []byte) (path leafPath, leafPID uint64, keyIdx int, ok bool, err error) {
	pid := root
	for {
		page, err := t.eng.ReadPage(pid)
		if err != nil {
			return leafPath{}, 0, 0, false, err
		}
		if page[5] == btreeKindLeaf {
			ld, err := parseLeafPage(page)
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
		if err != nil {
			return leafPath{}, 0, 0, false, err
		}
		ci := internalPickChild(in.keys, key)
		path.parentPIDs = append(path.parentPIDs, pid)
		path.childIdxs = append(path.childIdxs, ci)
		pid = in.ptrs[ci]
	}
}

func (t *BTree) removeChildFromParentChain(parentPIDs []uint64, childIdxs []int, root uint64) (uint64, error) {
	if len(parentPIDs) == 0 {
		return root, nil
	}
	parentPID := parentPIDs[len(parentPIDs)-1]
	idx := childIdxs[len(childIdxs)-1]
	return t.removeInternalChild(parentPID, idx, root)
}

func (t *BTree) removeInternalChild(parentPID uint64, childIdx int, root uint64) (uint64, error) {
	page, err := t.eng.ReadPage(parentPID)
	if err != nil {
		return root, err
	}
	in, err := parseInternalPage(page)
	if err != nil {
		return root, err
	}
	newPtrs, newKeys, err := removePtrAndKey(in.ptrs, in.keys, childIdx)
	if err != nil {
		return root, err
	}
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return root, err
	}
	if hdr.BTreeRoot == parentPID && len(newPtrs) == 1 {
		return newPtrs[0], nil
	}
	if len(newKeys) == 0 && len(newPtrs) == 1 {
		return newPtrs[0], nil
	}
	ipg, err := buildInternalPage(in.parent, newPtrs, newKeys)
	if err != nil {
		return root, err
	}
	if err := t.eng.WritePage(parentPID, ipg); err != nil {
		return root, err
	}
	return hdr.BTreeRoot, nil
}

func (t *BTree) rebalanceLeaf(parentPIDs []uint64, childIdxs []int, leafPID uint64, ld *leafData, root uint64) (uint64, error) {
	if ld.next != 0 {
		rp, err := t.eng.ReadPage(ld.next)
		if err != nil {
			return root, err
		}
		rd, err := parseLeafPage(rp)
		if err != nil {
			return root, err
		}
		rightIdx := childIdxs[len(childIdxs)-1] + 1
		if len(ld.keys)+len(rd.keys) <= maxLeafEntries {
			return t.mergeRightLeaf(parentPIDs, leafPID, ld, ld.next, rd, rightIdx, root)
		}
		if len(rd.keys) > minLeafKeys {
			return t.borrowFromRight(parentPIDs, childIdxs, leafPID, ld, ld.next, rd, root)
		}
		return t.mergeRightLeaf(parentPIDs, leafPID, ld, ld.next, rd, rightIdx, root)
	}
	if len(parentPIDs) == 0 {
		return root, nil
	}
	parentPID := parentPIDs[len(parentPIDs)-1]
	idx := childIdxs[len(childIdxs)-1]
	if idx == 0 {
		return root, nil
	}
	page, err := t.eng.ReadPage(parentPID)
	if err != nil {
		return root, err
	}
	in, err := parseInternalPage(page)
	if err != nil {
		return root, err
	}
	leftPID := in.ptrs[idx-1]
	leftPage, err := t.eng.ReadPage(leftPID)
	if err != nil {
		return root, err
	}
	leftd, err := parseLeafPage(leftPage)
	if err != nil {
		return root, err
	}
	if leftd.next != leafPID {
		return root, nil
	}
	if len(leftd.keys)+len(ld.keys) <= maxLeafEntries {
		return t.mergeRightLeaf(parentPIDs, leftPID, leftd, leafPID, ld, idx, root)
	}
	if len(leftd.keys) > minLeafKeys {
		return t.borrowFromLeft(parentPIDs, childIdxs, leftPID, leftd, leafPID, ld, root)
	}
	return t.mergeRightLeaf(parentPIDs, leftPID, leftd, leafPID, ld, idx, root)
}

func (t *BTree) mergeRightLeaf(parentPIDs []uint64, leftPID uint64, ld *leafData, _ uint64, rd *leafData, rightIdxInParent int, root uint64) (uint64, error) {
	ld.keys = append(ld.keys, rd.keys...)
	ld.vals = append(ld.vals, rd.vals...)
	ld.next = rd.next
	pg, err := buildLeafPage(ld.parent, ld.next, ld.keys, ld.vals)
	if err != nil {
		return root, err
	}
	if err := t.eng.WritePage(leftPID, pg); err != nil {
		return root, err
	}
	if len(parentPIDs) == 0 {
		return root, nil
	}
	parentPID := parentPIDs[len(parentPIDs)-1]
	return t.removeInternalChild(parentPID, rightIdxInParent, root)
}

func (t *BTree) borrowFromRight(parentPIDs []uint64, childIdxs []int, leftPID uint64, ld *leafData, rightPID uint64, rd *leafData, root uint64) (uint64, error) {
	k := append([]byte(nil), rd.keys[0]...)
	v := append([]byte(nil), rd.vals[0]...)
	rd.keys = rd.keys[1:]
	rd.vals = rd.vals[1:]
	ld.keys = append(ld.keys, k)
	ld.vals = append(ld.vals, v)
	lpg, err := buildLeafPage(ld.parent, rightPID, ld.keys, ld.vals)
	if err != nil {
		return root, err
	}
	rpg, err := buildLeafPage(rd.parent, rd.next, rd.keys, rd.vals)
	if err != nil {
		return root, err
	}
	if err := t.eng.WritePage(leftPID, lpg); err != nil {
		return root, err
	}
	if err := t.eng.WritePage(rightPID, rpg); err != nil {
		return root, err
	}
	if len(parentPIDs) == 0 {
		return root, nil
	}
	parentPID := parentPIDs[len(parentPIDs)-1]
	idx := childIdxs[len(childIdxs)-1]
	return t.updateSeparator(parentPID, idx, rd.keys[0], root)
}

func (t *BTree) borrowFromLeft(parentPIDs []uint64, childIdxs []int, leftPID uint64, ld *leafData, rightPID uint64, rd *leafData, root uint64) (uint64, error) {
	li := len(ld.keys) - 1
	k := append([]byte(nil), ld.keys[li]...)
	v := append([]byte(nil), ld.vals[li]...)
	ld.keys = ld.keys[:li]
	ld.vals = ld.vals[:li]
	rd.keys = append([][]byte{k}, rd.keys...)
	rd.vals = append([][]byte{v}, rd.vals...)
	lpg, err := buildLeafPage(ld.parent, rightPID, ld.keys, ld.vals)
	if err != nil {
		return root, err
	}
	rpg, err := buildLeafPage(rd.parent, rd.next, rd.keys, rd.vals)
	if err != nil {
		return root, err
	}
	if err := t.eng.WritePage(leftPID, lpg); err != nil {
		return root, err
	}
	if err := t.eng.WritePage(rightPID, rpg); err != nil {
		return root, err
	}
	if len(parentPIDs) == 0 {
		return root, nil
	}
	parentPID := parentPIDs[len(parentPIDs)-1]
	idx := childIdxs[len(childIdxs)-1]
	return t.updateSeparator(parentPID, idx-1, rd.keys[0], root)
}

func removePtrAndKey(ptrs []uint64, keys [][]byte, i int) (newPtrs []uint64, newKeys [][]byte, err error) {
	if i < 0 || i >= len(ptrs) {
		return nil, nil, ErrCorruptTree
	}
	newPtrs = make([]uint64, 0, len(ptrs)-1)
	newPtrs = append(newPtrs, ptrs[:i]...)
	newPtrs = append(newPtrs, ptrs[i+1:]...)
	if i == 0 {
		newKeys = append([][]byte(nil), keys[1:]...)
	} else {
		newKeys = make([][]byte, 0, len(keys)-1)
		newKeys = append(newKeys, keys[:i-1]...)
		newKeys = append(newKeys, keys[i:]...)
	}
	return newPtrs, newKeys, nil
}

func (t *BTree) updateSeparator(parentPID uint64, keyIdx int, newSep []byte, root uint64) (uint64, error) {
	if keyIdx < 0 {
		return root, nil
	}
	page, err := t.eng.ReadPage(parentPID)
	if err != nil {
		return root, err
	}
	in, err := parseInternalPage(page)
	if err != nil {
		return root, err
	}
	if keyIdx >= len(in.keys) {
		return root, nil
	}
	in.keys[keyIdx] = append([]byte(nil), newSep...)
	ipg, err := buildInternalPage(in.parent, in.ptrs, in.keys)
	if err != nil {
		return root, err
	}
	if err := t.eng.WritePage(parentPID, ipg); err != nil {
		return root, err
	}
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return root, err
	}
	return hdr.BTreeRoot, nil
}
