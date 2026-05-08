package app

import (
	"context"
	"errors"
	"time"

	"github.com/hexxla/hexxladb/internal/domain"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// ErrNoStorage is returned when a use case needs persistence but [Service.Storage] was never wired.
var ErrNoStorage = errors.New("app: storage port not configured")

// Compile-time: Service must satisfy the full [domain.Storage] interface so API drift is caught immediately.
var _ domain.Storage = (*Service)(nil)

// Service orchestrates use cases and outbound ports. Wire concrete adapters in cmd only.
type Service struct {
	// Storage is optional; nil when the process runs without a database (e.g. no HEXXLA_DB_PATH).
	Storage domain.Storage
}

// New constructs the application service with no outbound adapters.
func New() *Service {
	return &Service{}
}

// NewWithStorage constructs the service with persistence wired (hexagonal outbound port).
func NewWithStorage(st domain.Storage) *Service {
	return &Service{Storage: st}
}

// PutCell persists a cell through the outbound [domain.Storage] port (product-layer orchestration entrypoint).
func (s *Service) PutCell(ctx context.Context, rec record.CellRecord) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.PutCell(ctx, rec)
}

// AscendCellsByTag scans cells by tag via [domain.Storage].
func (s *Service) AscendCellsByTag(ctx context.Context, tag string, fn func(record.CellRecord) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.AscendCellsByTag(ctx, tag, fn)
}

// AscendDistinctTags lists distinct tag strings via [domain.Storage].
func (s *Service) AscendDistinctTags(ctx context.Context, fn func(tag string) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.AscendDistinctTags(ctx, fn)
}

// ListExistingTopics returns sorted distinct tags via [domain.Storage].
func (s *Service) ListExistingTopics(ctx context.Context) ([]string, error) {
	if s == nil || s.Storage == nil {
		return nil, ErrNoStorage
	}
	return s.Storage.ListExistingTopics(ctx)
}

// GetCell retrieves a cell by packed coordinate via [domain.Storage].
func (s *Service) GetCell(ctx context.Context, key lattice.PackedCoord) (record.CellRecord, bool, error) {
	if s == nil || s.Storage == nil {
		return record.CellRecord{}, false, ErrNoStorage
	}
	return s.Storage.GetCell(ctx, key)
}

// AscendCellsBySource scans the source/ secondary index via [domain.Storage].
func (s *Service) AscendCellsBySource(ctx context.Context, sourceID string, fn func(record.CellRecord) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.AscendCellsBySource(ctx, sourceID, fn)
}

// AscendCellsInTimeBucket scans the time/ secondary index for a UTC week bucket via [domain.Storage].
func (s *Service) AscendCellsInTimeBucket(ctx context.Context, bucket int64, fn func(record.CellRecord) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.AscendCellsInTimeBucket(ctx, bucket, fn)
}

// WalkRing visits one ring around center via [domain.Storage].
func (s *Service) WalkRing(ctx context.Context, center lattice.Coord, ring int, fn func(lattice.Coord, []byte, bool) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.WalkRing(ctx, center, ring, fn)
}

// WalkRingAt visits one ring with a validity filter via [domain.Storage].
func (s *Service) WalkRingAt(ctx context.Context, center lattice.Coord, ring int, asOf time.Time, fn func(lattice.Coord, record.CellRecord) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.WalkRingAt(ctx, center, ring, asOf, fn)
}

// FindSeams queries seams in a ball around center via [domain.Storage].
func (s *Service) FindSeams(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool) ([]record.SeamRecord, error) {
	if s == nil || s.Storage == nil {
		return nil, ErrNoStorage
	}
	return s.Storage.FindSeams(ctx, center, radius, unresolvedOnly)
}

// FindSeamsAt queries seams with a validity filter via [domain.Storage].
func (s *Service) FindSeamsAt(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool, asOf time.Time) ([]record.SeamRecord, error) {
	if s == nil || s.Storage == nil {
		return nil, ErrNoStorage
	}
	return s.Storage.FindSeamsAt(ctx, center, radius, unresolvedOnly, asOf)
}

// ScanContextRaw assembles a concentric context around center via [domain.Storage].
func (s *Service) ScanContextRaw(ctx context.Context, center lattice.Coord, maxR, maxCells int) ([]record.CellRecord, error) {
	if s == nil || s.Storage == nil {
		return nil, ErrNoStorage
	}
	return s.Storage.ScanContextRaw(ctx, center, maxR, maxCells)
}

