package hexxladb_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
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
		return tx.PutCell(t.Context(), rec)
	}); err != nil {
		t.Fatal(err)
	}
	opts := hexxladb.AssembleCellViewOpts{IncludeFacets: false}
	err = db.View(func(tx *hexxladb.Tx) error {
		v, err := tx.AssembleCellView(t.Context(), c, nil, opts)
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

func TestTx_LoadContext_nearestFirstResultLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "bounded.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	center := lattice.Coord{Q: 0, R: 0}
	cp, err := lattice.Pack(center)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	ring1 := lattice.Ring(center, 1)
	if len(ring1) != 6 {
		t.Fatal(len(ring1))
	}
	for i, c := range ring1[:3] {
		p, err := lattice.Pack(c)
		if err != nil {
			t.Fatal(err)
		}
		conf := float64(i) * 0.1
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
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{center},
			MaxRing:  1,
			MaxCells: 3,
			Assembly: hexxladb.ContextAssemblyConfig{
				Assemble: hexxladb.AssembleCellViewOpts{IncludeFacets: false},
			},
		})
		if err != nil {
			return err
		}
		want := []lattice.Coord{center, ring1[0], ring1[1]}
		if len(pack.Cells) != len(want) {
			t.Fatalf("expected %d bounded cells, got %d", len(want), len(pack.Cells))
		}
		for i := range want {
			if pack.Cells[i].Coord != want[i] {
				t.Fatalf("cell %d = %v, want nearest-first %v", i, pack.Cells[i].Coord, want[i])
			}
		}
		if !pack.Stats.ResultLimitReached {
			t.Fatal("ResultLimitReached = false, want true")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func mustPutCell(t *testing.T, db *hexxladb.DB, c lattice.Coord, content string) {
	t.Helper()
	p, err := lattice.Pack(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{
			Key:        p,
			RawContent: content,
			Provenance: record.ProvenanceWire{Confidence: 1.0},
		})
	}); err != nil {
		t.Fatal(err)
	}
}

// TestFilterSuperseded_excludesStale checks that a superseded cell is excluded and
// its successor is returned instead.
func TestFilterSuperseded_excludesStale(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "sup.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	center := lattice.Coord{Q: 0, R: 0}
	stale := lattice.Coord{Q: 1, R: 0}    // ring-1 cell, will be superseded
	current := lattice.Coord{Q: -1, R: 0} // ring-1 cell, the current truth

	mustPutCell(t, db, center, "center")
	mustPutCell(t, db, stale, "stale content")
	mustPutCell(t, db, current, "current content")

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.MarkSupersedes(current, stale, "updated")
	}); err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds: []hexxladb.Coord{center}, MaxRing: 1,
			Assembly: hexxladb.ContextAssemblyConfig{
				Assemble: hexxladb.AssembleCellViewOpts{IncludeFacets: false}, FilterSuperseded: true,
			},
		})
		if err != nil {
			return err
		}
		for _, cv := range pack.Cells {
			if cv.Coord == stale {
				t.Fatal("stale cell should have been excluded from context pack")
			}
		}
		var found bool
		for _, cv := range pack.Cells {
			if cv.Coord == current {
				found = true
			}
		}
		if !found {
			t.Fatal("current-truth cell should be present in context pack")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFilterSuperseded_OriginSuccessor(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)
	stale, origin := hexxladb.Coord{Q: 9}, hexxladb.Coord{}
	mustPutCell(t, db, stale, "stale")
	mustPutCell(t, db, origin, "current")
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.MarkSupersedes(origin, stale, "updated")
	}); err != nil {
		t.Fatal(err)
	}
	cfg := hexxladb.LoadContextConfig{
		Seeds: []hexxladb.Coord{stale}, MaxRing: 1,
		Assembly: hexxladb.ContextAssemblyConfig{FilterSuperseded: true, Explain: true},
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContext(t.Context(), cfg)
		if err != nil {
			return err
		}
		if len(pack.Cells) != 1 {
			t.Fatalf("got %d cells, want only the successor at the origin", len(pack.Cells))
		}
		cell := pack.Cells[0]
		if cell.Coord != origin || cell.RawContent != "current" || cell.SupersededFrom == nil || *cell.SupersededFrom != stale {
			t.Fatalf("unexpected successor: %+v", cell)
		}
		if len(pack.Explanations) == 0 || pack.Explanations[0].SupersededBy == nil || *pack.Explanations[0].SupersededBy != origin {
			t.Fatalf("explanations omitted the successor at the origin: %+v", pack.Explanations)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	packed, err := hexxladb.Pack(origin)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return hexxladb.PutRawFixture(tx, index.CellKeyWithVersion(packed, stats.CommitSeq+1), []byte{1})
	}); err != nil {
		t.Fatal(err)
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.LoadContext(t.Context(), cfg)
		return err
	})
	if !errors.Is(err, record.ErrInvalidRecord) {
		t.Fatalf("corrupt successor: got %v, want ErrInvalidRecord", err)
	}
}

