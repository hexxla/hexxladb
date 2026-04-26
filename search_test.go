package hexxladb_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func openSearchDB(t *testing.T) *hexxladb.DB {
	t.Helper()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "search.db"), nil)
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

func TestSearchCells_EmptyDB(t *testing.T) {
	t.Parallel()
	db := openSearchDB(t)
	ctx := context.Background()

	var results []hexxladb.CellSearchResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchCells(ctx, hexxladb.CellSearchConfig{Query: "anything"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestSearchCells_TagExactMatch(t *testing.T) {
	t.Parallel()
	db := openSearchDB(t)
	ctx := context.Background()

	coordA := hexxladb.Coord{Q: 0, R: 0}
	coordB := hexxladb.Coord{Q: 1, R: 0}
	pkA := mustPackTest(t, coordA)
	pkB := mustPackTest(t, coordB)

	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkA, "The sky is blue", "src", "science", 0.9)); err != nil {
			return err
		}
		return tx.PutCell(ctx, hexxladb.NewFactCell(pkB, "Unrelated content", "src", "other", 0.5))
	}); err != nil {
		t.Fatal(err)
	}

	var results []hexxladb.CellSearchResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchCells(ctx, hexxladb.CellSearchConfig{Query: "science"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result for tag 'science'")
	}
	// Tag exact match should score 1.0 + confidence bonus.
	if results[0].Score < 1.0 {
		t.Errorf("score = %.2f, want ≥ 1.0 for exact tag match", results[0].Score)
	}
	if results[0].Cell.Coord != coordA {
		t.Errorf("top result coord = %v, want %v", results[0].Cell.Coord, coordA)
	}
}

func TestSearchCells_ContentSubstringMatch(t *testing.T) {
	t.Parallel()
	db := openSearchDB(t)
	ctx := context.Background()

	coord := hexxladb.Coord{Q: 0, R: 0}
	pk := mustPackTest(t, coord)

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(ctx, hexxladb.NewFactCell(pk, "The capital of France is Paris", "src", "geography", 0.8))
	}); err != nil {
		t.Fatal(err)
	}

	var results []hexxladb.CellSearchResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchCells(ctx, hexxladb.CellSearchConfig{Query: "france"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected match for 'france' in content")
	}
	// Case-insensitive content match scores 0.5 + confidence bonus.
	if results[0].Score < 0.5 {
		t.Errorf("score = %.2f, want ≥ 0.5 for content match", results[0].Score)
	}
}

func TestSearchCells_RequireTagsAND(t *testing.T) {
	t.Parallel()
	db := openSearchDB(t)
	ctx := context.Background()

	coordA := hexxladb.Coord{Q: 0, R: 0}
	coordB := hexxladb.Coord{Q: 1, R: 0}
	pkA := mustPackTest(t, coordA)
	pkB := mustPackTest(t, coordB)

	if err := db.Update(func(tx *hexxladb.Tx) error {
		// A has both "science" and "fact" tags (via NewFactCell).
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkA, "content a", "src", "science", 0.9)); err != nil {
			return err
		}
		// B only has "fact" and "other".
		return tx.PutCell(ctx, hexxladb.NewFactCell(pkB, "content b", "src", "other", 0.7))
	}); err != nil {
		t.Fatal(err)
	}

	var results []hexxladb.CellSearchResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchCells(ctx, hexxladb.CellSearchConfig{
			RequireTags: []string{"fact", "science"},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Errorf("got %d results, want 1 (only coordA has both tags)", len(results))
	}
	if len(results) > 0 && results[0].Cell.Coord != coordA {
		t.Errorf("result coord = %v, want %v", results[0].Cell.Coord, coordA)
	}
}

func TestSearchCells_AnyTagsOR(t *testing.T) {
	t.Parallel()
	db := openSearchDB(t)
	ctx := context.Background()

	coordA := hexxladb.Coord{Q: 0, R: 0}
	coordB := hexxladb.Coord{Q: 1, R: 0}
	coordC := hexxladb.Coord{Q: 2, R: 0}
	pkA := mustPackTest(t, coordA)
	pkB := mustPackTest(t, coordB)
	pkC := mustPackTest(t, coordC)

	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkA, "alpha", "src", "science", 0.9)); err != nil {
			return err
		}
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkB, "beta", "src", "history", 0.8)); err != nil {
			return err
		}
		return tx.PutCell(ctx, hexxladb.NewFactCell(pkC, "gamma", "src", "cooking", 0.7))
	}); err != nil {
		t.Fatal(err)
	}

	var results []hexxladb.CellSearchResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchCells(ctx, hexxladb.CellSearchConfig{
			AnyTags: []string{"science", "history"},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Errorf("got %d results, want 2 (science + history, not cooking)", len(results))
	}
}

