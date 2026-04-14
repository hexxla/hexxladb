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

// PutCell implements [domain.Storage].
func (s *Storage) PutCell(ctx context.Context, rec record.CellRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.DB.Update(func(tx *hxdb.Tx) error {
		return tx.PutCell(rec)
	})
}

// GetCell implements [domain.Storage].
func (s *Storage) GetCell(ctx context.Context, key lattice.PackedCoord) (record.CellRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return record.CellRecord{}, false, err
	}
	var out record.CellRecord
	var ok bool
	err := s.DB.View(func(tx *hxdb.Tx) error {
		var inner error
		out, ok, inner = tx.GetCell(key)
		return inner
	})
	return out, ok, err
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
	var out []record.SeamRecord
	err := s.DB.View(func(tx *hxdb.Tx) error {
		var inner error
		out, inner = tx.FindSeams(ctx, center, radius, unresolvedOnly)
		return inner
	})
	return out, err
}

// LoadContext implements [domain.Storage].
func (s *Storage) LoadContext(ctx context.Context, center lattice.Coord, maxR, maxCells int) ([]record.CellRecord, error) {
	var out []record.CellRecord
	err := s.DB.View(func(tx *hxdb.Tx) error {
		var inner error
		out, inner = tx.LoadContext(ctx, center, maxR, maxCells)
		return inner
	})
	return out, err
}

// LoadContextAt implements [domain.Storage].
func (s *Storage) LoadContextAt(ctx context.Context, center lattice.Coord, maxR, maxCells int, asOf time.Time) ([]record.CellRecord, error) {
	var out []record.CellRecord
	err := s.DB.View(func(tx *hxdb.Tx) error {
		var inner error
		out, inner = tx.LoadContextAt(ctx, center, maxR, maxCells, asOf)
		return inner
	})
	return out, err
}

// WalkRingFacets implements [domain.Storage].
func (s *Storage) WalkRingFacets(ctx context.Context, center lattice.Coord, ring int, facetMask uint8, asOf *time.Time, fn func(lattice.Coord, record.CellRecord, []record.FacetRecord) bool) error {
	return s.DB.View(func(tx *hxdb.Tx) error {
		return tx.WalkRingFacets(ctx, center, ring, facetMask, asOf, fn)
	})
}

// ResolveSeam implements [domain.Storage].
func (s *Storage) ResolveSeam(ctx context.Context, id, resolutionStatus, resolutionNote string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.DB.Update(func(tx *hxdb.Tx) error {
		return tx.ResolveSeam(id, resolutionStatus, resolutionNote)
	})
}

// PutSeam implements [domain.Storage].
func (s *Storage) PutSeam(ctx context.Context, rec record.SeamRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.DB.Update(func(tx *hxdb.Tx) error {
		return tx.PutSeam(rec)
	})
}

// MarkConflict implements [domain.Storage].
func (s *Storage) MarkConflict(ctx context.Context, cellA, cellB lattice.Coord, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.DB.Update(func(tx *hxdb.Tx) error {
		return tx.MarkConflict(cellA, cellB, reason)
	})
}

// PutFacet implements [domain.Storage].
func (s *Storage) PutFacet(ctx context.Context, rec record.FacetRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.DB.Update(func(tx *hxdb.Tx) error {
		return tx.PutFacet(rec)
	})
}

// UpdateFacet implements [domain.Storage].
func (s *Storage) UpdateFacet(ctx context.Context, rec record.FacetRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.DB.Update(func(tx *hxdb.Tx) error {
		return tx.UpdateFacet(rec)
	})
}

// GetFacet implements [domain.Storage].
func (s *Storage) GetFacet(ctx context.Context, key lattice.PackedCoord, facetID byte) (record.FacetRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return record.FacetRecord{}, false, err
	}
	var out record.FacetRecord
	var ok bool
	err := s.DB.View(func(tx *hxdb.Tx) error {
		var inner error
		out, ok, inner = tx.GetFacet(key, facetID)
		return inner
	})
	return out, ok, err
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
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.DB.Update(func(tx *hxdb.Tx) error {
		return tx.PutEdge(rec)
	})
}

// LinkCells implements [domain.Storage].
func (s *Storage) LinkCells(ctx context.Context, from, to lattice.Coord, relationType string, weight float64, prov record.ProvenanceWire) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.DB.Update(func(tx *hxdb.Tx) error {
		return tx.LinkCells(from, to, relationType, weight, prov)
	})
}

// GetEdge implements [domain.Storage].
func (s *Storage) GetEdge(ctx context.Context, from, to lattice.PackedCoord, relationType string) (record.EdgeRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return record.EdgeRecord{}, false, err
	}
	var out record.EdgeRecord
	var ok bool
	err := s.DB.View(func(tx *hxdb.Tx) error {
		var inner error
		out, ok, inner = tx.GetEdge(from, to, relationType)
		return inner
	})
	return out, ok, err
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
