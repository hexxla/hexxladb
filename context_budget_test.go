package hexxladb

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestContextReaderSharesAndEnforcesWorkBudgets(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openMVCCRegressionDB(t, filepath.Join(t.TempDir(), "budget.db"), &Options{EnableMVCC: true})
	t.Cleanup(func() { _ = db.Close() })
	from, to := Coord{}, Coord{Q: 1}
	if err := db.Update(func(tx *Tx) error {
		if err := tx.LinkCells(from, to, "rel", 1, ProvenanceWire{}); err != nil {
			return err
		}
		return tx.MarkSupersedes(to, from, "test")
	}); err != nil {
		t.Fatal(err)
	}
	fromKey, err := Pack(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *Tx) error {
		reader := &contextReader{Tx: tx, ctx: ctx}
		reader.edgeRows.Store(MaxContextEdgeRows - 1)
		visited := 0
		visit := func(EdgeWalkRecord) bool { visited++; return true }
		if err := reader.AscendEdgesFrom(fromKey, visit); err != nil {
			return err
		}
		if err := reader.AscendEdgesFrom(fromKey, visit); !errors.Is(err, ErrSpatialScanLimit) || visited != 1 {
			t.Fatalf("edge budget = %v, visited=%d", err, visited)
		}

		reader.seamWork.Store(MaxContextSeamWork - 1)
		if seams, err := reader.FindSeams(ctx, from, 0, false); !errors.Is(err, ErrSpatialScanLimit) || len(seams) != 0 {
			t.Fatalf("seam budget = %v, seams=%+v", err, seams)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
