package hexxladb_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestTx_MarkConflict(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "mc.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 1, R: 0}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.MarkConflict(a, b, "manual test")
	}); err != nil {
		t.Fatal(err)
	}

	pa, _ := lattice.Pack(a)
	pb, _ := lattice.Pack(b)
	err = db.View(func(tx *hexxladb.Tx) error {
		seams, err := tx.FindSeams(context.Background(), a, 2, true)
		if err != nil {
			return err
		}
		if len(seams) != 1 {
			t.Fatalf("seams=%d", len(seams))
		}
		if seams[0].SeamType != "mark_conflict" || seams[0].Reason != "manual test" {
			t.Fatalf("seam=%+v", seams[0])
		}
		lo, hi := record.CanonicalCellPair(pa, pb)
		if seams[0].CellA != lo || seams[0].CellB != hi {
			t.Fatalf("canonical mismatch")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Swapped endpoints should still find the same seam (canonical pair).
	if err := db.View(func(tx *hexxladb.Tx) error {
		seams, err := tx.FindSeams(context.Background(), b, 2, true)
		if err != nil {
			return err
		}
		if len(seams) != 1 {
			t.Fatalf("swapped: seams=%d", len(seams))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTx_UpdateFacet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "uf.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	key, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	if err != nil {
		t.Fatal(err)
	}
	raw := "hello"
	cell := record.CellRecord{
		Key:        key,
		RawContent: raw,
		Provenance: record.ProvenanceWire{SourceID: "s", Confidence: 1, CreatedAt: 1, UpdatedAt: 1},
		Validity:   record.ValidityWire{},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(cell) }); err != nil {
		t.Fatal(err)
	}

	hash := record.HashRawContent([]byte(raw))
	badRec := record.FacetRecord{
		Key:            key,
		FacetID:        1,
		DerivedContent: "x",
		LastRotated:    1,
		DerivationHash: [32]byte{9},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.UpdateFacet(badRec) }); err == nil {
		t.Fatal("expected ErrFacetDerivationMismatch")
	} else if !errors.Is(err, hexxladb.ErrFacetDerivationMismatch) {
		t.Fatalf("got %v", err)
	}

	goodRec := badRec
	goodRec.DerivationHash = hash
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.UpdateFacet(goodRec) }); err != nil {
		t.Fatal(err)
	}

	emptyKey, _ := lattice.Pack(lattice.Coord{Q: 100, R: -40})
	nocell := goodRec
	nocell.Key = emptyKey
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.UpdateFacet(nocell) }); err == nil {
		t.Fatal("expected ErrCellNotFound")
	} else if !errors.Is(err, hexxladb.ErrCellNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestTx_LinkCells(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "lc.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	from := lattice.Coord{Q: 0, R: 0}
	to := lattice.Coord{Q: 1, R: 0}
	prov := record.ProvenanceWire{SourceID: "l", Confidence: 1, CreatedAt: 1, UpdatedAt: 1}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.LinkCells(from, to, "e", 0.5, prov)
	}); err != nil {
		t.Fatal(err)
	}
	pf, _ := lattice.Pack(from)
	pt, _ := lattice.Pack(to)
	err = db.View(func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetEdge(pf, pt, "e")
		if err != nil || !ok || got.Weight != 0.5 {
			t.Fatalf("ok=%v w=%v err=%v", ok, got.Weight, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
