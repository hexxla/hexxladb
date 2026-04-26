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

func TestLoadMultiContextPack_NoCenters(t *testing.T) {
	t.Parallel()
	db := openMultiCtxDB(t)
	ctx := context.Background()

	if err := db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadMultiContextPack(ctx, hexxladb.MultiContextConfig{})
		if err != nil {
			return err
		}
		if len(pack.Cells) != 0 {
			t.Errorf("empty Centers produced %d cells, want 0", len(pack.Cells))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMultiContextPack_SingleSeed(t *testing.T) {
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
		pack, err = tx.LoadMultiContextPack(ctx, hexxladb.MultiContextConfig{
			Centers:   []hexxladb.Coord{center},
			MaxR:      2,
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

func TestLoadMultiContextPack_MultiSeed_DeduplicateCoords(t *testing.T) {
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

	// Without dedup.
	var packNoDedup hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		packNoDedup, err = tx.LoadMultiContextPack(ctx, hexxladb.MultiContextConfig{
			Centers:           []hexxladb.Coord{seedA, seedB},
			MaxR:              1,
			MaxTokens:         8192,
			DeduplicateCoords: false,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// With dedup.
	var packDedup hexxladb.ContextPack
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		packDedup, err = tx.LoadMultiContextPack(ctx, hexxladb.MultiContextConfig{
			Centers:           []hexxladb.Coord{seedA, seedB},
			MaxR:              1,
			MaxTokens:         8192,
			DeduplicateCoords: true,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Dedup must produce fewer or equal cells than no-dedup.
	if len(packDedup.Cells) > len(packNoDedup.Cells) {
		t.Errorf("dedup pack has %d cells > no-dedup %d cells; dedup should reduce or equal",
			len(packDedup.Cells), len(packNoDedup.Cells))
	}

	// Dedup pack must have no repeated coords.
	seen := map[hexxladb.Coord]int{}
	for _, cv := range packDedup.Cells {
		seen[cv.Coord]++
	}
	for coord, count := range seen {
		if count > 1 {
			t.Errorf("coord %v appears %d times in deduplicated pack", coord, count)
		}
	}
}

func TestLoadMultiContextPack_SharedBudget(t *testing.T) {
	t.Parallel()
	db := openMultiCtxDB(t)
	ctx := context.Background()

	// Write cells with enough content to stress the budget.
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
		pack, err = tx.LoadMultiContextPack(ctx, hexxladb.MultiContextConfig{
			Centers:           []hexxladb.Coord{{Q: 0, R: 0}, {Q: 2, R: 0}},
			MaxR:              2,
			MaxTokens:         tinyBudget,
			Budgeter:          hexxladb.ByteLenBudgeter{},
			DeduplicateCoords: true,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Verify budget was respected: total token usage ≤ tinyBudget.
	total := 0
	for _, cv := range pack.Cells {
		total += hexxladb.ByteLenBudgeter{}.CountTokens(cv.RawContent)
	}
	if total > tinyBudget {
		t.Errorf("total tokens %d exceeds MaxTokens %d", total, tinyBudget)
	}
}

func TestLoadMultiContextPack_StatsAccumulated(t *testing.T) {
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
		pack, err = tx.LoadMultiContextPack(ctx, hexxladb.MultiContextConfig{
			Centers:   []hexxladb.Coord{coord},
			MaxR:      1,
			MaxTokens: 4096,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Stats should reflect at least one candidate scanned.
	if pack.Stats.CandidatesScanned == 0 {
		t.Error("Stats.CandidatesScanned = 0, expected > 0 after assembly")
	}
}
