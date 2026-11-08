package domain

import (
	"context"
	"time"

	"github.com/hexxla/hexxladb"
)

// Storage is the outbound port for cell and seam persistence. Implementations
// live under internal/adapters/out/... and must call only package hexxladb at
// the module root—not internal/engine or internal/index.
type Storage interface {
	PutCell(ctx context.Context, rec hexxladb.CellRecord) error
	GetCell(ctx context.Context, key hexxladb.PackedCoord) (hexxladb.CellRecord, bool, error)
	// AscendCellsBySource scans the source/ secondary index for sourceID.
	AscendCellsBySource(ctx context.Context, sourceID string, fn func(hexxladb.CellRecord) bool) error
	// AscendCellsInTimeBucket scans the time/ secondary index for a UTC week bucket index.
	AscendCellsInTimeBucket(ctx context.Context, bucket int64, fn func(hexxladb.CellRecord) bool) error
	// AscendCellsByTag scans the tag/ secondary index for tag.
	AscendCellsByTag(ctx context.Context, tag string, fn func(hexxladb.CellRecord) bool) error
	// AscendDistinctTags invokes fn once per distinct tag string in the tag/ index (keys only).
	AscendDistinctTags(ctx context.Context, fn func(tag string) bool) error
	// ListExistingTopics returns sorted distinct tag strings (same values as cell Tags).
	ListExistingTopics(ctx context.Context) ([]string, error)
	// AscendSeamsBySource scans the seam-source/ secondary index for sourceID.
	AscendSeamsBySource(ctx context.Context, sourceID string, fn func(hexxladb.SeamRecord) bool) error
	// AscendSeamsInTimeBucket scans the seam-time/ secondary index for a UTC week bucket.
	AscendSeamsInTimeBucket(ctx context.Context, bucket int64, fn func(hexxladb.SeamRecord) bool) error
	// WalkRing visits each cell on the ring at hex distance ring from center
	// (load_context ring order). fn receives raw bytes and ok=false when missing.
	WalkRing(ctx context.Context, center hexxladb.Coord, ring int, fn func(hexxladb.Coord, []byte, bool) bool) error
	// WalkRingAt calls fn only for cells whose validity contains asOf (single-version filter; not MVCC).
	WalkRingAt(ctx context.Context, center hexxladb.Coord, ring int, asOf time.Time, fn func(hexxladb.Coord, hexxladb.CellRecord) bool) error
	FindSeams(ctx context.Context, center hexxladb.Coord, radius int, unresolvedOnly bool) ([]hexxladb.SeamRecord, error)
	// FindSeamsAt is like FindSeams but only includes seams whose validity contains asOf (single-version filter; not MVCC).
	FindSeamsAt(ctx context.Context, center hexxladb.Coord, radius int, unresolvedOnly bool, asOf time.Time) ([]hexxladb.SeamRecord, error)
	ScanContextRaw(ctx context.Context, center hexxladb.Coord, maxR, maxCells int) ([]hexxladb.CellRecord, error)
	// ScanContextAtRaw is like ScanContextRaw but skips cells whose validity does not contain asOf.
	ScanContextAtRaw(ctx context.Context, center hexxladb.Coord, maxR, maxCells int, asOf time.Time) ([]hexxladb.CellRecord, error)
	// WalkRingFacets loads facet records (bit i = facet_id i) for each ring cell that passes the optional asOf filter.
	WalkRingFacets(ctx context.Context, center hexxladb.Coord, ring int, facetMask uint8, asOf *time.Time, fn func(hexxladb.Coord, hexxladb.CellRecord, []hexxladb.FacetWalkRecord) bool) error
	ResolveSeam(ctx context.Context, id, resolutionStatus, resolutionNote string) error
	PutSeam(ctx context.Context, rec hexxladb.SeamRecord) error
	MarkConflict(ctx context.Context, cellA, cellB hexxladb.Coord, reason string) error

	PutFacet(ctx context.Context, rec hexxladb.FacetWalkRecord) error
	UpdateFacet(ctx context.Context, rec hexxladb.FacetWalkRecord) error
	GetFacet(ctx context.Context, key hexxladb.PackedCoord, facetID byte) (hexxladb.FacetWalkRecord, bool, error)
	AscendFacetsForCell(ctx context.Context, key hexxladb.PackedCoord, fn func(hexxladb.FacetWalkRecord) bool) error
	PutEdge(ctx context.Context, rec hexxladb.EdgeWalkRecord) error
	LinkCells(ctx context.Context, from, to hexxladb.Coord, relationType string, weight float64, prov hexxladb.ProvenanceWire) error
	GetEdge(ctx context.Context, from, to hexxladb.PackedCoord, relationType string) (hexxladb.EdgeWalkRecord, bool, error)
	AscendEdgesFrom(ctx context.Context, from hexxladb.PackedCoord, fn func(hexxladb.EdgeWalkRecord) bool) error
	// DeleteCell removes a cell and all associated data (secondary indexes, facets, outbound edges).
	// Idempotent: deleting a non-existent cell returns nil. Seams are not removed.
	DeleteCell(ctx context.Context, key hexxladb.PackedCoord) error
}
