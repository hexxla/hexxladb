package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/examples/ports_adapters/internal/app"
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
	coord := hexxladb.Coord{}
	pc := hexxladb.PackedCoord{}
	now := time.Now()

	noStorageErr(t, "PutCell", s.PutCell(ctx, hexxladb.CellRecord{}))

	_, _, err := s.GetCell(ctx, pc)
	noStorageErr(t, "GetCell", err)

	noStorageErr(t, "AscendCellsBySource",
		s.AscendCellsBySource(ctx, "s", func(hexxladb.CellRecord) bool { return true }))

	noStorageErr(t, "AscendCellsInTimeBucket",
		s.AscendCellsInTimeBucket(ctx, 0, func(hexxladb.CellRecord) bool { return true }))

	noStorageErr(t, "AscendCellsByTag",
		s.AscendCellsByTag(ctx, "tag", func(hexxladb.CellRecord) bool { return true }))

	noStorageErr(t, "AscendDistinctTags",
		s.AscendDistinctTags(ctx, func(string) bool { return true }))

	_, err = s.ListExistingTopics(ctx)
	noStorageErr(t, "ListExistingTopics", err)

	noStorageErr(t, "WalkRing",
		s.WalkRing(ctx, coord, 0, func(hexxladb.Coord, []byte, bool) bool { return true }))

	noStorageErr(t, "WalkRingAt",
		s.WalkRingAt(ctx, coord, 0, now, func(hexxladb.Coord, hexxladb.CellRecord) bool { return true }))

	_, err = s.FindSeams(ctx, coord, 1, false)
	noStorageErr(t, "FindSeams", err)

	_, err = s.FindSeamsAt(ctx, coord, 1, false, now)
	noStorageErr(t, "FindSeamsAt", err)

	_, err = s.ScanContextRaw(ctx, coord, 3, 10)
	noStorageErr(t, "ScanContextRaw", err)

	_, err = s.ScanContextAtRaw(ctx, coord, 3, 10, now)
	noStorageErr(t, "ScanContextAtRaw", err)

	noStorageErr(t, "WalkRingFacets",
		s.WalkRingFacets(ctx, coord, 0, 0, nil, func(hexxladb.Coord, hexxladb.CellRecord, []hexxladb.FacetWalkRecord) bool { return true }))

	noStorageErr(t, "ResolveSeam", s.ResolveSeam(ctx, "id", "resolved", ""))

	noStorageErr(t, "PutSeam", s.PutSeam(ctx, hexxladb.SeamRecord{}))

	noStorageErr(t, "MarkConflict", s.MarkConflict(ctx, coord, coord, "reason"))

	noStorageErr(t, "AscendSeamsBySource",
		s.AscendSeamsBySource(ctx, "s", func(hexxladb.SeamRecord) bool { return true }))

	noStorageErr(t, "AscendSeamsInTimeBucket",
		s.AscendSeamsInTimeBucket(ctx, 0, func(hexxladb.SeamRecord) bool { return true }))

	noStorageErr(t, "PutFacet", s.PutFacet(ctx, hexxladb.FacetWalkRecord{}))

	noStorageErr(t, "UpdateFacet", s.UpdateFacet(ctx, hexxladb.FacetWalkRecord{}))

	_, _, err = s.GetFacet(ctx, pc, 0)
	noStorageErr(t, "GetFacet", err)

	noStorageErr(t, "AscendFacetsForCell",
		s.AscendFacetsForCell(ctx, pc, func(hexxladb.FacetWalkRecord) bool { return true }))

	noStorageErr(t, "PutEdge", s.PutEdge(ctx, hexxladb.EdgeWalkRecord{}))

	noStorageErr(t, "LinkCells",
		s.LinkCells(ctx, coord, coord, "rel", 1.0, hexxladb.ProvenanceWire{}))

	_, _, err = s.GetEdge(ctx, pc, pc, "rel")
	noStorageErr(t, "GetEdge", err)

	noStorageErr(t, "AscendEdgesFrom",
		s.AscendEdgesFrom(ctx, pc, func(hexxladb.EdgeWalkRecord) bool { return true }))
}
