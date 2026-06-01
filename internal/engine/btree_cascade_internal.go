// Package engine provides the B+ tree storage engine for HexxlaDB.
// This file handles internal node writes and bbolt-style spill (splitting an
// over-full internal node into multiple fitting pages).
package engine

import "fmt"

// childRef is a (separatorKey, pageID) pair promoted to a parent internal node
// after a child split. sepKey is the minimum key in the subtree rooted at
// pageID, used as the separator immediately before pageID's pointer when the
// parent splices the promoted child in.
type childRef struct {
	sepKey []byte
	pageID uint64
}

// internalGroup is one fitting internal page produced by splitInternalGroups.
type internalGroup struct {
	ptrs []uint64
	keys [][]byte
}

// spillInternal writes an internal node whose (ptrs, keys) may exceed one page.
//
// If the node fits, it is written to pid and no promotions are returned.
// Otherwise it is split (bbolt-style spill) into multiple fitting internal
// pages: the leftmost reuses pid, and each additional page is returned as a
// childRef for the caller to splice into the grandparent. All child pages have
// their parent pointer updated to the page that now owns them.
//
// Invariant: every page written here serializes within pageSize.
func (t *BTree) spillInternal(pid, parent uint64, ptrs []uint64, keys [][]byte) ([]childRef, error) {
	if len(ptrs) != len(keys)+1 {
		return nil, fmt.Errorf("%w: internal ptr/key mismatch (%d ptrs, %d keys)", ErrCorruptTree, len(ptrs), len(keys))
	}
	if internalSerializedSize(keys) <= t.pageSize() {
		if err := t.writeInternal(pid, parent, ptrs, keys); err != nil {
			return nil, err
		}
		return nil, nil
	}

	groups, promoted := splitInternalGroups(ptrs, keys, t.pageSize())
	refs := make([]childRef, 0, len(groups)-1)
	for gi := range groups {
		g := groups[gi]
		gpid := pid
		if gi > 0 {
			id, err := t.allocPageID()
			if err != nil {
				return nil, err
			}
			gpid = id
		}
		// Write leftmost (pid) before allocating siblings so NextPageID advances.
		if err := t.writeInternal(gpid, parent, g.ptrs, g.keys); err != nil {
			return nil, err
		}
		if gi > 0 {
			refs = append(refs, childRef{sepKey: promoted[gi-1], pageID: gpid})
		}
	}
	return refs, nil
}

// writeInternal builds and writes an internal page, then points every child at
// it via the child's parent pointer.
func (t *BTree) writeInternal(pid, parent uint64, ptrs []uint64, keys [][]byte) error {
	pg, err := buildInternalPage(t.pageSize(), parent, ptrs, keys)
	if err != nil {
		return err
	}
	if err := t.eng.WritePage(pid, pg); err != nil {
		return err
	}
	for _, p := range ptrs {
		if err := t.setParent(p, pid); err != nil {
			return err
		}
	}
	return nil
}

// splitInternalGroups partitions an over-full internal node into groups that
// each fit within pageSize, promoting the separating key between consecutive
// groups (B+ tree internal split promotes, not copies). Uses a greedy left-fill
// so every group is guaranteed to fit. len(promoted) == len(groups)-1.
func splitInternalGroups(ptrs []uint64, keys [][]byte, pageSize int) (groups []internalGroup, promoted [][]byte) {
	i := 0
	for i < len(ptrs) {
		g := internalGroup{ptrs: []uint64{ptrs[i]}}
		size := btreeHeaderSize + 8 // header + ptr0
		i++
		for i < len(ptrs) {
			sep := keys[i-1]
			add := 2 + len(sep) + 8 // keyLen(2) + key + ptr(8)
			if size+add > pageSize {
				// Promote the separator between this group and the next.
				promoted = append(promoted, sep)
				break
			}
			g.keys = append(g.keys, sep)
			g.ptrs = append(g.ptrs, ptrs[i])
			size += add
			i++
		}
		groups = append(groups, g)
	}
	return groups, promoted
}
