// Package hexxladbout implements [domain.Storage] by calling only the public
// github.com/hexxla/hexxladb API (no internal/engine or internal/index).
package hexxladbout

import (
	"context"
	"time"

	hxdb "github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/domain"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
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
func viewPair[T any](s *Storage, fn func(*hxdb.Tx) (T, error)) (T, error) {
	var out T
	err := s.DB.View(func(tx *hxdb.Tx) error {
		var inner error
		out, inner = fn(tx)
		return inner
	})
	return out, err
}

// viewTriple runs View and captures value, ok flag, and error from fn (typical for Get* APIs).
func viewTriple[T any](s *Storage, fn func(*hxdb.Tx) (T, bool, error)) (out T, ok bool, err error) {
	err = s.DB.View(func(tx *hxdb.Tx) error {
		var inner error
		out, ok, inner = fn(tx)
		return inner
	})
	return out, ok, err
}

// PutCell implements [domain.Storage].
func (s *Storage) PutCell(ctx context.Context, rec record.CellRecord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.PutCell(ctx, rec)
	})
}

// GetCell implements [domain.Storage].
func (s *Storage) GetCell(ctx context.Context, key lattice.PackedCoord) (record.CellRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return record.CellRecord{}, false, err
	}
	return viewTriple(s, func(tx *hxdb.Tx) (record.CellRecord, bool, error) {
		return tx.GetCell(key)
	})
}

// AscendCellsBySource implements [domain.Storage].
func (s *Storage) AscendCellsBySource(ctx context.Context, sourceID string, fn func(record.CellRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.AscendCellsBySource(ctx, sourceID, fn)
	})
}

// AscendCellsInTimeBucket implements [domain.Storage].
func (s *Storage) AscendCellsInTimeBucket(ctx context.Context, bucket int64, fn func(record.CellRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.AscendCellsInTimeBucket(ctx, bucket, fn)
	})
}

// AscendCellsByTag implements [domain.Storage].
func (s *Storage) AscendCellsByTag(ctx context.Context, tag string, fn func(record.CellRecord) bool) error {
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
	return viewPair(s, func(tx *hxdb.Tx) ([]string, error) {
		return tx.ListExistingTopics(ctx)
	})
}

// AscendSeamsBySource implements [domain.Storage].
func (s *Storage) AscendSeamsBySource(ctx context.Context, sourceID string, fn func(record.SeamRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.AscendSeamsBySource(ctx, sourceID, fn)
	})
}

// AscendSeamsInTimeBucket implements [domain.Storage].
func (s *Storage) AscendSeamsInTimeBucket(ctx context.Context, bucket int64, fn func(record.SeamRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.AscendSeamsInTimeBucket(ctx, bucket, fn)
	})
}

// WalkRing implements [domain.Storage].
func (s *Storage) WalkRing(ctx context.Context, center lattice.Coord, ring int, fn func(lattice.Coord, []byte, bool) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.WalkRing(ctx, center, ring, fn)
	})
}

// WalkRingAt implements [domain.Storage].
func (s *Storage) WalkRingAt(ctx context.Context, center lattice.Coord, ring int, asOf time.Time, fn func(lattice.Coord, record.CellRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.WalkRingAt(ctx, center, ring, asOf, fn)
	})
}

// FindSeams implements [domain.Storage].
func (s *Storage) FindSeams(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool) ([]record.SeamRecord, error) {
	return viewPair(s, func(tx *hxdb.Tx) ([]record.SeamRecord, error) {
		return tx.FindSeams(ctx, center, radius, unresolvedOnly)
	})
}

// FindSeamsAt implements [domain.Storage].
func (s *Storage) FindSeamsAt(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool, asOf time.Time) ([]record.SeamRecord, error) {
	return viewPair(s, func(tx *hxdb.Tx) ([]record.SeamRecord, error) {
		return tx.FindSeamsAt(ctx, center, radius, unresolvedOnly, asOf)
	})
}

