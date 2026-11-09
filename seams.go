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
	if err := invalidTypedRecord(validateSeamInvariants(rec)); err != nil {
		return err
	}
	pk, err := index.SeamKey(rec.ID)
	if err != nil {
		return err
	}
	if err := tx.validateAndPrepareSeamOverwrite(rec); err != nil {
		return err
	}
	data, err := record.EncodeSeam(rec)
	if err != nil {
		return err
	}
	writeKey, err := index.SeamKeyWithVersion(rec.ID, tx.writeSeq)
	if err != nil {
		return err
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
	if err := tx.putSeamSecondaryIndex(rec, tx.writeSeq); err != nil {
		return err
	}
	tx.noteChangelog(clogOp, pk, data)
	if h := tx.db.afterPutSeam; h != nil {
		return h.AfterPutSeam(ctx, rec)
	}
	return nil
}

// validateAndPrepareSeamOverwrite checks endpoint consistency for an existing seam and
// removes secondary indexes for an overwritten physical version.
func (tx *Tx) validateAndPrepareSeamOverwrite(rec record.SeamRecord) error {
	old, seq, ok, err := tx.visibleSeamAndSeq(rec.ID)
	if err != nil || !ok {
		return err
	}
	if err := checkSeamEndpoints(old, rec); err != nil {
		return err
	}
	if seq == tx.writeSeq {
		return tx.removeSeamSecondaryIndex(old, seq)
	}
	return nil
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
// canonically ordered packed endpoints, SeamType "mark_conflict", and DetectedAt set to now.
// Only allowed inside [DB.Update].
func (tx *Tx) MarkConflict(cellA, cellB Coord, reason string) error {
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
func (tx *Tx) MarkSupersedes(superseder, superseded Coord, reason string) error {
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
func (tx *Tx) FindSeams(ctx context.Context, center Coord, radius int, unresolvedOnly bool) ([]SeamRecord, error) {
	return tx.findSeams(ctx, center, radius, unresolvedOnly, nil)
}

// FindSeamsAt is like [Tx.FindSeams] but only includes seams whose [ValidityWire]
// contains asOf (single-version read filter; not MVCC).
func (tx *Tx) FindSeamsAt(ctx context.Context, center Coord, radius int, unresolvedOnly bool, asOf time.Time) ([]SeamRecord, error) {
	return tx.findSeams(ctx, center, radius, unresolvedOnly, &asOf)
}

func (tx *Tx) findSeams(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool, asOf *time.Time) ([]record.SeamRecord, error) {
	return tx.findSeamsWithBudget(ctx, center, radius, unresolvedOnly, asOf, seamSearchBudget{})
}

func (tx *Tx) findSeamsWithBudget(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool, asOf *time.Time, budget seamSearchBudget) ([]record.SeamRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if radius < 0 || radius > MaxSeamSearchRadius {
		return nil, fmt.Errorf("%w: seam radius must be between 0 and %d", ErrInvalidArgument, MaxSeamSearchRadius)
	}
	if tx.db.activeEng() == nil {
		return nil, ErrDatabaseClosed
	}
	if err := validatePackedRadius(center, radius); err != nil {
		return nil, err
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
	for c := range lattice.WalkRingsSeq(center, radius) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if budget.charge != nil {
			if err := budget.charge(); err != nil {
				return nil, err
			}
		}
		if err := tx.scanSeamsForCoord(ctx, c, center, radius, unresolvedOnly, asOf, &out, seen, &budget); err != nil {
			return nil, err
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
func (tx *Tx) scanSeamsForCoord(ctx context.Context, c, center lattice.Coord, radius int, unresolvedOnly bool, asOf *time.Time, out *[]record.SeamRecord, seen map[string]struct{}, budget *seamSearchBudget) error {
	p, err := lattice.Pack(c)
	if err != nil {
		return err
	}
	from, to := index.SeamByCellsRangeLoFixed(p)
	if err := tx.scanSeamRange(ctx, from, to, center, radius, unresolvedOnly, asOf, out, seen, budget); err != nil {
		return err
	}
	from2, to2, ok := index.SeamByCellsRangeHiFixedLoLess(p)
	if !ok {
		return nil
	}
	return tx.scanSeamRange(ctx, from2, to2, center, radius, unresolvedOnly, asOf, out, seen, budget)
}

// scanSeamRange performs a single AscendRange over a seam-by-cells key range,
// collecting matching seams into out.
func (tx *Tx) scanSeamRange(ctx context.Context, from, to []byte, center lattice.Coord, radius int, unresolvedOnly bool, asOf *time.Time, out *[]record.SeamRecord, seen map[string]struct{}, budget *seamSearchBudget) error {
	var scanErr error
	if err := tx.db.btree.AscendRange(from, to, func(k, _ []byte) bool {
		if ctxErr := ctx.Err(); ctxErr != nil {
			scanErr = ctxErr
			return false
		}
		if err := budget.addIndexRow(); err != nil {
			scanErr = err
			return false
		}
		_, _, id, parseErr := index.ParseSeamByCellsKey(k)
		if parseErr != nil {
			scanErr = parseErr
			return false
		}
		if collectErr := tx.collectSeamFind(out, seen, id, center, radius, unresolvedOnly, asOf, budget); collectErr != nil {
			scanErr = collectErr
			return false
		}
		return true
	}); err != nil {
		return err
	}
	return scanErr
}

func (tx *Tx) collectSeamFind(out *[]record.SeamRecord, seen map[string]struct{}, id string, center lattice.Coord, radius int, unresolvedOnly bool, asOf *time.Time, budget *seamSearchBudget) error {
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
	if err := budget.addResult(); err != nil {
		return err
	}
	*out = append(*out, rec)
	return nil
}

type seamSearchBudget struct {
	indexRows int
	results   int
	charge    func() error
}

func (b *seamSearchBudget) addIndexRow() error {
	b.indexRows++
	if b.indexRows > MaxSeamIndexRows {
		return fmt.Errorf("%w: seam index rows exceed %d", ErrSpatialScanLimit, MaxSeamIndexRows)
	}
	if b.charge != nil {
		return b.charge()
	}
	return nil
}

func (b *seamSearchBudget) addResult() error {
	b.results++
	if b.results > MaxSeamSearchResults {
		return fmt.Errorf("%w: seam results exceed %d", ErrSpatialScanLimit, MaxSeamSearchResults)
	}
	return nil
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