func TestSearchCells_MinConfidenceFilter(t *testing.T) {
	t.Parallel()
	db := openSearchDB(t)
	ctx := context.Background()

	coordA := hexxladb.Coord{Q: 0, R: 0}
	coordB := hexxladb.Coord{Q: 1, R: 0}
	pkA := mustPackTest(t, coordA)
	pkB := mustPackTest(t, coordB)

	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkA, "high confidence", "src", "topic", 0.95)); err != nil {
			return err
		}
		return tx.PutCell(ctx, hexxladb.NewFactCell(pkB, "low confidence", "src", "topic", 0.3))
	}); err != nil {
		t.Fatal(err)
	}

	var results []hexxladb.CellSearchResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchCells(ctx, hexxladb.CellSearchConfig{
			MinConfidence: 0.8,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Errorf("got %d results, want 1 (only high-confidence cell)", len(results))
	}
	if len(results) > 0 && results[0].Cell.Coord != coordA {
		t.Errorf("result coord = %v, want coordA", results[0].Cell.Coord)
	}
}

func TestSearchCells_MaxResults(t *testing.T) {
	t.Parallel()
	db := openSearchDB(t)
	ctx := context.Background()

	// Write 5 cells, all with matching tag.
	coords := []hexxladb.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 2, R: 0}, {Q: 0, R: 1}, {Q: 1, R: 1}}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range coords {
			pk := mustPackTest(t, c)
			if err := tx.PutCell(ctx, hexxladb.NewFactCell(pk, "content", "src", "mytag", 0.8)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var results []hexxladb.CellSearchResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchCells(ctx, hexxladb.CellSearchConfig{
			Query:      "mytag",
			MaxResults: 3,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if len(results) > 3 {
		t.Errorf("got %d results, want ≤ 3 (MaxResults=3)", len(results))
	}
}

func TestSearchCells_SpatialRadiusFilter(t *testing.T) {
	t.Parallel()
	db := openSearchDB(t)
	ctx := context.Background()

	// Near origin.
	nearCoord := hexxladb.Coord{Q: 1, R: 0}
	// Far from origin.
	farCoord := hexxladb.Coord{Q: 10, R: 0}
	pkNear := mustPackTest(t, nearCoord)
	pkFar := mustPackTest(t, farCoord)

	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkNear, "near cell", "src", "topic", 0.9)); err != nil {
			return err
		}
		return tx.PutCell(ctx, hexxladb.NewFactCell(pkFar, "far cell", "src", "topic", 0.9))
	}); err != nil {
		t.Fatal(err)
	}

	var results []hexxladb.CellSearchResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchCells(ctx, hexxladb.CellSearchConfig{
			Center: hexxladb.Coord{},
			Radius: 3, // only cells within 3 rings of origin
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Cell.Coord == farCoord {
			t.Errorf("far cell (distance 10) appeared in results with Radius=3")
		}
	}
}

func TestSearchCells_SortedByScore(t *testing.T) {
	t.Parallel()
	db := openSearchDB(t)
	ctx := context.Background()

	// A: has "science" tag (exact match) → higher score.
	// B: "science" only in content → lower score.
	coordA := hexxladb.Coord{Q: 0, R: 0}
	coordB := hexxladb.Coord{Q: 1, R: 0}
	pkA := mustPackTest(t, coordA)
	pkB := mustPackTest(t, coordB)

	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkA, "general content", "src", "science", 0.9)); err != nil {
			return err
		}
		return tx.PutCell(ctx, hexxladb.NewFactCell(pkB, "science is interesting", "src", "other", 0.9))
	}); err != nil {
		t.Fatal(err)
	}

	var results []hexxladb.CellSearchResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchCells(ctx, hexxladb.CellSearchConfig{Query: "science", MaxResults: 10})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Score < results[1].Score {
		t.Errorf("results not sorted: results[0].Score=%.2f < results[1].Score=%.2f",
			results[0].Score, results[1].Score)
	}
	// Tag exact match (coordA) should rank above content match (coordB).
	if results[0].Cell.Coord != coordA {
		t.Errorf("top result = %v, want coordA (tag exact match ranks higher)", results[0].Cell.Coord)
	}
}
