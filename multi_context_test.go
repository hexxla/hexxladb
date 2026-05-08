package hexxladb_test

import (
	"context"
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
			Seeds:     []hexxladb.Coord{center},
			MaxRing:   2,
			MaxTokens: 4096,
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

	// Multi-seed via LoadContext always deduplicates (concurrent assembly).
	var pack hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		pack, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:     []hexxladb.Coord{seedA, seedB},
			MaxRing:   1,
			MaxTokens: 8192,
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
}

func TestLoadContext_SharedBudget(t *testing.T) {
	t.Parallel()
	db := openMultiCtxDB(t)
	ctx := context.Background()

	longContent := "This is a fairly long cell content string that will consume budget tokens. "
	coords := []hexxladb.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 2, R: 0}, {Q: 0, R: 1}, {Q: 1, R: 1}}
	for _, c := range coords {
		pk := mustPackTest(t, c)
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(ctx, hexxladb.NewFactCell(pk, longContent, "src", "topic", 0.8))
		}); err != nil {
			t.Fatal(err)
		}
	}

	tinyBudget := 50 // bytes — fits roughly 1 cell
	var pack hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		pack, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:     []hexxladb.Coord{{Q: 0, R: 0}, {Q: 2, R: 0}},
			MaxRing:   2,
			MaxTokens: tinyBudget,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	total := 0
	for _, cv := range pack.Cells {
		total += hexxladb.ByteLenBudgeter{}.CountTokens(cv.RawContent)
	}
	if total > tinyBudget {
		t.Errorf("total tokens %d exceeds MaxTokens %d", total, tinyBudget)
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
			Seeds:     []hexxladb.Coord{coord},
			MaxRing:   1,
			MaxTokens: 4096,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if pack.Stats.CandidatesScanned == 0 {
		t.Error("Stats.CandidatesScanned = 0, expected > 0 after assembly")
	}
}