func TestFilterSuperseded_HopLimit(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)
	const maxHops = 16
	var chain [maxHops + 2]hexxladb.Coord
	for i := range chain {
		chain[i] = hexxladb.Coord{Q: i * 3}
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for i, from := range chain[:len(chain)-1] {
			if err := tx.MarkSupersedes(chain[i+1], from, "updated"); err != nil {
				return err
			}
		}
		return putCell(tx, chain[len(chain)-1], "current")
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		cfg := hexxladb.LoadContextConfig{
			Seeds: []hexxladb.Coord{chain[1]}, MaxRing: 1,
			Assembly: hexxladb.ContextAssemblyConfig{FilterSuperseded: true},
		}
		pack, err := tx.LoadContext(t.Context(), cfg)
		if err != nil {
			return err
		}
		if len(pack.Cells) != 1 || pack.Cells[0].Coord != chain[len(chain)-1] {
			t.Fatalf("at the hop limit: got %v, want the terminal successor", pack.Cells)
		}
		cfg.Seeds[0] = chain[0]
		pack, err = tx.LoadContext(t.Context(), cfg)
		if err != nil {
			return err
		}
		if len(pack.Cells) != 0 {
			t.Fatalf("past the hop limit: got %v, want no successor", pack.Cells)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestFilterSuperseded_chainWalk checks that a multi-hop supersession chain resolves to the final truth.
func TestFilterSuperseded_chainWalk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "chain.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	center := lattice.Coord{Q: 0, R: 0}
	a := lattice.Coord{Q: 1, R: 0}  // stale
	b := lattice.Coord{Q: 0, R: 1}  // intermediate (a→b→c)
	c := lattice.Coord{Q: -1, R: 0} // final truth

	mustPutCell(t, db, center, "center")
	mustPutCell(t, db, a, "v1")
	mustPutCell(t, db, b, "v2")
	mustPutCell(t, db, c, "v3")

	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.MarkSupersedes(b, a, "v2 supersedes v1"); err != nil {
			return err
		}
		return tx.MarkSupersedes(c, b, "v3 supersedes v2")
	}); err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds: []hexxladb.Coord{center}, MaxRing: 1,
			Assembly: hexxladb.ContextAssemblyConfig{
				Assemble: hexxladb.AssembleCellViewOpts{IncludeFacets: false}, FilterSuperseded: true,
			},
		})
		if err != nil {
			return err
		}
		for _, cv := range pack.Cells {
			if cv.Coord == a {
				t.Error("stale cell a should not appear")
			}
		}
		var foundC bool
		for _, cv := range pack.Cells {
			if cv.Coord == c {
				foundC = true
			}
		}
		if !foundC {
			t.Fatal("final truth cell c should appear in pack")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestFilterSuperseded_noSuccessor checks that a superseded cell with no live successor is excluded.
func TestFilterSuperseded_noSuccessor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "nosuc.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	center := lattice.Coord{Q: 0, R: 0}
	stale := lattice.Coord{Q: 1, R: 0}
	ghost := lattice.Coord{Q: 0, R: 1} // successor coord — cell NOT stored in DB

	mustPutCell(t, db, center, "center")
	mustPutCell(t, db, stale, "stale")
	// Record supersedes seam: stale→ghost, but ghost has no cell record
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.MarkSupersedes(ghost, stale, "replaced by ghost (no cell)")
	}); err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds: []hexxladb.Coord{center}, MaxRing: 1,
			Assembly: hexxladb.ContextAssemblyConfig{
				Assemble: hexxladb.AssembleCellViewOpts{IncludeFacets: false}, FilterSuperseded: true,
			},
		})
		if err != nil {
			return err
		}
		for _, cv := range pack.Cells {
			if cv.Coord == stale {
				t.Fatal("stale cell with no live successor should be excluded")
			}
			if cv.Coord == ghost {
				t.Fatal("ghost cell (no stored cell) should not appear")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestFilterSuperseded_offByDefault checks that without FilterSuperseded, superseded cells still appear.
func TestFilterSuperseded_offByDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "off.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	center := lattice.Coord{Q: 0, R: 0}
	stale := lattice.Coord{Q: 1, R: 0}
	current := lattice.Coord{Q: -1, R: 0}

	mustPutCell(t, db, center, "center")
	mustPutCell(t, db, stale, "stale")
	mustPutCell(t, db, current, "current")

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.MarkSupersedes(current, stale, "updated")
	}); err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds: []hexxladb.Coord{center}, MaxRing: 1,
			Assembly: hexxladb.ContextAssemblyConfig{
				Assemble: hexxladb.AssembleCellViewOpts{IncludeFacets: false}, FilterSuperseded: false,
			},
		})
		if err != nil {
			return err
		}
		var foundStale bool
		for _, cv := range pack.Cells {
			if cv.Coord == stale {
				foundStale = true
			}
		}
		if !foundStale {
			t.Fatal("without FilterSuperseded, stale cell should still appear")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFilterCellViews(t *testing.T) {
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
}