// ScanContextAtRaw assembles a context with a validity filter via [domain.Storage].
func (s *Service) ScanContextAtRaw(ctx context.Context, center lattice.Coord, maxR, maxCells int, asOf time.Time) ([]record.CellRecord, error) {
	if s == nil || s.Storage == nil {
		return nil, ErrNoStorage
	}
	return s.Storage.ScanContextAtRaw(ctx, center, maxR, maxCells, asOf)
}

// WalkRingFacets visits facets on a ring via [domain.Storage].
func (s *Service) WalkRingFacets(ctx context.Context, center lattice.Coord, ring int, facetMask uint8, asOf *time.Time, fn func(lattice.Coord, record.CellRecord, []record.FacetRecord) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.WalkRingFacets(ctx, center, ring, facetMask, asOf, fn)
}

// ResolveSeam updates resolution fields on an existing seam via [domain.Storage].
func (s *Service) ResolveSeam(ctx context.Context, id, resolutionStatus, resolutionNote string) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.ResolveSeam(ctx, id, resolutionStatus, resolutionNote)
}

// PutSeam persists a seam record via [domain.Storage].
func (s *Service) PutSeam(ctx context.Context, rec record.SeamRecord) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.PutSeam(ctx, rec)
}

// MarkConflict records a conflict seam between two cells via [domain.Storage].
func (s *Service) MarkConflict(ctx context.Context, cellA, cellB lattice.Coord, reason string) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.MarkConflict(ctx, cellA, cellB, reason)
}

// AscendSeamsBySource scans the seam-source/ index via [domain.Storage].
func (s *Service) AscendSeamsBySource(ctx context.Context, sourceID string, fn func(record.SeamRecord) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.AscendSeamsBySource(ctx, sourceID, fn)
}

// AscendSeamsInTimeBucket scans the seam-time/ index for a UTC week bucket via [domain.Storage].
func (s *Service) AscendSeamsInTimeBucket(ctx context.Context, bucket int64, fn func(record.SeamRecord) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.AscendSeamsInTimeBucket(ctx, bucket, fn)
}

// PutFacet upserts a facet record via [domain.Storage].
func (s *Service) PutFacet(ctx context.Context, rec record.FacetRecord) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.PutFacet(ctx, rec)
}

// UpdateFacet updates a facet record (derivation hash must match) via [domain.Storage].
func (s *Service) UpdateFacet(ctx context.Context, rec record.FacetRecord) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.UpdateFacet(ctx, rec)
}

// GetFacet retrieves a facet by cell key and facet ID via [domain.Storage].
func (s *Service) GetFacet(ctx context.Context, key lattice.PackedCoord, facetID byte) (record.FacetRecord, bool, error) {
	if s == nil || s.Storage == nil {
		return record.FacetRecord{}, false, ErrNoStorage
	}
	return s.Storage.GetFacet(ctx, key, facetID)
}

// AscendFacetsForCell iterates all facets at a cell key via [domain.Storage].
func (s *Service) AscendFacetsForCell(ctx context.Context, key lattice.PackedCoord, fn func(record.FacetRecord) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.AscendFacetsForCell(ctx, key, fn)
}

// PutEdge persists a directed edge via [domain.Storage].
func (s *Service) PutEdge(ctx context.Context, rec record.EdgeRecord) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.PutEdge(ctx, rec)
}

// LinkCells creates a typed, weighted edge between two coordinates via [domain.Storage].
func (s *Service) LinkCells(ctx context.Context, from, to lattice.Coord, relationType string, weight float64, prov record.ProvenanceWire) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.LinkCells(ctx, from, to, relationType, weight, prov)
}

// GetEdge retrieves an edge by endpoints and relation type via [domain.Storage].
func (s *Service) GetEdge(ctx context.Context, from, to lattice.PackedCoord, relationType string) (record.EdgeRecord, bool, error) {
	if s == nil || s.Storage == nil {
		return record.EdgeRecord{}, false, ErrNoStorage
	}
	return s.Storage.GetEdge(ctx, from, to, relationType)
}

// AscendEdgesFrom iterates all out-edges from a packed coordinate via [domain.Storage].
func (s *Service) AscendEdgesFrom(ctx context.Context, from lattice.PackedCoord, fn func(record.EdgeRecord) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.AscendEdgesFrom(ctx, from, fn)
}

// DeleteCell removes a cell and all associated data via [domain.Storage].
func (s *Service) DeleteCell(ctx context.Context, key lattice.PackedCoord) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.DeleteCell(ctx, key)
}
