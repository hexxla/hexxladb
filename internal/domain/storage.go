package domain

import (
	"context"
	"time"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// Storage is the outbound port for cell and seam persistence. Implementations
// live under internal/adapters/out/... and must call only package hexxladb at
// the module root—not internal/engine or internal/index.
type Storage interface {
	PutCell(ctx context.Context, rec record.CellRecord) error
	GetCell(ctx context.Context, key lattice.PackedCoord) (record.CellRecord, bool, error)
	// AscendCellsBySource scans the source/ secondary index for sourceID.
	AscendCellsBySource(ctx context.Context, sourceID string, fn func(record.CellRecord) bool) error
	// AscendCellsInTimeBucket scans the time/ secondary index for a UTC week bucket index.
	AscendCellsInTimeBucket(ctx context.Context, bucket int64, fn func(record.CellRecord) bool) error
	// AscendCellsByTag scans the tag/ secondary index for tag.
	AscendCellsByTag(ctx context.Context, tag string, fn func(record.CellRecord) bool) error
	// AscendDistinctTags invokes fn once per distinct tag string in the tag/ index (keys only).
	AscendDistinctTags(ctx context.Context, fn func(tag string) bool) error
	// ListExistingTopics returns sorted distinct tag strings (same values as cell Tags).
	ListExistingTopics(ctx context.Context) ([]string, error)
	// AscendSeamsBySource scans the seam-source/ secondary index for sourceID.
	AscendSeamsBySource(ctx context.Context, sourceID string, fn func(record.SeamRecord) bool) error
	// AscendSeamsInTimeBucket scans the seam-time/ secondary index for a UTC week bucket.
	AscendSeamsInTimeBucket(ctx context.Context, bucket int64, fn func(record.SeamRecord) bool) error
	// WalkRing visits each cell on the ring at hex distance ring from center
	// (load_context ring order). fn receives raw bytes and ok=false when missing.
	WalkRing(ctx context.Context, center lattice.Coord, ring int, fn func(lattice.Coord, []byte, bool) bool) error
	// WalkRingAt calls fn only for cells whose validity contains asOf (single-version filter; not MVCC).
	WalkRingAt(ctx context.Context, center lattice.Coord, ring int, asOf time.Time, fn func(lattice.Coord, record.CellRecord) bool) error
	FindSeams(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool) ([]record.SeamRecord, error)
	// FindSeamsAt is like FindSeams but only includes seams whose validity contains asOf (single-version filter; not MVCC).
	FindSeamsAt(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool, asOf time.Time) ([]record.SeamRecord, error)
	LoadContext(ctx context.Context, center lattice.Coord, maxR, maxCells int) ([]record.CellRecord, error)
	// LoadContextAt is like LoadContext but skips cells whose validity does not contain asOf.
	LoadContextAt(ctx context.Context, center lattice.Coord, maxR, maxCells int, asOf time.Time) ([]record.CellRecord, error)
	// WalkRingFacets loads facet records (bit i = facet_id i) for each ring cell that passes the optional asOf filter.
	WalkRingFacets(ctx context.Context, center lattice.Coord, ring int, facetMask uint8, asOf *time.Time, fn func(lattice.Coord, record.CellRecord, []record.FacetRecord) bool) error
	ResolveSeam(ctx context.Context, id, resolutionStatus, resolutionNote string) error
	PutSeam(ctx context.Context, rec record.SeamRecord) error
	MarkConflict(ctx context.Context, cellA, cellB lattice.Coord, reason string) error

	PutFacet(ctx context.Context, rec record.FacetRecord) error
	UpdateFacet(ctx context.Context, rec record.FacetRecord) error
	GetFacet(ctx context.Context, key lattice.PackedCoord, facetID byte) (record.FacetRecord, bool, error)
	AscendFacetsForCell(ctx context.Context, key lattice.PackedCoord, fn func(record.FacetRecord) bool) error
	PutEdge(ctx context.Context, rec record.EdgeRecord) error
	LinkCells(ctx context.Context, from, to lattice.Coord, relationType string, weight float64, prov record.ProvenanceWire) error
	GetEdge(ctx context.Context, from, to lattice.PackedCoord, relationType string) (record.EdgeRecord, bool, error)
	AscendEdgesFrom(ctx context.Context, from lattice.PackedCoord, fn func(record.EdgeRecord) bool) error
	// DeleteCell removes a cell and all associated data (secondary indexes, facets, outbound edges).
	// Idempotent: deleting a non-existent cell returns nil. Seams are not removed.
	DeleteCell(ctx context.Context, key lattice.PackedCoord) error
}
