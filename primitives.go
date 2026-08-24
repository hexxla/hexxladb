package hexxladb

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb/internal/changelog"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// PutCell writes a cell record at cell/<packed> for rec.Key (v1) or version-suffixed keys (MVCC).
// It maintains source/, time/, and tag/ secondary indexes (see [Tx.AscendCellsBySource],
// [Tx.AscendCellsInTimeBucket], [Tx.AscendCellsByTag]).
// Only allowed inside [DB.Update].
func (tx *Tx) PutCell(ctx context.Context, rec record.CellRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.requireWritable(); err != nil {
		return err
	}
	if v := tx.db.cellValidator; v != nil {
		if err := v.ValidateCell(rec); err != nil {
			return err
		}
	}
	data, err := record.EncodeCell(rec)
	if err != nil {
		return err
	}
	if !tx.db.useMVCC {
		old, had, err := tx.GetCell(rec.Key)
		if err != nil {
			return err
		}
		key := index.CellKey(rec.Key)
		if err := tx.putDirect(key, data); err != nil {
			return err
		}
		if had {
			if err := tx.removeCellSecondaryIndex(old, 0); err != nil {
				return err
			}
		}
		if err := tx.putCellSecondaryIndex(rec, 0); err != nil {
			return err
		}
		tx.noteChangelog(changelog.OpPutCell, key, data)
		if h := tx.db.afterPutCell; h != nil {
			return h.AfterPutCell(ctx, rec)
		}
		return nil
	}
	old, oldSeq, had, err := tx.visibleCellAndSeq(rec.Key)
	if err != nil {
		return err
	}
	key := index.CellKeyWithVersion(rec.Key, tx.writeSeq)
	if err := tx.putDirect(key, data); err != nil {
		return err
	}
	// Non-MVCC: replace-in-place secondary keys. MVCC: keep prior commit's source/time
	// keys so snapshot reads ([ViewAt]) still see correct index entries; reclaim via pruning.
	if had && !tx.db.useMVCC {
		if err := tx.removeCellSecondaryIndex(old, oldSeq); err != nil {
			return err
		}
	}
	if err := tx.putCellSecondaryIndex(rec, tx.writeSeq); err != nil {
		return err
	}
	tx.cellOverlay[rec.Key] = rec
	delete(tx.cellDeleted, rec.Key) // clear same-tx delete if re-putting
	tx.noteChangelog(changelog.OpPutCell, index.CellKey(rec.Key), data)
	if h := tx.db.afterPutCell; h != nil {
		return h.AfterPutCell(ctx, rec)
	}
	return nil
}

// GetCell returns the decoded cell visible at the transaction's snapshot, or (zero, false, nil) if missing.
func (tx *Tx) GetCell(key lattice.PackedCoord) (record.CellRecord, bool, error) {
	if tx == nil || tx.db == nil {
		return record.CellRecord{}, false, ErrClosed
	}
	raw, _, ok, err := tx.getCellVisibleRaw(key)
	if err != nil || !ok {
		return record.CellRecord{}, ok, err
	}
	rec, err := record.DecodeCell(raw)
	if err != nil {
		return record.CellRecord{}, false, err
	}
	return rec, true, nil
}

