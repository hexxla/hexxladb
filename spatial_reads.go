package hexxladb

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// WalkRing visits each coordinate on the hex ring at distance ring from center
// (same order as [Ring]). For each cell, fn receives raw bytes or ok=false if
// no cell record exists. Stops early if fn returns false. ctx is checked between cells.
func (tx *Tx) WalkRing(ctx context.Context, center Coord, ring int, fn func(Coord, []byte, bool) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if ring < 0 || ring > MaxSpatialScanRadius || fn == nil {
		return fmt.Errorf("%w: ring must be between 0 and %d and callback must be non-nil", ErrInvalidArgument, MaxSpatialScanRadius)
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if err := validatePackedRadius(center, ring); err != nil {
		return err
	}
	for c := range lattice.RingSeq(center, ring) {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, err := lattice.Pack(c)
		if err != nil {
			return err
		}
		raw, seq, ok, err := tx.getCellVisibleRaw(p)
		if err != nil {
			return err
		}
		if seq != 0 && seq == tx.writeSeq {
			raw = bytes.Clone(raw)
		}
		if !fn(c, raw, ok) {
			break
		}
	}
	return nil
}

// WalkRingAt visits the same coordinates as [Tx.WalkRing], but decodes each cell and calls fn
// only when a cell exists and its validity window contains asOf (interpreted in UTC).
// Coordinates with no cell or a cell that fails the validity filter are omitted (fn is not called).
func (tx *Tx) WalkRingAt(ctx context.Context, center Coord, ring int, asOf time.Time, fn func(Coord, CellRecord) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if ring < 0 || ring > MaxSpatialScanRadius || fn == nil {
		return fmt.Errorf("%w: ring must be between 0 and %d and callback must be non-nil", ErrInvalidArgument, MaxSpatialScanRadius)
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if err := validatePackedRadius(center, ring); err != nil {
		return err
	}
	asOfNano := asOf.UTC().UnixNano()
	for c := range lattice.RingSeq(center, ring) {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, err := lattice.Pack(c)
		if err != nil {
			return err
		}
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok || !record.ValidAt(rec.Validity, asOfNano) {
			continue
		}
		if !fn(c, rec) {
			break
		}
	}
	return nil
}

// ScanContextRaw walks concentric rings from center and collects up to maxCells
// existing cell records. Raw primitive; prefer [Tx.LoadContext] for new callers.
func (tx *Tx) ScanContextRaw(ctx context.Context, center Coord, maxR, maxCells int) ([]CellRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if maxCells <= 0 || maxR < 0 || maxR > MaxSpatialScanRadius {
		return nil, ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return nil, ErrDatabaseClosed
	}
	if err := validatePackedRadius(center, maxR); err != nil {
		return nil, err
	}
	out := make([]record.CellRecord, 0, min(maxCells, hexDiskCellCount(maxR)))
	for cp := range lattice.WalkRingsPackedSeq(center, maxR) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) >= maxCells {
			break
		}
		rec, ok, err := tx.GetCell(cp.Packed)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// ScanContextAtRaw is like [Tx.ScanContextRaw] but skips cells whose validity does not contain asOf.
// Raw primitive; prefer [Tx.LoadContext] with AsOf field for new callers.
func (tx *Tx) ScanContextAtRaw(ctx context.Context, center Coord, maxR, maxCells int, asOf time.Time) ([]CellRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if maxCells <= 0 || maxR < 0 || maxR > MaxSpatialScanRadius {
		return nil, ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return nil, ErrDatabaseClosed
	}
	if err := validatePackedRadius(center, maxR); err != nil {
		return nil, err
	}
	asOfNano := asOf.UTC().UnixNano()
	out := make([]record.CellRecord, 0, min(maxCells, hexDiskCellCount(maxR)))
	for cp := range lattice.WalkRingsPackedSeq(center, maxR) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) >= maxCells {
			break
		}
		rec, ok, err := tx.GetCell(cp.Packed)
		if err != nil {
			return nil, err
		}
		if !ok || !record.ValidAt(rec.Validity, asOfNano) {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// WalkRingFacets visits the same ring coordinates as [Tx.WalkRing]. For each cell that exists and
// passes the optional asOf validity filter (when asOf is non-nil), it loads facet records whose
// facet_id bits are set in facetMask (bits 0..5 correspond to facet_id 0..5). Bits outside 0x3f
// are rejected with [ErrInvalidArgument]. Cost is O(ring_cells × popcount(facetMask)) btree lookups
// in the typical case ([Tx.GetFacet] per set bit). Facets are returned in ascending facet_id order;
// missing facet keys are omitted from the slice.
func (tx *Tx) WalkRingFacets(ctx context.Context, center Coord, ring int, facetMask uint8, asOf *time.Time, fn func(Coord, CellRecord, []FacetWalkRecord) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if ring < 0 || ring > MaxSpatialScanRadius || fn == nil {
		return fmt.Errorf("%w: ring must be between 0 and %d and callback must be non-nil", ErrInvalidArgument, MaxSpatialScanRadius)
	}
	if facetMask&^0x3f != 0 {
		return ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if err := validatePackedRadius(center, ring); err != nil {
		return err
	}
	mask := facetMask & 0x3f
	for c := range lattice.RingSeq(center, ring) {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, err := lattice.Pack(c)
		if err != nil {
			return err
		}
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if asOf != nil && !record.ValidAt(rec.Validity, asOf.UTC().UnixNano()) {
			continue
		}
		facets, err := tx.collectFacetsForMask(p, mask)
		if err != nil {
			return err
		}
		if !fn(c, rec, facets) {
			break
		}
	}
	return nil
}

// collectFacetsForMask collects facet records for all bits set in mask.
func (tx *Tx) collectFacetsForMask(p lattice.PackedCoord, mask uint8) ([]record.FacetRecord, error) {
	var facets []record.FacetRecord
	for id := range byte(index.MaxFacetID + 1) {
		if mask&(1<<id) == 0 {
			continue
		}
		fr, have, err := tx.GetFacet(p, id)
		if err != nil {
			return nil, err
		}
		if have {
			facets = append(facets, fr)
		}
	}
	return facets, nil
}
