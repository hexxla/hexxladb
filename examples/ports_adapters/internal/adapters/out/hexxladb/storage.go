// Package hexxladbout implements [domain.Storage] by calling only the public
// github.com/hexxla/hexxladb API (no internal/engine or internal/index).
package hexxladbout

import (
	"context"
	"time"

	hxdb "github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/examples/ports_adapters/internal/domain"
)

// Storage implements [domain.Storage] over an opened [hxdb.DB].
type Storage struct {
	DB *hxdb.DB
}

var _ domain.Storage = (*Storage)(nil)

// NewStorage returns an adapter backed by db. db must be non-nil for operations.
func NewStorage(db *hxdb.DB) *Storage {
	return &Storage{DB: db}
}

func (s *Storage) withUpdate(ctx context.Context, fn func(*hxdb.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.DB.Update(fn)
}

// viewPair runs View and captures one value plus error from fn (typical for slice returns).
func (s *Storage) viewPair[T any](fn func(*hxdb.Tx) (T, error)) (T, error) {
	var out T
	err := s.DB.View(func(tx *hxdb.Tx) error {
		var inner error
		out, inner = fn(tx)
		return inner
	})
	return out, err
}

// viewTriple runs View and captures value, ok flag, and error from fn (typical for Get* APIs).
func (s *Storage) viewTriple[T any](fn func(*hxdb.Tx) (T, bool, error)) (out T, ok bool, err error) {
	err = s.DB.View(func(tx *hxdb.Tx) error {
		var inner error
		out, ok, inner = fn(tx)
		return inner
	})
	return out, ok, err
}

// PutCell implements [domain.Storage].
func (s *Storage) PutCell(ctx context.Context, rec hxdb.CellRecord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.PutCell(ctx, rec)
	})
}

// GetCell implements [domain.Storage].
func (s *Storage) GetCell(ctx context.Context, key hxdb.PackedCoord) (hxdb.CellRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return hxdb.CellRecord{}, false, err
	}
	return s.viewTriple(func(tx *hxdb.Tx) (hxdb.CellRecord, bool, error) {
		return tx.GetCell(key)
	})
}

// AscendCellsBySource implements [domain.Storage].
func (s *Storage) AscendCellsBySource(ctx context.Context, sourceID string, fn func(hxdb.CellRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.AscendCellsBySource(ctx, sourceID, fn)
	})
}

// AscendCellsInTimeBucket implements [domain.Storage].
func (s *Storage) AscendCellsInTimeBucket(ctx context.Context, bucket int64, fn func(hxdb.CellRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.AscendCellsInTimeBucket(ctx, bucket, fn)
	})
}

// AscendCellsByTag implements [domain.Storage].
func (s *Storage) AscendCellsByTag(ctx context.Context, tag string, fn func(hxdb.CellRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.AscendCellsByTag(ctx, tag, fn)
	})
}

// AscendDistinctTags implements [domain.Storage].
func (s *Storage) AscendDistinctTags(ctx context.Context, fn func(tag string) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.AscendDistinctTags(ctx, fn)
	})
}

// ListExistingTopics implements [domain.Storage].
func (s *Storage) ListExistingTopics(ctx context.Context) ([]string, error) {
	return s.viewPair(func(tx *hxdb.Tx) ([]string, error) {
		return tx.ListExistingTopics(ctx)
	})
}

// AscendSeamsBySource implements [domain.Storage].
func (s *Storage) AscendSeamsBySource(ctx context.Context, sourceID string, fn func(hxdb.SeamRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.AscendSeamsBySource(ctx, sourceID, fn)
	})
}

// AscendSeamsInTimeBucket implements [domain.Storage].
func (s *Storage) AscendSeamsInTimeBucket(ctx context.Context, bucket int64, fn func(hxdb.SeamRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.AscendSeamsInTimeBucket(ctx, bucket, fn)
	})
}

// WalkRing implements [domain.Storage].
func (s *Storage) WalkRing(ctx context.Context, center hxdb.Coord, ring int, fn func(hxdb.Coord, []byte, bool) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.WalkRing(ctx, center, ring, fn)
	})
}

// WalkRingAt implements [domain.Storage].
func (s *Storage) WalkRingAt(ctx context.Context, center hxdb.Coord, ring int, asOf time.Time, fn func(hxdb.Coord, hxdb.CellRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.WalkRingAt(ctx, center, ring, asOf, fn)
	})
}

// FindSeams implements [domain.Storage].
func (s *Storage) FindSeams(ctx context.Context, center hxdb.Coord, radius int, unresolvedOnly bool) ([]hxdb.SeamRecord, error) {
	return s.viewPair(func(tx *hxdb.Tx) ([]hxdb.SeamRecord, error) {
		return tx.FindSeams(ctx, center, radius, unresolvedOnly)
	})
}

