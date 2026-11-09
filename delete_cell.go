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
// On MVCC databases (plaintext format v2 or authenticated format v3) the primary
// cell key is tombstoned (zero-length value) so [DB.ViewAt] snapshots before the
// delete remain consistent. Facets receive the same tombstone treatment.
// [DB.PruneCellVersions] removes stale
// non-latest versions (seq < beforeSeq); it never removes the latest row per coord,
// so a tombstone that is still the head version remains until superseded.
// Outbound edges are hard-deleted in every format (edges are not MVCC-versioned).
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
// visible cell existed and was tombstoned. A second
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
	if err := invalidTypedRecord(validatePackedCoordInvariant("cell key", key)); err != nil {
		return false, err
	}

	return tx.deleteCellMVCC(ctx, key)
}

// deleteCellMVCC tombstones the cell and its facets, removes staged secondary
// entries, and hard-deletes outbound edges.
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

	// A tombstone supersedes committed versions without erasing their indexes.
	if oldSeq == tx.writeSeq {
		if err := tx.removeCellSecondaryIndex(old, oldSeq); err != nil {
			return false, err
		}
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

// tombstoneFacetsForCell writes zero-length tombstones for each visible facet (MVCC path).
func (tx *Tx) tombstoneFacetsForCell(_ context.Context, key lattice.PackedCoord) error {
	for facetID := range byte(index.MaxFacetID + 1) {
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
		keys = append(keys, bytes.Clone(k))
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
