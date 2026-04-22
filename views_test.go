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

func TestTx_AssembleCellView(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "v.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	c := lattice.Coord{Q: 0, R: 1}
	p, err := lattice.Pack(c)
	if err != nil {
		t.Fatal(err)
	}
	rec := record.CellRecord{
		Key:        p,
		RawContent: "hello",
		Provenance: record.ProvenanceWire{SourceID: "s", Confidence: 0.9},
		Tags:       []string{"t1"},
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(context.Background(), rec)
	}); err != nil {
		t.Fatal(err)
	}
	opts := hexxladb.AssembleCellViewOpts{IncludeFacets: false}
	err = db.View(func(tx *hexxladb.Tx) error {
		v, err := tx.AssembleCellView(context.Background(), c, nil, opts)
		if err != nil {
			return err
		}
		if v.RawContent != "hello" || len(v.Tags) != 1 {
			t.Fatalf("view: %+v", v)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTx_LoadContextWithBudgeting_evictionOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "budget.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	center := lattice.Coord{Q: 0, R: 0}
	cp, err := lattice.Pack(center)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ring1 := lattice.Ring(center, 1)
	if len(ring1) != 6 {
		t.Fatal(len(ring1))
	}
	for i, c := range ring1[:3] {
		p, err := lattice.Pack(c)
		if err != nil {
			t.Fatal(err)
		}
		conf := float64(i) * 0.1 // 0, 0.1, 0.2 — lowest on ring 1 is cell 0
		rec := record.CellRecord{
			Key:        p,
			RawContent: "xxxx",
			Provenance: record.ProvenanceWire{Confidence: conf},
		}
		if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(ctx, rec) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(ctx, record.CellRecord{Key: cp, RawContent: "center", Provenance: record.ProvenanceWire{Confidence: 1}})
	}); err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContextWithBudgeting(ctx, center, 1, 14, hexxladb.ByteLenBudgeter{}, hexxladb.LoadContextBudgetConfig{
			Assemble:          hexxladb.AssembleCellViewOpts{IncludeFacets: false},
			MaxCandidateCells: 10,
			IncludeFacetText:  false,
			IncludeSeams:      false,
		})
		if err != nil {
			return err
		}
		if len(pack.Cells) != 3 {
			t.Fatalf("expected 3 cells after eviction, got %d", len(pack.Cells))
		}
		for _, v := range pack.Cells {
			if v.Coord == center {
				if v.RawContent != "center" {
					t.Fatal("center missing")
				}
				continue
			}
			if v.Provenance.Confidence == 0 {
				t.Fatal("lowest-confidence ring cell should have been dropped first")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTx_LoadContextPack_alias(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "pack.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	center := lattice.Coord{Q: 0, R: 0}
	ctx := context.Background()
	if err := db.Update(func(tx *hexxladb.Tx) error {
		p, err := lattice.Pack(center)
		if err != nil {
			return err
		}
		return tx.PutCell(ctx, record.CellRecord{Key: p, RawContent: "c"})
	}); err != nil {
		t.Fatal(err)
	}
	var a, b hexxladb.ContextPack
	cfg := hexxladb.LoadContextBudgetConfig{
		Assemble:          hexxladb.AssembleCellViewOpts{IncludeFacets: false},
		MaxCandidateCells: 4,
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		var err1, err2 error
		a, err1 = tx.LoadContextWithBudgeting(ctx, center, 0, 100, hexxladb.ByteLenBudgeter{}, cfg)
		b, err2 = tx.LoadContextPack(ctx, center, 0, 100, hexxladb.ByteLenBudgeter{}, cfg)
		return errors.Join(err1, err2)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Cells) != len(b.Cells) || a.TotalTokens != b.TotalTokens {
		t.Fatalf("pack vs budgeting: %+v vs %+v", a, b)
	}
}

func TestFilterCellViews_and_TruncateCellViewsToTokenBudget(t *testing.T) {
	t.Parallel()
	a := lattice.Coord{Q: 1, R: 0}
	b := lattice.Coord{Q: 0, R: 1}
	va := hexxladb.CellView{Coord: a, RawContent: "aaaa", Provenance: record.ProvenanceWire{Confidence: 0.5}}
	vb := hexxladb.CellView{Coord: b, RawContent: "bbb", Provenance: record.ProvenanceWire{Confidence: 0.9}}
	all := []hexxladb.CellView{va, vb}
	filtered := hexxladb.FilterCellViews(all, func(v hexxladb.CellView) bool {
		return v.Provenance.Confidence >= 0.9
	})
	if len(filtered) != 1 || filtered[0].Coord != b {
		t.Fatalf("filter: %+v", filtered)
	}
	trunc, n := hexxladb.TruncateCellViewsToTokenBudget(all, hexxladb.ByteLenBudgeter{}, 4, false)
	if n != 4 || len(trunc) != 1 || trunc[0].RawContent != "aaaa" {
		t.Fatalf("truncate: n=%d pack=%+v", n, trunc)
	}
}