// FindSeamsAt implements [domain.Storage].
func (s *Storage) FindSeamsAt(ctx context.Context, center hxdb.Coord, radius int, unresolvedOnly bool, asOf time.Time) ([]hxdb.SeamRecord, error) {
	return s.viewPair(func(tx *hxdb.Tx) ([]hxdb.SeamRecord, error) {
		return tx.FindSeamsAt(ctx, center, radius, unresolvedOnly, asOf)
	})
}

// ScanContextRaw implements [domain.Storage].
func (s *Storage) ScanContextRaw(ctx context.Context, center hxdb.Coord, maxR, maxCells int) ([]hxdb.CellRecord, error) {
	return s.viewPair(func(tx *hxdb.Tx) ([]hxdb.CellRecord, error) {
		return tx.ScanContextRaw(ctx, center, maxR, maxCells)
	})
}

// ScanContextAtRaw implements [domain.Storage].
func (s *Storage) ScanContextAtRaw(ctx context.Context, center hxdb.Coord, maxR, maxCells int, asOf time.Time) ([]hxdb.CellRecord, error) {
	return s.viewPair(func(tx *hxdb.Tx) ([]hxdb.CellRecord, error) {
		return tx.ScanContextAtRaw(ctx, center, maxR, maxCells, asOf)
	})
}

// WalkRingFacets implements [domain.Storage].
func (s *Storage) WalkRingFacets(ctx context.Context, center hxdb.Coord, ring int, facetMask uint8, asOf *time.Time, fn func(hxdb.Coord, hxdb.CellRecord, []hxdb.FacetWalkRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.WalkRingFacets(ctx, center, ring, facetMask, asOf, fn)
	})
}

// ResolveSeam implements [domain.Storage].
func (s *Storage) ResolveSeam(ctx context.Context, id, resolutionStatus, resolutionNote string) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.ResolveSeam(id, resolutionStatus, resolutionNote)
	})
}

// PutSeam implements [domain.Storage].
func (s *Storage) PutSeam(ctx context.Context, rec hxdb.SeamRecord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.PutSeam(ctx, rec)
	})
}

// MarkConflict implements [domain.Storage].
func (s *Storage) MarkConflict(ctx context.Context, cellA, cellB hxdb.Coord, reason string) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.MarkConflict(cellA, cellB, reason)
	})
}

// PutFacet implements [domain.Storage].
func (s *Storage) PutFacet(ctx context.Context, rec hxdb.FacetWalkRecord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.PutFacet(rec)
	})
}

// UpdateFacet implements [domain.Storage].
func (s *Storage) UpdateFacet(ctx context.Context, rec hxdb.FacetWalkRecord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.UpdateFacet(rec)
	})
}

// GetFacet implements [domain.Storage].
func (s *Storage) GetFacet(ctx context.Context, key hxdb.PackedCoord, facetID byte) (hxdb.FacetWalkRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return hxdb.FacetWalkRecord{}, false, err
	}
	return s.viewTriple(func(tx *hxdb.Tx) (hxdb.FacetWalkRecord, bool, error) {
		return tx.GetFacet(key, facetID)
	})
}

// AscendFacetsForCell implements [domain.Storage].
func (s *Storage) AscendFacetsForCell(ctx context.Context, key hxdb.PackedCoord, fn func(hxdb.FacetWalkRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return tx.AscendFacetsForCell(key, func(rec hxdb.FacetWalkRecord) bool {
			if err := ctx.Err(); err != nil {
				return false
			}
			return fn(rec)
		})
	})
}

// PutEdge implements [domain.Storage].
func (s *Storage) PutEdge(ctx context.Context, rec hxdb.EdgeWalkRecord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.PutEdge(rec)
	})
}

// LinkCells implements [domain.Storage].
func (s *Storage) LinkCells(ctx context.Context, from, to hxdb.Coord, relationType string, weight float64, prov hxdb.ProvenanceWire) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.LinkCells(from, to, relationType, weight, prov)
	})
}

// GetEdge implements [domain.Storage].
func (s *Storage) GetEdge(ctx context.Context, from, to hxdb.PackedCoord, relationType string) (hxdb.EdgeWalkRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return hxdb.EdgeWalkRecord{}, false, err
	}
	return s.viewTriple(func(tx *hxdb.Tx) (hxdb.EdgeWalkRecord, bool, error) {
		return tx.GetEdge(from, to, relationType)
	})
}

// AscendEdgesFrom implements [domain.Storage].
func (s *Storage) AscendEdgesFrom(ctx context.Context, from hxdb.PackedCoord, fn func(hxdb.EdgeWalkRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return tx.AscendEdgesFrom(from, func(rec hxdb.EdgeWalkRecord) bool {
			if err := ctx.Err(); err != nil {
				return false
			}
			return fn(rec)
		})
	})
}

// DeleteCell implements [domain.Storage].
func (s *Storage) DeleteCell(ctx context.Context, key hxdb.PackedCoord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.DeleteCell(ctx, key)
	})
}
