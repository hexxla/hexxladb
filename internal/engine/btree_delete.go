package engine

import (
	"bytes"
	"fmt"
	"slices"
)

// minLeafKeysForPage returns the minimum key count for a non-root leaf at the given page size.
// This is approximately half the max entries that could fit on a page.
func minLeafKeysForPage(pageSize int) int {
	return int((maxLeafEntriesForPage(pageSize) + 1) / 2)
}

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
	page, release, err := t.eng.readPagePooled(leafPID)
	if err != nil {
		return err
	}
	defer release()
	ld, err := parseLeafPage(page)
	if err != nil {
		return err
	}
	if err := t.releaseDeletedOverflow(ld.vals[keyIdx]); err != nil {
		return err
	}
	ld.keys = append(ld.keys[:keyIdx], ld.keys[keyIdx+1:]...)
	ld.vals = append(ld.vals[:keyIdx], ld.vals[keyIdx+1:]...)
	pg, err := buildLeafPage(t.pageSize(), ld.parent, ld.next, ld.keys, ld.vals)
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
	// Deleting the leftmost key in a leaf increases the min key in that leaf's subtree; every
	// internal separator that equals the old first key (keys[i-1] for ptrs[i]) must be updated
	// up the spine while this node remains the leftmost under each ancestor.
	if len(ld.keys) > 0 && keyIdx == 0 {
		if err := t.cascadeNewMinToAncestors(parentPIDs, childIdxs, ld.keys[0], hdr.BTreeRoot); err != nil {
			return err
		}
	}
	if len(ld.keys) == 0 {
		if hdr.BTreeRoot == leafPID {
			if err := t.eng.UpdateHeader(func(h *Header) { h.BTreeRoot = 0 }); err != nil {
				return err
			}
			return t.eng.releasePageID(leafPID)
		}
		newRoot, err := t.removeChildFromParentChain(parentPIDs, childIdxs, hdr.BTreeRoot)
		if err != nil {
			return err
		}
		if newRoot != hdr.BTreeRoot {
			if err := t.eng.UpdateHeader(func(h *Header) { h.BTreeRoot = newRoot }); err != nil {
				return err
			}
		}
		return t.eng.releasePageID(leafPID)
	}
	if hdr.BTreeRoot == leafPID || len(ld.keys) >= minLeafKeysForPage(t.pageSize()) {
		return nil
	}
	newRoot, err := t.rebalanceLeaf(parentPIDs, childIdxs, leafPID, ld, hdr.BTreeRoot)
	if err != nil {
		return fmt.Errorf("rebalance: %w", err)
	}
	if newRoot != hdr.BTreeRoot {
		return t.eng.UpdateHeader(func(h *Header) { h.BTreeRoot = newRoot })
	}
	return nil
}

