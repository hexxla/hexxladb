package hexxladb_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
)

// TestExportedWalkAliasesAndTemplateHelpers verifies that embedders can use
// [hexxladb.FacetWalkRecord] / [hexxladb.EdgeWalkRecord] in facet/edge walk
// closures and [hexxladb.NewFacetDerived] / [hexxladb.NewProvenanceWire] for
// writes without importing internal packages.
func TestExportedWalkAliasesAndTemplateHelpers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "export_aliases.db")
	var hookedSeams int
	db, err := hexxladb.Open(path, &hexxladb.Options{
		AfterPutSeam: hexxladb.AfterPutSeamHookFunc(func(_ context.Context, rec hexxladb.SeamRecord) error {
			if rec.ID == "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
				hookedSeams++
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	a := hexxladb.Coord{Q: 0, R: 0}
	b := hexxladb.Coord{Q: 1, R: 0}
	pkA, err := hexxladb.Pack(a)
	if err != nil {
		t.Fatal(err)
	}
	pkB, err := hexxladb.Pack(b)
	if err != nil {
		t.Fatal(err)
	}

	rot := time.Now().UTC().UnixNano()
	ctx := context.Background()
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkA, "cell-a", "test", "kind", 0.9)); err != nil {
			return err
		}
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkB, "cell-b", "test", "kind", 0.9)); err != nil {
			return err
		}
		facetRec := hexxladb.NewFacetDerived(pkA, 0, "derived", rot)
		if err := tx.PutFacet(facetRec); err != nil {
			return err
		}
		prov := hexxladb.NewProvenanceWire("mcp-test", 1)
		if err := tx.LinkCells(a, b, "rel", 1, prov); err != nil {
			return err
		}
		return tx.PutSeam(ctx, hexxladb.SeamRecord{
			ID:         "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			CellA:      pkA,
			CellB:      pkB,
			SeamType:   hexxladb.SeamTypeConflict,
			Provenance: prov,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if hookedSeams != 1 {
		t.Fatalf("AfterPutSeamHookFunc: want 1 seam, got %d", hookedSeams)
	}

	var facets int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendFacetsForCell(pkA, func(r hexxladb.FacetWalkRecord) bool {
			if r.DerivedContent == "derived" {
				facets++
			}
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if facets != 1 {
		t.Fatalf("AscendFacetsForCell FacetWalkRecord callback: want 1 facet, got %d", facets)
	}

	var edges int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendEdgesFrom(pkA, func(r hexxladb.EdgeWalkRecord) bool {
			if r.RelationType == "rel" {
				edges++
			}
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if edges != 1 {
		t.Fatalf("AscendEdgesFrom EdgeWalkRecord callback: want 1 edge, got %d", edges)
	}

	var seams int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendSeamsBySource(ctx, "mcp-test", func(rec hexxladb.SeamRecord) bool {
			if rec.ID == "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
				seams++
			}
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if seams != 1 {
		t.Fatalf("AscendSeamsBySource SeamRecord callback: want 1 seam, got %d", seams)
	}
}
