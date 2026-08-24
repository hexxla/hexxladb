package hexxladb_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func openMultiCtxDB(t *testing.T) *hexxladb.DB {
	t.Helper()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "multicontext.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})
	return db
}

func TestLoadContext_NoSeeds(t *testing.T) {
	t.Parallel()
	db := openMultiCtxDB(t)
	ctx := context.Background()

	if err := db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{})
		if err != nil {
			return err
		}
		if len(pack.Cells) != 0 {
			t.Errorf("empty Seeds produced %d cells, want 0", len(pack.Cells))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadContext_NegativeMaxCells(t *testing.T) {
	t.Parallel()
	db := openMultiCtxDB(t)
	err := db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.LoadContext(t.Context(), hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{{}},
			MaxCells: -1,
		})
		return err
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
}

func TestLoadContext_SingleSeed(t *testing.T) {
	t.Parallel()
	db := openMultiCtxDB(t)
	ctx := context.Background()

	center := hexxladb.Coord{Q: 0, R: 0}
	pk := mustPackTest(t, center)

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(ctx, hexxladb.NewFactCell(pk, "center cell content", "src", "topic", 0.9))
	}); err != nil {
		t.Fatal(err)
	}

	var pack hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		pack, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:   []hexxladb.Coord{center},
			MaxRing: 2,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if len(pack.Cells) == 0 {
		t.Error("expected at least one cell in pack")
	}
}

func TestLoadContext_MultiSeed_DeduplicateCoords(t *testing.T) {
	t.Parallel()
	db := openMultiCtxDB(t)
	ctx := context.Background()

	// Two seeds that share a neighbour at (1,0).
	seedA := hexxladb.Coord{Q: 0, R: 0}
	seedB := hexxladb.Coord{Q: 2, R: 0}
	shared := hexxladb.Coord{Q: 1, R: 0}

	for _, c := range []hexxladb.Coord{seedA, seedB, shared} {
		pk := mustPackTest(t, c)
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(ctx, hexxladb.NewFactCell(pk, "content", "src", "topic", 0.8))
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.MarkConflict(seedA, seedB, "multi-seed seam")
	}); err != nil {
		t.Fatal(err)
	}

	// Multi-seed via LoadContext always deduplicates (concurrent assembly).
	var pack hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		pack, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:   []hexxladb.Coord{seedA, seedB},
			MaxRing: 1,
			Assembly: hexxladb.ContextAssemblyConfig{
				IncludeSeams: true,
			},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// No repeated coords in the returned pack.
	seen := map[hexxladb.Coord]int{}
	for _, cv := range pack.Cells {
		seen[cv.Coord]++
	}
	for coord, count := range seen {
		if count > 1 {
			t.Errorf("coord %v appears %d times in pack", coord, count)
		}
	}
	if len(pack.Seams) != 1 {
		t.Fatalf("multi-seed seams = %d, want one deduplicated seam", len(pack.Seams))
	}
}

func TestLoadContext_SharedResultLimit(t *testing.T) {
	t.Parallel()
	db := openMultiCtxDB(t)
	ctx := context.Background()

	coords := []hexxladb.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 2, R: 0}, {Q: 0, R: 1}, {Q: 1, R: 1}}
	for _, c := range coords {
		pk := mustPackTest(t, c)
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(ctx, hexxladb.NewFactCell(pk, "content", "src", "topic", 0.8))
		}); err != nil {
			t.Fatal(err)
		}
	}

	const maxCells = 3
	var pack hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		pack, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{{Q: 0, R: 0}, {Q: 2, R: 0}},
			MaxRing:  2,
			MaxCells: maxCells,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if len(pack.Cells) != maxCells {
		t.Fatalf("cells = %d, want shared limit %d", len(pack.Cells), maxCells)
	}
	if !pack.Stats.ResultLimitReached {
		t.Error("ResultLimitReached = false, want true")
	}
	if pack.Cells[0].Coord != coords[0] || pack.Cells[1].Coord != coords[2] {
		t.Fatalf("first merge round = %v, %v; want seed order %v, %v", pack.Cells[0].Coord, pack.Cells[1].Coord, coords[0], coords[2])
	}
	wantOrder := make([]hexxladb.Coord, len(pack.Cells))
	for i := range pack.Cells {
		wantOrder[i] = pack.Cells[i].Coord
	}
	for range 10 {
		err := db.View(func(tx *hexxladb.Tx) error {
			again, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
				Seeds:    []hexxladb.Coord{{Q: 0, R: 0}, {Q: 2, R: 0}},
				MaxRing:  2,
				MaxCells: maxCells,
			})
			if err != nil {
				return err
			}
			for i := range wantOrder {
				if again.Cells[i].Coord != wantOrder[i] {
					t.Fatalf("repeat order[%d] = %v, want %v", i, again.Cells[i].Coord, wantOrder[i])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadContext_StatsAccumulated(t *testing.T) {
	t.Parallel()
	db := openMultiCtxDB(t)
	ctx := context.Background()

	coord := hexxladb.Coord{Q: 0, R: 0}
	pk := mustPackTest(t, coord)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(ctx, hexxladb.NewFactCell(pk, "some content", "src", "topic", 0.9))
	}); err != nil {
		t.Fatal(err)
	}

	var pack hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		pack, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:   []hexxladb.Coord{coord},
			MaxRing: 1,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if pack.Stats.CandidatesScanned == 0 {
		t.Error("Stats.CandidatesScanned = 0, expected > 0 after assembly")
	}
}