// LoadContext implements [domain.Storage].
func (s *Storage) LoadContext(ctx context.Context, center lattice.Coord, maxR, maxCells int) ([]record.CellRecord, error) {
	return viewPair(s, func(tx *hxdb.Tx) ([]record.CellRecord, error) {
		return tx.LoadContext(ctx, center, maxR, maxCells)
	})
}

// LoadContextAt implements [domain.Storage].
func (s *Storage) LoadContextAt(ctx context.Context, center lattice.Coord, maxR, maxCells int, asOf time.Time) ([]record.CellRecord, error) {
	return viewPair(s, func(tx *hxdb.Tx) ([]record.CellRecord, error) {
		return tx.LoadContextAt(ctx, center, maxR, maxCells, asOf)
	})
}

// WalkRingFacets implements [domain.Storage].
func (s *Storage) WalkRingFacets(ctx context.Context, center lattice.Coord, ring int, facetMask uint8, asOf *time.Time, fn func(lattice.Coord, record.CellRecord, []record.FacetRecord) bool) error {
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
func (s *Storage) PutSeam(ctx context.Context, rec record.SeamRecord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.PutSeam(ctx, rec)
	})
}

// MarkConflict implements [domain.Storage].
func (s *Storage) MarkConflict(ctx context.Context, cellA, cellB lattice.Coord, reason string) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.MarkConflict(cellA, cellB, reason)
	})
}

// PutFacet implements [domain.Storage].
func (s *Storage) PutFacet(ctx context.Context, rec record.FacetRecord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.PutFacet(rec)
	})
}

// UpdateFacet implements [domain.Storage].
func (s *Storage) UpdateFacet(ctx context.Context, rec record.FacetRecord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.UpdateFacet(rec)
	})
}

// GetFacet implements [domain.Storage].
func (s *Storage) GetFacet(ctx context.Context, key lattice.PackedCoord, facetID byte) (record.FacetRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return record.FacetRecord{}, false, err
	}
	return viewTriple(s, func(tx *hxdb.Tx) (record.FacetRecord, bool, error) {
		return tx.GetFacet(key, facetID)
	})
}

// AscendFacetsForCell implements [domain.Storage].
func (s *Storage) AscendFacetsForCell(ctx context.Context, key lattice.PackedCoord, fn func(record.FacetRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return tx.AscendFacetsForCell(key, func(rec record.FacetRecord) bool {
			if err := ctx.Err(); err != nil {
				return false
			}
			return fn(rec)
		})
	})
}

// PutEdge implements [domain.Storage].
func (s *Storage) PutEdge(ctx context.Context, rec record.EdgeRecord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.PutEdge(rec)
	})
}

// LinkCells implements [domain.Storage].
func (s *Storage) LinkCells(ctx context.Context, from, to lattice.Coord, relationType string, weight float64, prov record.ProvenanceWire) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.LinkCells(from, to, relationType, weight, prov)
	})
}

// GetEdge implements [domain.Storage].
func (s *Storage) GetEdge(ctx context.Context, from, to lattice.PackedCoord, relationType string) (record.EdgeRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return record.EdgeRecord{}, false, err
	}
	return viewTriple(s, func(tx *hxdb.Tx) (record.EdgeRecord, bool, error) {
		return tx.GetEdge(from, to, relationType)
	})
}

// AscendEdgesFrom implements [domain.Storage].
func (s *Storage) AscendEdgesFrom(ctx context.Context, from lattice.PackedCoord, fn func(record.EdgeRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return tx.AscendEdgesFrom(from, func(rec record.EdgeRecord) bool {
			if err := ctx.Err(); err != nil {
				return false
			}
			return fn(rec)
		})
	})
}

// DeleteCell implements [domain.Storage].
func (s *Storage) DeleteCell(ctx context.Context, key lattice.PackedCoord) error {
	return s.withUpdate(ctx, func(tx *hxdb.Tx) error {
		return tx.DeleteCell(ctx, key)
	})
}