// WalkRing visits each coordinate on the hex ring at distance ring from center
// (same order as [Ring]). For each cell, fn receives raw bytes or ok=false if
// no cell record exists. Stops early if fn returns false. ctx is checked between cells.
func (tx *Tx) WalkRing(ctx context.Context, center lattice.Coord, ring int, fn func(lattice.Coord, []byte, bool) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if ring < 0 {
		return ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	for _, c := range lattice.Ring(center, ring) {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, err := lattice.Pack(c)
		if err != nil {
			return err
		}
		raw, _, ok, err := tx.getCellVisibleRaw(p)
		if err != nil {
			return err
		}
		if !fn(c, raw, ok) {
			break
		}
	}
	return nil
}

// WalkRingAt visits the same coordinates as [Tx.WalkRing], but decodes each cell and calls fn
// only when a cell exists and [record.ValidAt] holds for rec.Validity at asOf (interpreted in UTC).
// Coordinates with no cell or a cell that fails the validity filter are omitted (fn is not called).
func (tx *Tx) WalkRingAt(ctx context.Context, center lattice.Coord, ring int, asOf time.Time, fn func(lattice.Coord, record.CellRecord) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if ring < 0 {
		return ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	asOfNano := asOf.UTC().UnixNano()
	for _, c := range lattice.Ring(center, ring) {
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

// PutSeam writes a seam record at seam/<ulid> and a secondary seam-by-cells/<lo>/<hi>/<ulid>
// index entry (empty value). If a primary already exists for the ULID, CellA/CellB must match
// the stored endpoints or [ErrSeamEndpointMismatch] is returned. Only allowed inside [DB.Update].
//
// The logical changefeed records this as [ChangelogOpPutSeam]. [Tx.ResolveSeam] uses the same
// storage path but logs [ChangelogOpResolveSeam].
func (tx *Tx) PutSeam(ctx context.Context, rec SeamRecord) error {
	return tx.putSeamWithOp(ctx, rec, changelog.OpPutSeam)
}

// putSeamWithOp implements seam primary + secondary writes and MVCC indexing; clogOp is either
// OpPutSeam or OpResolveSeam so consumers can distinguish workflow resolution from other seam writes.
func (tx *Tx) putSeamWithOp(ctx context.Context, rec record.SeamRecord, clogOp byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.requireWritable(); err != nil {
		return err
	}
	pk, err := index.SeamKey(rec.ID)
	if err != nil {
		return err
	}
	if err := tx.validateAndPrepareSeamOverwrite(pk, rec); err != nil {
		return err
	}
	data, err := record.EncodeSeam(rec)
	if err != nil {
		return err
	}
	writeKey := pk
	if tx.db.useMVCC {
		writeKey, err = index.SeamKeyWithVersion(rec.ID, tx.writeSeq)
		if err != nil {
			return err
		}
	}
	if err := tx.putDirect(writeKey, data); err != nil {
		return err
	}
	lo, hi := record.CanonicalCellPair(rec.CellA, rec.CellB)
	sk, err := index.SeamByCellsKey(lo, hi, rec.ID)
	if err != nil {
		return err
	}
	if err := tx.putDirect(sk, nil); err != nil {
		return err
	}
	secSeq := uint64(0)
	if tx.db.useMVCC {
		secSeq = tx.writeSeq
	}
	if err := tx.putSeamSecondaryIndex(rec, secSeq); err != nil {
		return err
	}
	tx.noteChangelog(clogOp, pk, data)
	if h := tx.db.afterPutSeam; h != nil {
		return h.AfterPutSeam(ctx, rec)
	}
	return nil
}

// validateAndPrepareSeamOverwrite checks endpoint consistency for an existing seam and
// removes stale secondary indexes in non-MVCC mode.
func (tx *Tx) validateAndPrepareSeamOverwrite(pk []byte, rec record.SeamRecord) error {
	// MVCC does not remove seam-source/seam-time keys on overwrite: entries are versioned by
	// commit_seq and must remain for [ViewAt] scans; pruning reclaims superseded versions.
	if tx.db.useMVCC {
		old, _, ok, err := tx.visibleSeamAndSeq(rec.ID)
		if err != nil {
			return err
		}
		if ok {
			return checkSeamEndpoints(old, rec)
		}
		return nil
	}
	raw, ok, err := tx.Get(pk)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	old, err := record.DecodeSeam(raw)
	if err != nil {
		return err
	}
	if err := checkSeamEndpoints(old, rec); err != nil {
		return err
	}
	return tx.removeSeamSecondaryIndex(old, 0)
}

// checkSeamEndpoints returns ErrSeamEndpointMismatch if old and updated records disagree on endpoints.
func checkSeamEndpoints(old, updated record.SeamRecord) error {
	elo, ehi := record.CanonicalCellPair(old.CellA, old.CellB)
	nlo, nhi := record.CanonicalCellPair(updated.CellA, updated.CellB)
	if elo != nlo || ehi != nhi {
		return ErrSeamEndpointMismatch
	}
	return nil
}

// SeamType constants for well-known seam relationships.
// SeamType is a free string on [SeamRecord]; these constants define the
// canonical values used by built-in helpers and context assembly.
const (
	// SeamTypeConflict is written by [Tx.MarkConflict].
	SeamTypeConflict = "mark_conflict"
	// SeamTypeSupersedes indicates that CellB is the current truth and CellA is stale.
	// Written by [Tx.MarkSupersedes]. Context assembly (FilterSuperseded) walks these
	// chains and excludes superseded cells from the ContextPack.
	SeamTypeSupersedes = "supersedes"
)

// MarkConflict creates a manual seam between two cells (spec: mark_conflict): new ULID,
// canonical endpoints via [record.CanonicalCellPair], SeamType "mark_conflict", and DetectedAt set to now.
// Only allowed inside [DB.Update].
func (tx *Tx) MarkConflict(cellA, cellB lattice.Coord, reason string) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	pa, err := lattice.Pack(cellA)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	pb, err := lattice.Pack(cellB)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	lo, hi := record.CanonicalCellPair(pa, pb)
	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		return err
	}
	rec := record.SeamRecord{
		ID:               id.String(),
		CellA:            lo,
		CellB:            hi,
		SeamType:         "mark_conflict",
		Reason:           reason,
		ConfidenceDelta:  0,
		DetectedAt:       time.Now().UnixNano(),
		ResolutionStatus: "",
		ResolutionNote:   "",
	}
	return tx.PutSeam(context.Background(), rec)
}

// MarkSupersedes records that superseder is the current truth and superseded is stale.
// The seam uses [SeamTypeSupersedes] with CellA=superseded, CellB=superseder (directional:
// "superseded is superseded by superseder"). Context assembly with [ContextAssemblyConfig.FilterSuperseded]
// walks these chains and excludes stale cells from the [ContextPack].
// Only allowed inside [DB.Update].
func (tx *Tx) MarkSupersedes(superseder, superseded lattice.Coord, reason string) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	pa, err := lattice.Pack(superseded)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	pb, err := lattice.Pack(superseder)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		return err
	}
	rec := record.SeamRecord{
		ID:               id.String(),
		CellA:            pa,
		CellB:            pb,
		SeamType:         SeamTypeSupersedes,
		Reason:           reason,
		ConfidenceDelta:  0,
		DetectedAt:       time.Now().UnixNano(),
		ResolutionStatus: "",
		ResolutionNote:   "",
	}
	return tx.PutSeam(context.Background(), rec)
}