func (t *BTree) releaseDeletedOverflow(value []byte) error {
	if !isOverflowStub(value) {
		return nil
	}
	_, firstPage := decodeOverflowStub(value)
	return t.freeOverflowChain(firstPage)
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

func (t *BTree) removeChildFromParentChain(parentPIDs []uint64, childIdxs []int, root uint64) (uint64, error) {
	if len(parentPIDs) == 0 {
		return root, nil
	}
	parentPID := parentPIDs[len(parentPIDs)-1]
	idx := childIdxs[len(childIdxs)-1]
	return t.removeInternalChild(parentPID, idx, root)
}

func (t *BTree) removeInternalChild(parentPID uint64, childIdx int, root uint64) (uint64, error) {
	page, release, err := t.eng.readPagePooled(parentPID)
	if err != nil {
		return root, err
	}
	in, err := parseInternalPage(page)
	release()
	if err != nil {
		return root, err
	}
	newPtrs, newKeys, err := removePtrAndKey(in.ptrs, in.keys, childIdx)
	if err != nil {
		return root, fmt.Errorf("remove child %d from parent %d (nptrs=%d): %w", childIdx, parentPID, len(in.ptrs), err)
	}
	hdr, err := t.eng.ReadHeader()
	if err != nil {
		return root, err
	}
	// Root collapse: when parentPID is the B+ tree root and it is left with a single remaining
	// child, that child becomes the new root (depth decreases by 1). If the root goes empty, the
	// whole tree is empty.
	if parentPID == hdr.BTreeRoot {
		if len(newPtrs) == 0 {
			if err := t.eng.releasePageID(parentPID); err != nil {
				return root, err
			}
			return 0, nil
		}
		if len(newPtrs) == 1 {
			if err := t.setParent(newPtrs[0], 0); err != nil {
				return root, err
			}
			if err := t.eng.releasePageID(parentPID); err != nil {
				return root, err
			}
			return newPtrs[0], nil
		}
		ipg, err := buildInternalPage(t.pageSize(), in.parent, newPtrs, newKeys)
		if err != nil {
			return root, err
		}
		if err := t.eng.WritePage(parentPID, ipg); err != nil {
			return root, err
		}
		return hdr.BTreeRoot, nil
	}
	// Non-root internal that lost its last child: remove it from its grandparent (cascades
	// subtree-empty notifications up the spine).
	if len(newPtrs) == 0 {
		gp, gidx, found, err := t.findParentOfPage(hdr.BTreeRoot, parentPID)
		if err != nil {
			return root, err
		}
		if !found {
			return root, fmt.Errorf("%w: parent of internal %d not found", ErrCorruptTree, parentPID)
		}
		newRoot, err := t.removeInternalChild(gp, gidx, root)
		if err != nil {
			return root, err
		}
		if err := t.eng.releasePageID(parentPID); err != nil {
			return root, err
		}
		return newRoot, nil
	}
	// Non-root internal with ≥1 remaining child: keep it in place (even with 1 ptr / 0 keys).
	// Splicing it into its grandparent (the previous behavior) would place one grandparent slot
	// directly at a leaf while sibling slots still point to internal nodes, breaking the uniform
	// "all leaves at the same depth" B+ invariant. Leaf rebalance then reads parent.ptrs[i+1]
	// expecting a leaf and trips "not leaf" corruption (see TestIntegration_MVCC_sustainedPutCellSameKey).
	// The cost is a thin internal node (1 ptr); subsequent inserts fill it naturally.
	ipg, err := buildInternalPage(t.pageSize(), in.parent, newPtrs, newKeys)
	if err != nil {
		return root, err
	}
	if err := t.eng.WritePage(parentPID, ipg); err != nil {
		return root, err
	}
	return hdr.BTreeRoot, nil
}

// cascadeNewMinToAncestors updates on-disk separators on the path from a leaf to the root when
// the first key in that leaf's subtree (under ptrs[ci] at each level) was removed and a new min
// applies. Only ancestors where this subtree is a non-leftmost child (ci > 0) carry a key for it
// (in.keys[ci-1] is the min of ptrs[ci]).
func (t *BTree) cascadeNewMinToAncestors(parentPIDs []uint64, childIdxs []int, newMin []byte, root uint64) error {
	for l := range slices.Backward(parentPIDs) {
		ci := childIdxs[l]
		if ci > 0 {
			_, err := t.updateSeparator(parentPIDs[l], ci-1, newMin, root)
			if err != nil {
				return fmt.Errorf("cascade parent %d at depth %d: %w", parentPIDs[l], l, err)
			}
			return nil
		}
	}
	return nil
}

// findParentOfPage returns the internal page that points to target among its child ptrs, and the
// child index, when target is a direct child. Used when collapsing a non-root internal to one child.
func (t *BTree) findParentOfPage(root, target uint64) (parentID uint64, childIdx int, found bool, err error) {
	if root == target {
		return 0, 0, false, fmt.Errorf("%w: root is target", ErrCorruptTree)
	}
	page, release, err := t.eng.readPagePooled(root)
	if err != nil {
		return 0, 0, false, err
	}
	defer release()
	if page[5] == btreeKindLeaf {
		return 0, 0, false, nil
	}
	in, err := parseInternalPage(page)
	if err != nil {
		return 0, 0, false, err
	}
	for i, p := range in.ptrs {
		if p == target {
			return root, i, true, nil
		}
	}
	for _, p := range in.ptrs {
		pp, ii, ok, err := t.findParentOfPage(p, target)
		if err != nil {
			return 0, 0, false, err
		}
		if ok {
			return pp, ii, true, nil
		}
	}
	return 0, 0, false, nil
}

// childPtrIndexInParent returns the index in parent.ptrs for childPtr (a direct child page id).
func (t *BTree) childPtrIndexInParent(parentPID, childPtr uint64) (int, error) {
	page, release, err := t.eng.readPagePooled(parentPID)
	if err != nil {
		return 0, err
	}
	defer release()
	if page[5] == btreeKindLeaf {
		return 0, fmt.Errorf("%w: parent is leaf", ErrCorruptTree)
	}
	in, err := parseInternalPage(page)
	if err != nil {
		return 0, err
	}
	for i, p := range in.ptrs {
		if p == childPtr {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%w: ptr %d not a child of parent %d", ErrCorruptTree, childPtr, parentPID)
}

// rightLeafInParent is the B+ parent ordering (canonical) for the right neighbor leaf.
// The on-disk next pointer in the page can diverge; rebalance/merge use this. If hasRight is
// false, the leaf is the rightmost under parent (ok to fall back to a left-sibling path).
func (t *BTree) rightLeafInParent(parentPID, leafPID uint64) (rightPID uint64, inLeft, rightIdx int, hasRight bool, err error) {
	page, release, err := t.eng.readPagePooled(parentPID)
	if err != nil {
		return 0, 0, 0, false, err
	}
	defer release()
	if page[5] == btreeKindLeaf {
		return 0, 0, 0, false, fmt.Errorf("%w: parent is leaf", ErrCorruptTree)
	}
	in, err := parseInternalPage(page)
	if err != nil {
		return 0, 0, 0, false, err
	}
	li := -1
	for i, p := range in.ptrs {
		if p == leafPID {
			li = i
			break
		}
	}
	if li < 0 {
		return 0, 0, 0, false, fmt.Errorf("%w: leaf %d not a child of parent %d", ErrCorruptTree, leafPID, parentPID)
	}
	if li+1 >= len(in.ptrs) {
		return 0, li, 0, false, nil
	}
	return in.ptrs[li+1], li, li + 1, true, nil
}

// pickLeafParentForSiblingWalk returns the internal page that directly owns leafPID: prefer the
// parent at the end of the findLeaf path; if that does not list the leaf, use ld.parent. This
// handles cases where on-disk parent was stale or paths diverged after internal splice.
func (t *BTree) pickLeafParentForSiblingWalk(parentPIDs []uint64, leafPID uint64, ld *leafData) (uint64, error) {
	if len(parentPIDs) > 0 {
		pp := parentPIDs[len(parentPIDs)-1]
		if _, err := t.childPtrIndexInParent(pp, leafPID); err == nil {
			return pp, nil
		}
	}
	if ld.parent == 0 {
		return 0, fmt.Errorf("%w: no direct parent for leaf %d in rebalance", ErrCorruptTree, leafPID)
	}
	if _, err := t.childPtrIndexInParent(ld.parent, leafPID); err != nil {
		return 0, fmt.Errorf("%w: on-disk parent %d of leaf %d: %w", ErrCorruptTree, ld.parent, leafPID, err)
	}
	return ld.parent, nil
}

func (t *BTree) rebalanceLeaf(parentPIDs []uint64, childIdxs []int, leafPID uint64, ld *leafData, root uint64) (uint64, error) {
	// Right neighbor: parent's ptr order is canonical. Prefer path parent, then on-disk parent.
	if newRoot, done, err := t.tryRightSiblingRebalance(parentPIDs, childIdxs, leafPID, ld, root); err != nil {
		return root, err
	} else if done {
		return newRoot, nil
	}
	// Left neighbor fallback.
	return t.tryLeftSiblingRebalance(parentPIDs, childIdxs, leafPID, ld, root)
}

// tryRightSiblingRebalance attempts to merge or borrow from the right sibling.
// Returns (newRoot, true, nil) if rebalancing was performed, (root, false, nil) if
// no right sibling was usable, or (root, false, err) on failure.
func (t *BTree) tryRightSiblingRebalance(parentPIDs []uint64, childIdxs []int, leafPID uint64, ld *leafData, root uint64) (newRoot uint64, done bool, err error) {
	parentPID, perr := t.pickLeafParentForSiblingWalk(parentPIDs, leafPID, ld)
	if perr != nil {
		return root, false, nil
	}
	rightPID, _, _, hasRight, err := t.rightLeafInParent(parentPID, leafPID)
	if err != nil {
		return root, false, fmt.Errorf("right sibling: %w", err)
	}
	if !hasRight {
		return root, false, nil
	}
	rightPID, rd, err := t.resolveRightLeaf(rightPID, leafPID, ld)
	if err != nil {
		return root, false, err
	}
	if leafSerializedSize(append(ld.keys, rd.keys...), append(ld.vals, rd.vals...)) <= t.pageSize() {
		newRoot, err := t.mergeRightLeaf(parentPIDs, leafPID, ld, rd, rightPID, root)
		return newRoot, true, err
	}
	if len(rd.keys) > minLeafKeysForPage(t.pageSize()) {
		newRoot, err := t.borrowFromRight(parentPIDs, childIdxs, leafPID, ld, rightPID, rd, root)
		return newRoot, true, err
	}
	// Neither merge nor borrow possible; leave slightly underfull (valid B+ state).
	return root, false, nil
}

// resolveRightLeaf reads the right sibling page. In rare cases the parent's i+1 pointer
// can disagree with the leaf's next link; when that happens, fall back to ld.next.
func (t *BTree) resolveRightLeaf(rightPID, leafPID uint64, ld *leafData) (resolvedPID uint64, rd *leafData, err error) {
	rp, releaseRP, err := t.eng.readPagePooled(rightPID)
	if err != nil {
		return 0, nil, err
	}
	rightKind := rp[5]
	var parseErr error
	rd, parseErr = parseLeafPage(rp)
	releaseRP()
	if parseErr == nil && rightKind == btreeKindLeaf {
		return rightPID, rd, nil
	}
	// Parent pointer disagrees — try ld.next as fallback.
	if ld.next == 0 || ld.next == rightPID {
		if parseErr != nil {
			return 0, nil, fmt.Errorf("parse right sibling at %d: %w", rightPID, parseErr)
		}
		return 0, nil, fmt.Errorf("%w: right ptr %d in parent of %d is not a leaf (kind %d)", ErrCorruptTree, rightPID, leafPID, rightKind)
	}
	rp2, rel2, err := t.eng.readPagePooled(ld.next)
	if err != nil {
		return 0, nil, err
	}
	if rp2[5] != btreeKindLeaf {
		rel2()
		return 0, nil, fmt.Errorf("%w: right ptr %d and next %d are not leaves (parent of leaf %d)", ErrCorruptTree, rightPID, ld.next, leafPID)
	}
	rd, err = parseLeafPage(rp2)
	rel2()
	if err != nil {
		return 0, nil, err
	}
	return ld.next, rd, nil
}

// tryLeftSiblingRebalance attempts to merge or borrow from the left sibling.
func (t *BTree) tryLeftSiblingRebalance(parentPIDs []uint64, childIdxs []int, leafPID uint64, ld *leafData, root uint64) (uint64, error) {
	if len(parentPIDs) == 0 {
		return root, nil
	}
	parentPID := parentPIDs[len(parentPIDs)-1]
	idx := childIdxs[len(childIdxs)-1]
	if idx == 0 {
		return root, nil
	}
	page, releaseP, err := t.eng.readPagePooled(parentPID)
	if err != nil {
		return root, err
	}
	in, err := parseInternalPage(page)
	releaseP()
	if err != nil {
		return root, err
	}
	leftPID := in.ptrs[idx-1]
	leftPage, releaseLP, err := t.eng.readPagePooled(leftPID)
	if err != nil {
		return root, err
	}
	leftd, err := parseLeafPage(leftPage)
	releaseLP()
	if err != nil {
		return root, err
	}
	if leftd.next != leafPID {
		return root, nil
	}
	if leafSerializedSize(append(leftd.keys, ld.keys...), append(leftd.vals, ld.vals...)) <= t.pageSize() {
		return t.mergeRightLeaf(parentPIDs, leftPID, leftd, ld, leafPID, root)
	}
	if len(leftd.keys) > minLeafKeysForPage(t.pageSize()) {
		return t.borrowFromLeft(parentPIDs, childIdxs, leftPID, leftd, leafPID, ld, root)
	}
	// Neither merge nor borrow possible; leave slightly underfull (valid B+ state).
	return root, nil
}

// mergeRightLeaf concatenates the right leaf into the left, drops the right page, and removes
// the right's pointer from the direct parent of the *right* leaf (which may differ from the
// left's parent when the B+ next chain crosses internal boundaries at the same tree level).
func (t *BTree) mergeRightLeaf(_ []uint64, leftPID uint64, ld, rd *leafData, rightPageID, root uint64) (uint64, error) {
	remParent := rd.parent
	ri, err := t.childPtrIndexInParent(remParent, rightPageID)
	if err != nil {
		return root, fmt.Errorf("mergeRightLeaf: right child %d under parent %d: %w", rightPageID, remParent, err)
	}
	ld.keys = append(ld.keys, rd.keys...)
	ld.vals = append(ld.vals, rd.vals...)
	ld.next = rd.next
	pg, err := buildLeafPage(t.pageSize(), ld.parent, ld.next, ld.keys, ld.vals)
	if err != nil {
		return root, err
	}
	if err := t.eng.WritePage(leftPID, pg); err != nil {
		return root, err
	}
	newR, err := t.removeInternalChild(remParent, ri, root)
	if err != nil {
		return root, fmt.Errorf("mergeRightLeaf: %w", err)
	}
	if err := t.eng.releasePageID(rightPageID); err != nil {
		return root, err
	}
	return newR, nil
}

func (t *BTree) borrowFromRight(parentPIDs []uint64, childIdxs []int, leftPID uint64, ld *leafData, rightPID uint64, rd *leafData, root uint64) (uint64, error) {
	k := append([]byte(nil), rd.keys[0]...)
	v := append([]byte(nil), rd.vals[0]...)
	rd.keys = rd.keys[1:]
	rd.vals = rd.vals[1:]
	ld.keys = append(ld.keys, k)
	ld.vals = append(ld.vals, v)
	lpg, err := buildLeafPage(t.pageSize(), ld.parent, rightPID, ld.keys, ld.vals)
	if err != nil {
		return root, err
	}
	rpg, err := buildLeafPage(t.pageSize(), rd.parent, rd.next, rd.keys, rd.vals)
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
	lpg, err := buildLeafPage(t.pageSize(), ld.parent, rightPID, ld.keys, ld.vals)
	if err != nil {
		return root, err
	}
	rpg, err := buildLeafPage(t.pageSize(), rd.parent, rd.next, rd.keys, rd.vals)
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

func (t *BTree) updateSeparator(parentPID uint64, keyIdx int, newSep []byte, root uint64) (uint64, error) {
	if keyIdx < 0 {
		return root, nil
	}
	page, release, err := t.eng.readPagePooled(parentPID)
	if err != nil {
		return root, err
	}
	defer release()
	in, err := parseInternalPage(page)
	if err != nil {
		return root, err
	}
	if keyIdx >= len(in.keys) {
		return root, nil
	}
	in.keys[keyIdx] = append([]byte(nil), newSep...)
	ipg, err := buildInternalPage(t.pageSize(), in.parent, in.ptrs, in.keys)
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
