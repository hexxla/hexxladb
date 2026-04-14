package hexxladb

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// PutFacet writes a facet record at facet/<packed>/<facet_id>. Only allowed inside [DB.Update].
func (tx *Tx) PutFacet(rec record.FacetRecord) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	data, err := record.EncodeFacet(rec)
	if err != nil {
		return err
	}
	key, err := index.FacetKey(rec.Key, rec.FacetID)
	if err != nil {
		return err
	}
	return tx.Put(key, data)
}

// UpdateFacet writes a facet only if rec.DerivationHash equals SHA-256 of the cell's current RawContent
// (see docs/hexxladb/HEXXLA.md Facet lifecycle). Use [Tx.PutFacet] to write without this check.
// Only allowed inside [DB.Update].
func (tx *Tx) UpdateFacet(rec record.FacetRecord) error {
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
func (tx *Tx) GetFacet(key lattice.PackedCoord, facetID byte) (record.FacetRecord, bool, error) {
	if tx == nil || tx.db == nil {
		return record.FacetRecord{}, false, ErrClosed
	}
	if facetID > index.MaxFacetID {
		return record.FacetRecord{}, false, ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return record.FacetRecord{}, false, ErrDatabaseClosed
	}
	k, err := index.FacetKey(key, facetID)
	if err != nil {
		return record.FacetRecord{}, false, err
	}
	raw, ok, err := tx.Get(k)
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
func (tx *Tx) AscendFacetsForCell(key lattice.PackedCoord, fn func(record.FacetRecord) bool) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
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

// PutEdge writes an edge at edge/<from>/<to>/<relation_type>. Only allowed inside [DB.Update].
func (tx *Tx) PutEdge(rec record.EdgeRecord) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	if rec.RelationType == "" {
		return ErrInvalidArgument
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
	return tx.Put(key, data)
}

// GetEdge returns the edge for (from, to, relationType), or (zero, false, nil) if missing.
func (tx *Tx) GetEdge(from, to lattice.PackedCoord, relationType string) (record.EdgeRecord, bool, error) {
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
// Stops when fn returns false or when keys leave the from-prefix (see internal/index.EdgeFromPrefix).
func (tx *Tx) AscendEdgesFrom(from lattice.PackedCoord, fn func(record.EdgeRecord) bool) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	prefix := index.EdgeFromPrefix(from)
	return tx.AscendRange(prefix, nil, func(k, v []byte) bool {
		if !bytes.HasPrefix(k, prefix) {
			return false
		}
		rec, err := record.DecodeEdge(v)
		if err != nil {
			return false
		}
		return fn(rec)
	})
}

// LinkCells is the spec-shaped helper for link_cells: packs endpoints and [Tx.PutEdge].
// relationType must be non-empty. Only allowed inside [DB.Update].
func (tx *Tx) LinkCells(from, to lattice.Coord, relationType string, weight float64, prov record.ProvenanceWire) error {
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