// FindSeams returns seams where at least one endpoint lies within hex distance
// radius of center. If unresolvedOnly is true, only seams with empty
// ResolutionStatus are returned.
//
// The implementation uses the seam-by-cells secondary index: for each cell in the
// ball of radius R around center, range scans list incident seams; results are
// deduplicated by ULID.
func (tx *Tx) FindSeams(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool) ([]SeamRecord, error) {
	return tx.findSeams(ctx, center, radius, unresolvedOnly, nil)
}

// FindSeamsAt is like [Tx.FindSeams] but only includes seams whose [ValidityWire]
// contains asOf (single-version read filter; not MVCC).
func (tx *Tx) FindSeamsAt(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool, asOf time.Time) ([]SeamRecord, error) {
	return tx.findSeams(ctx, center, radius, unresolvedOnly, &asOf)
}

func (tx *Tx) findSeams(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool, asOf *time.Time) ([]record.SeamRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if radius < 0 {
		return nil, ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return nil, ErrDatabaseClosed
	}
	// Pre-flight: if the seam-by-cells index is entirely empty, return immediately.
	// This saves 2×(3r²+3r+1) AscendRange calls (74 at r=3, 182 at r=5) when no
	// seams have ever been written to this database.
	if empty, err := tx.seamIndexEmpty(); err != nil {
		return nil, err
	} else if empty {
		return nil, nil
	}

	var out []record.SeamRecord
	seen := make(map[string]struct{})
	for ring := range radius + 1 {
		for _, c := range lattice.Ring(center, ring) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := tx.scanSeamsForCoord(ctx, c, center, radius, unresolvedOnly, asOf, &out, seen); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// seamIndexEmpty returns true if the seam-by-cells index has no entries.
func (tx *Tx) seamIndexEmpty() (bool, error) {
	var hasAny bool
	if err := tx.db.btree.AscendRange([]byte(index.SeamByCellsPrefix), index.SeamByCellsScanUpperBound(), func(_, _ []byte) bool {
		hasAny = true
		return false
	}); err != nil {
		return false, err
	}
	return !hasAny, nil
}

// scanSeamsForCoord scans both index ranges (Lo-fixed and Hi-fixed-Lo-less) for a single
// coordinate and collects matching seams into out.
func (tx *Tx) scanSeamsForCoord(ctx context.Context, c, center lattice.Coord, radius int, unresolvedOnly bool, asOf *time.Time, out *[]record.SeamRecord, seen map[string]struct{}) error {
	p, err := lattice.Pack(c)
	if err != nil {
		return err
	}
	from, to := index.SeamByCellsRangeLoFixed(p)
	if err := tx.scanSeamRange(ctx, from, to, center, radius, unresolvedOnly, asOf, out, seen); err != nil {
		return err
	}
	from2, to2, ok := index.SeamByCellsRangeHiFixedLoLess(p)
	if !ok {
		return nil
	}
	return tx.scanSeamRange(ctx, from2, to2, center, radius, unresolvedOnly, asOf, out, seen)
}

// scanSeamRange performs a single AscendRange over a seam-by-cells key range,
// collecting matching seams into out.
func (tx *Tx) scanSeamRange(ctx context.Context, from, to []byte, center lattice.Coord, radius int, unresolvedOnly bool, asOf *time.Time, out *[]record.SeamRecord, seen map[string]struct{}) error {
	var scanErr error
	if err := tx.db.btree.AscendRange(from, to, func(k, _ []byte) bool {
		if ctxErr := ctx.Err(); ctxErr != nil {
			scanErr = ctxErr
			return false
		}
		_, _, id, parseErr := index.ParseSeamByCellsKey(k)
		if parseErr != nil {
			scanErr = parseErr
			return false
		}
		if collectErr := tx.collectSeamFind(out, seen, id, center, radius, unresolvedOnly, asOf); collectErr != nil {
			scanErr = collectErr
			return false
		}
		return true
	}); err != nil {
		return err
	}
	return scanErr
}

func (tx *Tx) collectSeamFind(out *[]record.SeamRecord, seen map[string]struct{}, id string, center lattice.Coord, radius int, unresolvedOnly bool, asOf *time.Time) error {
	if _, dup := seen[id]; dup {
		return nil
	}
	raw, _, ok, err := tx.getSeamVisibleRaw(id)
	if err != nil {
		return err
	}
	if !ok {
		seen[id] = struct{}{}
		return nil
	}
	rec, err := record.DecodeSeam(raw)
	if err != nil {
		return err
	}
	if asOf != nil && !record.ValidAt(rec.Validity, asOf.UnixNano()) {
		seen[id] = struct{}{}
		return nil
	}
	if unresolvedOnly && strings.TrimSpace(rec.ResolutionStatus) != "" {
		seen[id] = struct{}{}
		return nil
	}
	ca, err := lattice.Unpack(rec.CellA)
	if err != nil {
		seen[id] = struct{}{}
		return nil
	}
	cb, err := lattice.Unpack(rec.CellB)
	if err != nil {
		seen[id] = struct{}{}
		return nil
	}
	da := center.Distance(ca)
	db := center.Distance(cb)
	if da > radius && db > radius {
		seen[id] = struct{}{}
		return nil
	}
	seen[id] = struct{}{}
	*out = append(*out, rec)
	return nil
}

// ScanContextRaw walks concentric rings from center and collects up to maxCells
// existing cell records. Raw primitive; prefer [Tx.LoadContext] for new callers.
func (tx *Tx) ScanContextRaw(ctx context.Context, center lattice.Coord, maxR, maxCells int) ([]record.CellRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if maxCells <= 0 || maxR < 0 {
		return nil, ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return nil, ErrDatabaseClosed
	}
	var coords []lattice.Coord
	coords = lattice.WalkRings(coords, center, maxR)
	var out []record.CellRecord
	for _, c := range coords {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) >= maxCells {
			break
		}
		p, err := lattice.Pack(c)
		if err != nil {
			return nil, err
		}
		rec, ok, err := tx.GetCell(p)
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
func (tx *Tx) ScanContextAtRaw(ctx context.Context, center lattice.Coord, maxR, maxCells int, asOf time.Time) ([]record.CellRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if maxCells <= 0 || maxR < 0 {
		return nil, ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return nil, ErrDatabaseClosed
	}
	asOfNano := asOf.UTC().UnixNano()
	var coords []lattice.Coord
	coords = lattice.WalkRings(coords, center, maxR)
	var out []record.CellRecord
	for _, c := range coords {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) >= maxCells {
			break
		}
		p, err := lattice.Pack(c)
		if err != nil {
			return nil, err
		}
		rec, ok, err := tx.GetCell(p)
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
func (tx *Tx) WalkRingFacets(ctx context.Context, center lattice.Coord, ring int, facetMask uint8, asOf *time.Time, fn func(lattice.Coord, record.CellRecord, []record.FacetRecord) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if ring < 0 {
		return ErrInvalidArgument
	}
	if facetMask&^0x3f != 0 {
		return ErrInvalidArgument
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	mask := facetMask & 0x3f
	for _, c := range lattice.Ring(center, ring) {
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
	for id := byte(0); id <= index.MaxFacetID; id++ {
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

// ResolveSeam updates resolution fields on the visible seam for id.
// Storage and MVCC indexing match [Tx.PutSeam]; the changefeed records [ChangelogOpResolveSeam].
// Only allowed inside [DB.Update].
func (tx *Tx) ResolveSeam(id, resolutionStatus, resolutionNote string) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	raw, _, ok, err := tx.getSeamVisibleRaw(id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSeamNotFound
	}
	rec, err := record.DecodeSeam(raw)
	if err != nil {
		return err
	}
	rec.ResolutionStatus = resolutionStatus
	rec.ResolutionNote = resolutionNote
	return tx.putSeamWithOp(context.Background(), rec, changelog.OpResolveSeam)
}

func (tx *Tx) requireWritable() error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if !tx.writable {
		return ErrTxReadOnly
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	return nil
}
