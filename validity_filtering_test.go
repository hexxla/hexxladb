package hexxladb_test

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestTx_FindSeamsAt_validityFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "seams_at.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 1, R: 0}
	pa, err := lattice.Pack(a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := lattice.Pack(b)
	if err != nil {
		t.Fatal(err)
	}
	var lo int64 = 100
	var hi int64 = 200
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	seam := record.SeamRecord{
		ID:               id,
		CellA:            pa,
		CellB:            pb,
		SeamType:         "t",
		Reason:           "",
		ConfidenceDelta:  0,
		DetectedAt:       1,
		ResolutionStatus: "",
		ResolutionNote:   "",
		Validity:         record.ValidityWire{ValidFrom: &lo, ValidTo: &hi},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutSeam(context.Background(), seam) }); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	asOfInside := time.Unix(0, 150).UTC()
	err = db.View(func(tx *hexxladb.Tx) error {
		seams, err := tx.FindSeamsAt(ctx, a, 2, false, asOfInside)
		if err != nil {
			return err
		}
		if len(seams) != 1 || seams[0].ID != id {
			t.Fatalf("inside window: got %+v", seams)
		}
		seams, err = tx.FindSeamsAt(ctx, a, 2, false, time.Unix(0, 50).UTC())
		if err != nil {
			return err
		}
		if len(seams) != 0 {
			t.Fatalf("before window: want 0, got %d", len(seams))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTx_WalkRingAt_validityFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "walk_at.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	center := lattice.Coord{Q: 0, R: 0}
	p, err := lattice.Pack(center)
	if err != nil {
		t.Fatal(err)
	}
	var lo int64 = 100
	var hi int64 = 200
	rec := record.CellRecord{
		Key:        p,
		RawContent: "x",
		Validity:   record.ValidityWire{ValidFrom: &lo, ValidTo: &hi},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), rec) }); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	asOfInside := time.Unix(0, 150).UTC()
	var n int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.WalkRingAt(ctx, center, 0, asOfInside, func(c lattice.Coord, got record.CellRecord) bool {
			n++
			if got.RawContent != "x" {
				t.Errorf("content %q", got.RawContent)
			}
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("callbacks inside window: got %d", n)
	}

	asOfBefore := time.Unix(0, 50).UTC()
	n = 0
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.WalkRingAt(ctx, center, 0, asOfBefore, func(lattice.Coord, record.CellRecord) bool {
			n++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("callbacks before window: got %d", n)
	}
}

func TestTx_LoadContextAt_maxCellsAfterFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "lcat.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	center := lattice.Coord{Q: 0, R: 0}
	coords := lattice.WalkRings(nil, center, 1)
	if len(coords) < 2 {
		t.Fatalf("need at least 2 coords, got %d", len(coords))
	}
	var lo int64 = 0
	var hi int64 = 100
	// First coord in walk order: valid at t=50
	p0, err := lattice.Pack(coords[0])
	if err != nil {
		t.Fatal(err)
	}
	p1, err := lattice.Pack(coords[1])
	if err != nil {
		t.Fatal(err)
	}
	rec0 := record.CellRecord{Key: p0, RawContent: "a", Validity: record.ValidityWire{ValidFrom: &lo, ValidTo: &hi}}
	var past int64 = 200
	rec1 := record.CellRecord{Key: p1, RawContent: "b", Validity: record.ValidityWire{ValidFrom: &past, ValidTo: nil}}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(context.Background(), rec0); err != nil {
			return err
		}
		return tx.PutCell(context.Background(), rec1)
	}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	asOf := time.Unix(0, 50).UTC()
	var out []record.CellRecord
	err = db.View(func(tx *hexxladb.Tx) error {
		var inner error
		out, inner = tx.ScanContextAtRaw(ctx, center, 1, 10, asOf)
		return inner
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].RawContent != "a" {
		t.Fatalf("got %+v", out)
	}

	var out2 []record.CellRecord
	err = db.View(func(tx *hexxladb.Tx) error {
		var inner error
		out2, inner = tx.ScanContextAtRaw(ctx, center, 1, 1, asOf)
		return inner
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out2) != 1 {
		t.Fatalf("maxCells=1 after filter: got %d", len(out2))
	}

	// The unified loader must propagate AsOf through its ordinary single-seed
	// ring path and continue past ineligible cells until MaxCells is filled.
	asOf = time.Unix(0, 250).UTC()
	var pack hexxladb.ContextPack
	err = db.View(func(tx *hexxladb.Tx) error {
		var inner error
		pack, inner = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{center},
			MaxRing:  1,
			MaxCells: 1,
			AsOf:     &asOf,
		})
		return inner
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Cells) != 1 || pack.Cells[0].RawContent != "b" {
		t.Fatalf("LoadContext AsOf result = %+v, want only future-valid cell b", pack.Cells)
	}
}

func TestTx_WalkRingFacets_maskAndAsOf(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "wf.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	center := lattice.Coord{Q: 0, R: 0}
	p, err := lattice.Pack(center)
	if err != nil {
		t.Fatal(err)
	}
	var lo int64 = 0
	var hi int64 = 1000
	cell := record.CellRecord{
		Key:        p,
		RawContent: "raw",
		Validity:   record.ValidityWire{ValidFrom: &lo, ValidTo: &hi},
	}
	f0 := record.FacetRecord{Key: p, FacetID: 0, DerivedContent: "f0", DerivationHash: record.HashRawContent([]byte("raw"))}
	f2 := record.FacetRecord{Key: p, FacetID: 2, DerivedContent: "f2", DerivationHash: record.HashRawContent([]byte("raw"))}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(context.Background(), cell); err != nil {
			return err
		}
		if err := tx.PutFacet(f0); err != nil {
			return err
		}
		return tx.PutFacet(f2)
	}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	asOf := time.Unix(0, 500).UTC()
	var gotFacets int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.WalkRingFacets(ctx, center, 0, 0x5, &asOf, func(_ lattice.Coord, _ record.CellRecord, facets []record.FacetRecord) bool {
			gotFacets = len(facets)
			if len(facets) != 2 {
				t.Fatalf("facet count %d", len(facets))
			}
			if facets[0].FacetID != 0 || facets[1].FacetID != 2 {
				t.Fatalf("order: %#v", facets)
			}
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotFacets != 2 {
		t.Fatalf("gotFacets %d", gotFacets)
	}

	asOfLate := time.Unix(0, 2000).UTC()
	n := 0
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.WalkRingFacets(ctx, center, 0, 0x5, &asOfLate, func(lattice.Coord, record.CellRecord, []record.FacetRecord) bool {
			n++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected no callbacks when asOf outside validity, got %d", n)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.WalkRingFacets(ctx, center, 0, 0x40, nil, func(lattice.Coord, record.CellRecord, []record.FacetRecord) bool {
			return true
		})
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("WalkRingFacets mask: %v", err)
	}
}
