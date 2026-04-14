package hexxladb_test

import (
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestTx_FacetPutGetAscend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	key, err := lattice.Pack(lattice.Coord{Q: 1, R: -1})
	if err != nil {
		t.Fatal(err)
	}
	rec := record.FacetRecord{
		Key:            key,
		FacetID:        2,
		DerivedContent: "d",
		LastRotated:    99,
		DerivationHash: [32]byte{1, 2, 3},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutFacet(rec) }); err != nil {
		t.Fatal(err)
	}

	var n int
	err = db.View(func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetFacet(key, 2)
		if err != nil || !ok || got.DerivedContent != "d" {
			t.Fatalf("GetFacet ok=%v err=%v content=%q", ok, err, got.DerivedContent)
		}
		return tx.AscendFacetsForCell(key, func(r record.FacetRecord) bool {
			n++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("AscendFacetsForCell count=%d", n)
	}
}

func TestTx_EdgePutGetAscend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "e.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	a, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	b, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	c, _ := lattice.Pack(lattice.Coord{Q: 2, R: 0})
	rec := record.EdgeRecord{
		From:         a,
		To:           b,
		RelationType: "north",
		Weight:       1,
		Provenance:   record.ProvenanceWire{SourceID: "t", Confidence: 1, CreatedAt: 1, UpdatedAt: 1},
	}
	rec2 := record.EdgeRecord{
		From:         a,
		To:           c,
		RelationType: "skip",
		Weight:       2,
		Provenance:   record.ProvenanceWire{SourceID: "t", Confidence: 1, CreatedAt: 1, UpdatedAt: 1},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutEdge(rec); err != nil {
			return err
		}
		return tx.PutEdge(rec2)
	}); err != nil {
		t.Fatal(err)
	}

	var seen int
	err = db.View(func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetEdge(a, b, "north")
		if err != nil || !ok || got.Weight != 1 {
			t.Fatalf("GetEdge ok=%v w=%v err=%v", ok, got.Weight, err)
		}
		return tx.AscendEdgesFrom(a, func(r record.EdgeRecord) bool {
			seen++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("AscendEdgesFrom want 2 got %d", seen)
	}
}
