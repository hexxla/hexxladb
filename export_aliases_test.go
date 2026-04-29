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
	db, err := hexxladb.Open(path, nil)
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
		return tx.LinkCells(a, b, "rel", 1, prov)
	}); err != nil {
		t.Fatal(err)
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
}
