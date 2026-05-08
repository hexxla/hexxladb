package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hexxla/hexxladb/internal/app"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestNew(t *testing.T) {
	t.Parallel()
	if app.New() == nil {
		t.Fatal("New() returned nil")
	}
}

// noStorageErr asserts that err wraps ErrNoStorage.
func noStorageErr(t *testing.T, method string, err error) {
	t.Helper()
	if !errors.Is(err, app.ErrNoStorage) {
		t.Errorf("%s: expected ErrNoStorage, got %v", method, err)
	}
}

func TestService_ErrNoStorage_AllMethods(t *testing.T) {
	t.Parallel()
	s := app.New()
	ctx := context.Background()
	coord := lattice.Coord{}
	pc := lattice.PackedCoord{}
	now := time.Now()

	noStorageErr(t, "PutCell", s.PutCell(ctx, record.CellRecord{}))

	_, _, err := s.GetCell(ctx, pc)
	noStorageErr(t, "GetCell", err)

	noStorageErr(t, "AscendCellsBySource",
		s.AscendCellsBySource(ctx, "s", func(record.CellRecord) bool { return true }))

	noStorageErr(t, "AscendCellsInTimeBucket",
		s.AscendCellsInTimeBucket(ctx, 0, func(record.CellRecord) bool { return true }))

	noStorageErr(t, "AscendCellsByTag",
		s.AscendCellsByTag(ctx, "tag", func(record.CellRecord) bool { return true }))

	noStorageErr(t, "AscendDistinctTags",
		s.AscendDistinctTags(ctx, func(string) bool { return true }))

	_, err = s.ListExistingTopics(ctx)
	noStorageErr(t, "ListExistingTopics", err)

	noStorageErr(t, "WalkRing",
		s.WalkRing(ctx, coord, 0, func(lattice.Coord, []byte, bool) bool { return true }))

	noStorageErr(t, "WalkRingAt",
		s.WalkRingAt(ctx, coord, 0, now, func(lattice.Coord, record.CellRecord) bool { return true }))

	_, err = s.FindSeams(ctx, coord, 1, false)
	noStorageErr(t, "FindSeams", err)

	_, err = s.FindSeamsAt(ctx, coord, 1, false, now)
	noStorageErr(t, "FindSeamsAt", err)

	_, err = s.ScanContextRaw(ctx, coord, 3, 10)
	noStorageErr(t, "ScanContextRaw", err)

	_, err = s.ScanContextAtRaw(ctx, coord, 3, 10, now)
	noStorageErr(t, "ScanContextAtRaw", err)

	noStorageErr(t, "WalkRingFacets",
		s.WalkRingFacets(ctx, coord, 0, 0, nil, func(lattice.Coord, record.CellRecord, []record.FacetRecord) bool { return true }))

	noStorageErr(t, "ResolveSeam", s.ResolveSeam(ctx, "id", "resolved", ""))

	noStorageErr(t, "PutSeam", s.PutSeam(ctx, record.SeamRecord{}))

	noStorageErr(t, "MarkConflict", s.MarkConflict(ctx, coord, coord, "reason"))

	noStorageErr(t, "AscendSeamsBySource",
		s.AscendSeamsBySource(ctx, "s", func(record.SeamRecord) bool { return true }))

	noStorageErr(t, "AscendSeamsInTimeBucket",
		s.AscendSeamsInTimeBucket(ctx, 0, func(record.SeamRecord) bool { return true }))

	noStorageErr(t, "PutFacet", s.PutFacet(ctx, record.FacetRecord{}))

	noStorageErr(t, "UpdateFacet", s.UpdateFacet(ctx, record.FacetRecord{}))

	_, _, err = s.GetFacet(ctx, pc, 0)
	noStorageErr(t, "GetFacet", err)

	noStorageErr(t, "AscendFacetsForCell",
		s.AscendFacetsForCell(ctx, pc, func(record.FacetRecord) bool { return true }))

	noStorageErr(t, "PutEdge", s.PutEdge(ctx, record.EdgeRecord{}))

	noStorageErr(t, "LinkCells",
		s.LinkCells(ctx, coord, coord, "rel", 1.0, record.ProvenanceWire{}))

	_, _, err = s.GetEdge(ctx, pc, pc, "rel")
	noStorageErr(t, "GetEdge", err)

	noStorageErr(t, "AscendEdgesFrom",
		s.AscendEdgesFrom(ctx, pc, func(record.EdgeRecord) bool { return true }))
}
