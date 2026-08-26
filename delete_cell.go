package hexxladb

import (
	"bytes"
	"context"

	"github.com/hexxla/hexxladb/internal/changelog"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// DeleteCell removes a cell and all associated data atomically: primary key,
// secondary indexes (source/, time/, tag/), facets, and outbound edges.
//
// Idempotent: deleting a non-existent cell returns nil. Use [Tx.DeleteCellWithOutcome]
// when callers need to know whether a visible cell was actually removed.
//
// On format-v2 (MVCC) databases the primary cell key is tombstoned (zero-length
// value) so [DB.ViewAt] snapshots before the delete remain consistent. Facets
// receive the same tombstone treatment. [DB.PruneCellVersions] removes stale
// non-latest versions (seq < beforeSeq); it never removes the latest row per coord,
// so a tombstone that is still the head version remains until superseded. Outbound edges are hard-deleted in both formats
// (edges are not MVCC-versioned).
//
// Seams are NOT removed. Seams reference two cells; removing one endpoint is a
// domain decision. Orphaned seams surface via [DB.HealthCheck].
//
// Only allowed inside [DB.Update].
func (tx *Tx) DeleteCell(ctx context.Context, key PackedCoord) error {
	_, err := tx.DeleteCellWithOutcome(ctx, key)
	return err
}

// DeleteCellWithOutcome behaves like [Tx.DeleteCell] but sets removed=true when a
// visible cell existed and was tombstoned (MVCC) or hard-deleted (v1). A second
// delete on the same coordinate returns removed=false with a nil error.
//
// Only allowed inside [DB.Update].
func (tx *Tx) DeleteCellWithOutcome(ctx context.Context, key PackedCoord) (removed bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := tx.requireWritable(); err != nil {
		return false, err
	}

	if !tx.db.useMVCC {
		return tx.deleteCellV1(key)
	}
	return tx.deleteCellMVCC(ctx, key)
}

// deleteCellV1 hard-deletes the cell, secondary indexes, facets, and outbound edges.
func (tx *Tx) deleteCellV1(key lattice.PackedCoord) (removed bool, err error) {
	old, had, err := tx.GetCell(key)
	if err != nil {
		return false, err
	}
	if !had {
		return false, nil // idempotent
	}
	if err := tx.deleteDirect(index.CellKey(key)); err != nil {
		return false, err
	}
	if err := tx.removeCellSecondaryIndex(old, 0); err != nil {
		return false, err
	}
	if err := tx.deleteFacetsForCell(key); err != nil {
		return false, err
	}
	if err := tx.deleteOutboundEdges(key); err != nil {
		return false, err
	}
	if err := tx.deleteEmbeddingIfEnabled(key); err != nil {
		return false, err
	}
	tx.noteChangelog(changelog.OpDeleteCell, index.CellKey(key), nil)
	return true, nil
}

// deleteCellMVCC writes a tombstone for the cell, removes visible secondary
// indexes, tombstones visible facets, and hard-deletes outbound edges.
func (tx *Tx) deleteCellMVCC(ctx context.Context, key lattice.PackedCoord) (removed bool, err error) {
	old, oldSeq, had, err := tx.visibleCellAndSeq(key)
	if err != nil {
		return false, err
	}
	if !had {
		return false, nil // idempotent
	}

	// Tombstone: zero-length value at the current write sequence.
	tombKey := index.CellKeyWithVersion(key, tx.writeSeq)
	if err := tx.putDirect(tombKey, []byte{}); err != nil {
		return false, err
	}

	// Remove secondary indexes for the version being superseded.
	if err := tx.removeCellSecondaryIndex(old, oldSeq); err != nil {
		return false, err
	}

	// Tombstone visible facets.
	if err := tx.tombstoneFacetsForCell(ctx, key); err != nil {
		return false, err
	}

	// Outbound edges are not MVCC-versioned; hard-delete.
	if err := tx.deleteOutboundEdges(key); err != nil {
		return false, err
	}

	// Cascade: remove embedding (not MVCC-versioned; hard-delete).
	if err := tx.deleteEmbeddingIfEnabled(key); err != nil {
		return false, err
	}

	// Track delete in overlay so same-tx reads see not-found.
	if tx.cellDeleted == nil {
		tx.cellDeleted = make(map[lattice.PackedCoord]bool)
	}
	tx.cellDeleted[key] = true
	delete(tx.cellOverlay, key)

	tx.noteChangelog(changelog.OpDeleteCell, index.CellKey(key), nil)
	return true, nil
}

// deleteFacetsForCell hard-deletes all facet keys for the given cell (v1 path).
func (tx *Tx) deleteFacetsForCell(key lattice.PackedCoord) error {
	lo, err := index.FacetRangeLower(key)
	if err != nil {
		return err
	}
	hi, err := index.FacetRangeUpper(key)
	if err != nil {
		return err
	}
	// Collect keys first to avoid mutation during iteration.
	var keys [][]byte
	if err := tx.AscendRange(lo, hi, func(k, _ []byte) bool {
		keys = append(keys, append([]byte(nil), k...))
		return true
	}); err != nil {
		return err
	}
	for _, k := range keys {
		if err := tx.deleteDirect(k); err != nil {
			return err
		}
	}
	return nil
}

// tombstoneFacetsForCell writes zero-length tombstones for each visible facet (MVCC path).
func (tx *Tx) tombstoneFacetsForCell(_ context.Context, key lattice.PackedCoord) error {
	for facetID := byte(0); facetID <= index.MaxFacetID; facetID++ {
		_, ok, err := tx.getFacetVisibleRaw(key, facetID)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		tombKey, err := index.FacetKeyWithVersion(key, facetID, tx.writeSeq)
		if err != nil {
			return err
		}
		if err := tx.putDirect(tombKey, []byte{}); err != nil {
			return err
		}
	}
	return nil
}

// deleteOutboundEdges removes all edge/<key>/... entries (outbound edges from key).
func (tx *Tx) deleteOutboundEdges(key lattice.PackedCoord) error {
	prefix := index.EdgeFromPrefix(key)
	var keys [][]byte
	if err := tx.AscendRange(prefix, nil, func(k, _ []byte) bool {
		if !bytes.HasPrefix(k, prefix) {
			return false
		}
		keys = append(keys, append([]byte(nil), k...))
		return true
	}); err != nil {
		return err
	}
	for _, k := range keys {
		if err := tx.deleteDirect(k); err != nil {
			return err
		}
	}
	return nil
}
