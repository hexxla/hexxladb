package hexxladb

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/hexxla/hexxladb/internal/changelog"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// PutFacet writes a facet record at facet/<packed>/<facet_id>. Only allowed inside [DB.Update].
func (tx *Tx) PutFacet(rec FacetWalkRecord) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	if err := invalidTypedRecord(validateFacetInvariants(rec)); err != nil {
		return err
	}
	data, err := record.EncodeFacet(rec)
	if err != nil {
		return err
	}
	var key []byte
	if tx.db.useMVCC {
		key, err = index.FacetKeyWithVersion(rec.Key, rec.FacetID, tx.writeSeq)
	} else {
		key, err = index.FacetKey(rec.Key, rec.FacetID)
	}
	if err != nil {
		return err
	}
	if err := tx.putDirect(key, data); err != nil {
		return err
	}
	tx.noteChangelog(changelog.OpPutFacet, key, data)
	return nil
}

// UpdateFacet writes a facet only if rec.DerivationHash equals SHA-256 of the cell's current RawContent
// (see docs/hexxladb/HEXXLA.md Facet lifecycle). Use [Tx.PutFacet] to write without this check.
// Only allowed inside [DB.Update].
func (tx *Tx) UpdateFacet(rec FacetWalkRecord) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	cell, ok, err := tx.GetCell(rec.Key)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCellNotFound
	}
	want := record.HashRawContent([]byte(cell.RawContent))
	if rec.DerivationHash != want {
		return ErrFacetDerivationMismatch
	}
	return tx.PutFacet(rec)
}

// GetFacet returns the facet at (key, facetID), or (zero, false, nil) if missing.
func (tx *Tx) GetFacet(key PackedCoord, facetID byte) (FacetWalkRecord, bool, error) {
	if tx == nil || tx.db == nil {
		return record.FacetRecord{}, false, ErrClosed
	}
	if facetID > index.MaxFacetID {
		return record.FacetRecord{}, false, ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return record.FacetRecord{}, false, ErrDatabaseClosed
	}
	raw, ok, err := tx.getFacetVisibleRaw(key, facetID)
	if err != nil || !ok {
		return record.FacetRecord{}, ok, err
	}
	rec, err := record.DecodeFacet(raw)
	if err != nil {
		return record.FacetRecord{}, false, err
	}
	return rec, true, nil
}

// AscendFacetsForCell visits every facet for the given cell (facet_id 0..5) in key order.
// Stops early if fn returns false.
func (tx *Tx) AscendFacetsForCell(key PackedCoord, fn func(FacetWalkRecord) bool) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if !tx.db.useMVCC {
		lo, err := index.FacetRangeLower(key)
		if err != nil {
			return err
		}
		hi, err := index.FacetRangeUpper(key)
		if err != nil {
			return err
		}
		return tx.AscendRange(lo, hi, func(_, v []byte) bool {
			rec, err := record.DecodeFacet(v)
			if err != nil {
				return false
			}
			return fn(rec)
		})
	}
	for facetID := byte(0); facetID <= index.MaxFacetID; facetID++ {
		raw, ok, err := tx.getFacetVisibleRaw(key, facetID)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		rec, err := record.DecodeFacet(raw)
		if err != nil {
			return err
		}
		if !fn(rec) {
			break
		}
	}
	return nil
}

// PutEdge writes an edge at edge/<from>/<to>/<relation_type>. Only allowed inside [DB.Update].
func (tx *Tx) PutEdge(rec EdgeWalkRecord) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	if rec.RelationType == "" {
		return ErrInvalidArgument
	}
	if err := invalidTypedRecord(validateEdgeInvariants(rec)); err != nil {
		return err
	}
	data, err := record.EncodeEdge(rec)
	if err != nil {
		return err
	}
	key, err := index.EdgeKey(rec.From, rec.To, rec.RelationType)
	if err != nil {
		if errors.Is(err, index.ErrEmptyRelationType) {
			return ErrInvalidArgument
		}
		return err
	}
	if err := tx.putDirect(key, data); err != nil {
		return err
	}
	tx.noteChangelog(changelog.OpPutEdge, key, data)
	return nil
}

// GetEdge returns the edge for (from, to, relationType), or (zero, false, nil) if missing.
func (tx *Tx) GetEdge(from, to PackedCoord, relationType string) (EdgeWalkRecord, bool, error) {
	if tx == nil || tx.db == nil {
		return record.EdgeRecord{}, false, ErrClosed
	}
	if relationType == "" {
		return record.EdgeRecord{}, false, ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return record.EdgeRecord{}, false, ErrDatabaseClosed
	}
	k, err := index.EdgeKey(from, to, relationType)
	if err != nil {
		if errors.Is(err, index.ErrEmptyRelationType) {
			return record.EdgeRecord{}, false, ErrInvalidArgument
		}
		return record.EdgeRecord{}, false, err
	}
	raw, ok, err := tx.Get(k)
	if err != nil || !ok {
		return record.EdgeRecord{}, ok, err
	}
	rec, err := record.DecodeEdge(raw)
	if err != nil {
		return record.EdgeRecord{}, false, err
	}
	return rec, true, nil
}

// AscendEdgesFrom visits every edge whose from-cell matches from (prefix scan in key order).
// It stops when fn returns false or when keys leave the matching from-cell prefix.
func (tx *Tx) AscendEdgesFrom(from PackedCoord, fn func(EdgeWalkRecord) bool) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	prefix := index.EdgeFromPrefix(from)
	var decodeErr error
	err := tx.AscendRange(prefix, nil, func(k, v []byte) bool {
		if !bytes.HasPrefix(k, prefix) {
			return false
		}
		rec, err := record.DecodeEdge(v)
		if err != nil {
			decodeErr = err
			return false
		}
		return fn(rec)
	})
	if err != nil {
		return err
	}
	return decodeErr
}

// LinkCells is the spec-shaped helper for link_cells: packs endpoints and [Tx.PutEdge].
// relationType must be non-empty. Only allowed inside [DB.Update].
func (tx *Tx) LinkCells(from, to Coord, relationType string, weight float64, prov ProvenanceWire) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	if relationType == "" {
		return ErrInvalidArgument
	}
	pf, err := lattice.Pack(from)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	pt, err := lattice.Pack(to)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	return tx.PutEdge(record.EdgeRecord{
		From:         pf,
		To:           pt,
		RelationType: relationType,
		Weight:       weight,
		Provenance:   prov,
	})
}
