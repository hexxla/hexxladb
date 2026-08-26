package hexxladb_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
)

func TestQueryCells_PlannerPrefersBoundedSpatialProbe(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)
	for q := -2; q <= 2; q++ {
		for r := -2; r <= 2; r++ {
			putQueryCell(t, db, hexxladb.Coord{Q: q, R: r}, "cell", "shared", nil, 1, time.Time{})
		}
	}

	var results []hexxladb.CellQueryResult
	err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.QueryCells(t.Context(), hexxladb.CellQuery{
			SourceID:    "shared",
			Center:      hexxladb.Coord{},
			Radius:      1,
			MaxScanRows: 7,
		})
		return err
	})
	if err != nil {
		t.Fatalf("QueryCells error = %v, want complete bounded spatial plan", err)
	}
	if len(results) != 7 {
		t.Fatalf("results = %d, want 7", len(results))
	}
}

func TestQueryCells_FilteredEmbeddingWidensCandidates(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "filtered-ann.db"), &hexxladb.Options{
		EmbeddingDimension: 2,
		DistanceMetric:     hexxladb.DistanceDotProduct,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	err = db.Update(func(tx *hexxladb.Tx) error {
		for i := range 60 {
			coord := hexxladb.Coord{Q: i, R: 0}
			key, err := hexxladb.Pack(coord)
			if err != nil {
				return err
			}
			tags := []string{"skip"}
			if i == 50 {
				tags = []string{"keep"}
			}
			if err := tx.PutCell(t.Context(), hexxladb.CellRecord{Key: key, RawContent: "candidate", Tags: tags}); err != nil {
				return err
			}
			if err := tx.PutEmbeddingWithOptions(key, []float32{float32(100 - i), 0}, hexxladb.EmbeddingWriteOptions{DeferIndexMaintenance: true}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	query := hexxladb.CellQuery{
		Embedding:   []float32{1, 0},
		RequireTags: []string{"keep"},
		MaxResults:  1,
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(t.Context(), query)
		if err != nil {
			return err
		}
		if len(results) != 1 || results[0].Cell.Coord != (hexxladb.Coord{Q: 50, R: 0}) {
			t.Fatalf("results = %+v, want filtered candidate at (50,0)", results)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	query.EmbeddingCandidateLimit = 40
	err = db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(t.Context(), query)
		if err != nil {
			return err
		}
		if len(results) != 0 {
			t.Fatalf("limited results = %+v, want no match within first 40 candidates", results)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func openQueryDB(t *testing.T) *hexxladb.DB {
	t.Helper()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "query.db"), nil)
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

// putQueryCell writes a cell with explicit ValidFrom so temporal tests work.
func putQueryCell(t *testing.T, db *hexxladb.DB, coord hexxladb.Coord, content, source string, tags []string, confidence float64, validFrom time.Time) {
	t.Helper()
	pk := mustPackTest(t, coord)
	ctx := context.Background()
	if err := db.Update(func(tx *hexxladb.Tx) error {
		rec := hexxladb.NewFactCell(pk, content, source, "topic", confidence)
		rec.Tags = tags
		if !validFrom.IsZero() {
			ns := validFrom.UnixNano()
			rec.Validity.ValidFrom = &ns
		}
		return tx.PutCell(ctx, rec)
	}); err != nil {
		t.Fatal(err)
	}
}

// ── basic ────────────────────────────────────────────────────────────────────

func TestQueryCells_EmptyDB(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)
	ctx := context.Background()

	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{})
		if err != nil {
			return err
		}
		if len(results) != 0 {
			t.Errorf("empty DB returned %d results, want 0", len(results))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_RequireTagsAND(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "both tags", "src", []string{"alpha", "beta"}, 0.9, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "only alpha", "src", []string{"alpha"}, 0.9, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			RequireTags: []string{"alpha", "beta"},
		})
		if err != nil {
			return err
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (only cell with both tags)", len(results))
		}
		if len(results) > 0 && results[0].Cell.RawContent != "both tags" {
			t.Errorf("wrong cell returned: %s", results[0].Cell.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_AnyTagsOR(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "has alpha", "src", []string{"alpha"}, 0.9, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "has beta", "src", []string{"beta"}, 0.9, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 2, R: 0}, "has gamma", "src", []string{"gamma"}, 0.9, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			AnyTags:    []string{"alpha", "beta"},
			MaxResults: 10,
		})
		if err != nil {
			return err
		}
		if len(results) != 2 {
			t.Errorf("got %d results, want 2", len(results))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_ExcludeTags(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "keep this", "src", []string{"fact"}, 0.9, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "exclude this", "src", []string{"fact", "draft"}, 0.9, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			RequireTags: []string{"fact"},
			ExcludeTags: []string{"draft"},
			MaxResults:  10,
		})
		if err != nil {
			return err
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (draft cell excluded)", len(results))
		}
		if len(results) > 0 && results[0].Cell.RawContent != "keep this" {
			t.Errorf("wrong cell: %s", results[0].Cell.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_ContentQuery(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "the database is spatial", "src", []string{"fact"}, 0.9, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "the weather is fine", "src", []string{"fact"}, 0.9, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			Query:      "database",
			MaxResults: 10,
		})
		if err != nil {
			return err
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1", len(results))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_FallbackScansCompleteKeyspace(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)
	putQueryCell(t, db, hexxladb.Coord{Q: 100, R: 0}, "far-away needle", "src", nil, 0.9, time.Time{})

	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(t.Context(), hexxladb.CellQuery{Query: "needle"})
		if err != nil {
			return err
		}
		if len(results) != 1 || results[0].Cell.Coord != (hexxladb.Coord{Q: 100, R: 0}) {
			t.Fatalf("complete fallback scan: got %#v", results)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_ZeroMaxResultsIsUnlimited(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)
	for i := range 25 {
		putQueryCell(t, db, hexxladb.Coord{Q: 100 + i, R: 0}, "all", "src", nil, 0.9, time.Time{})
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(t.Context(), hexxladb.CellQuery{Query: "all"})
		if err != nil {
			return err
		}
		if len(results) != 25 {
			t.Fatalf("MaxResults=0: got %d results, want 25", len(results))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_RejectsUnpackableRadius(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	err := db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.QueryCells(t.Context(), hexxladb.CellQuery{
			Center: hexxladb.Coord{Q: hexxladb.MaxAxialAbs, R: 0},
			Radius: 1,
		})
		return err
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("QueryCells boundary error = %v, want ErrInvalidArgument", err)
	}
}

func TestQueryCells_PreEpochTimeRange(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)
	valid := time.Unix(-8*24*60*60, 0).UTC()
	putQueryCell(t, db, hexxladb.Coord{}, "pre-epoch", "src", nil, 0.9, valid)

	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(t.Context(), hexxladb.CellQuery{
			After:  time.Unix(-9*24*60*60, 0).UTC(),
			Before: time.Unix(-7*24*60*60, 0).UTC(),
		})
		if err != nil {
			return err
		}
		if len(results) != 1 || results[0].Cell.RawContent != "pre-epoch" {
			t.Fatalf("pre-epoch query: got %#v", results)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_SourceID(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "from session-A", "session-A", []string{"msg"}, 0.9, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "from session-B", "session-B", []string{"msg"}, 0.9, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			SourceID:   "session-A",
			MaxResults: 10,
		})
		if err != nil {
			return err
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1", len(results))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_ConfidenceRange(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "high conf", "src", []string{"f"}, 0.9, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "mid conf", "src", []string{"f"}, 0.5, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 2, R: 0}, "low conf", "src", []string{"f"}, 0.2, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			MinConfidence: 0.4,
			MaxConfidence: 0.8,
			MaxResults:    10,
		})
		if err != nil {
			return err
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (mid conf only)", len(results))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_SpatialRadius(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	center := hexxladb.Coord{Q: 0, R: 0}
	near := hexxladb.Coord{Q: 1, R: 0} // distance 1
	far := hexxladb.Coord{Q: 5, R: 0}  // distance 5

	for _, c := range []hexxladb.Coord{center, near, far} {
		putQueryCell(t, db, c, "content", "src", []string{"x"}, 0.8, time.Time{})
	}

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			Center:     center,
			Radius:     2,
			MaxResults: 10,
		})
		if err != nil {
			return err
		}
		if len(results) != 2 {
			t.Errorf("got %d results, want 2 (center + near)", len(results))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// ── temporal ──────────────────────────────────────────────────────────────────

func TestQueryCells_TemporalAfter(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := base.Add(-30 * 24 * time.Hour)
	recent := base.Add(1 * time.Hour)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "old cell", "src", []string{"t"}, 0.9, old)
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "recent cell", "src", []string{"t"}, 0.9, recent)

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			After:      base,
			MaxResults: 10,
		})
		if err != nil {
			return err
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (recent only)", len(results))
		}
		if len(results) > 0 && results[0].Cell.RawContent != "recent cell" {
			t.Errorf("wrong cell: %s", results[0].Cell.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_TemporalBefore(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := base.Add(-30 * 24 * time.Hour)
	recent := base.Add(1 * time.Hour)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "old cell", "src", []string{"t"}, 0.9, old)
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "recent cell", "src", []string{"t"}, 0.9, recent)

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			Before:     base,
			MaxResults: 10,
		})
		if err != nil {
			return err
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (old only)", len(results))
		}
		if len(results) > 0 && results[0].Cell.RawContent != "old cell" {
			t.Errorf("wrong cell: %s", results[0].Cell.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_TemporalRange(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := t0.Add(-30 * 24 * time.Hour)
	inWindow := t0.Add(3 * 24 * time.Hour)
	future := t0.Add(30 * 24 * time.Hour)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "old", "src", []string{"t"}, 0.9, old)
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "in window", "src", []string{"t"}, 0.9, inWindow)
	putQueryCell(t, db, hexxladb.Coord{Q: 2, R: 0}, "future", "src", []string{"t"}, 0.9, future)

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			After:      t0,
			Before:     t0.Add(7 * 24 * time.Hour),
			MaxResults: 10,
		})
		if err != nil {
			return err
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1 (in-window only)", len(results))
		}
		if len(results) > 0 && results[0].Cell.RawContent != "in window" {
			t.Errorf("wrong cell: %s", results[0].Cell.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_NoValidFrom_ExcludedFromTemporalQuery(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	// Cell with no ValidFrom should be excluded from any temporal query.
	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "no time", "src", []string{"t"}, 0.9, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			After:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			MaxResults: 10,
		})
		if err != nil {
			return err
		}
		if len(results) != 0 {
			t.Errorf("got %d results, want 0 (no ValidFrom cells excluded)", len(results))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// ── sort orders ───────────────────────────────────────────────────────────────

func TestQueryCells_SortByConfidence(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "low", "src", []string{"x"}, 0.3, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "high", "src", []string{"x"}, 0.9, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 2, R: 0}, "mid", "src", []string{"x"}, 0.6, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			MaxResults: 10,
			SortBy:     hexxladb.SortByConfidence,
		})
		if err != nil {
			return err
		}
		if len(results) < 3 {
			t.Fatalf("got %d results, want 3", len(results))
		}
		if results[0].Cell.RawContent != "high" {
			t.Errorf("first result = %q, want 'high'", results[0].Cell.RawContent)
		}
		if results[2].Cell.RawContent != "low" {
			t.Errorf("last result = %q, want 'low'", results[2].Cell.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_SortByRecency(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "oldest", "src", []string{"x"}, 0.8, base.Add(-2*24*time.Hour))
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "newest", "src", []string{"x"}, 0.8, base.Add(2*24*time.Hour))
	putQueryCell(t, db, hexxladb.Coord{Q: 2, R: 0}, "middle", "src", []string{"x"}, 0.8, base)

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			MaxResults: 10,
			SortBy:     hexxladb.SortByRecency,
		})
		if err != nil {
			return err
		}
		if len(results) < 3 {
			t.Fatalf("got %d results, want 3", len(results))
		}
		if results[0].Cell.RawContent != "newest" {
			t.Errorf("first = %q, want 'newest'", results[0].Cell.RawContent)
		}
		if results[2].Cell.RawContent != "oldest" {
			t.Errorf("last = %q, want 'oldest'", results[2].Cell.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_SortByCoord(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	putQueryCell(t, db, hexxladb.Coord{Q: 3, R: 0}, "c", "src", []string{"x"}, 0.8, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "a", "src", []string{"x"}, 0.8, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 2, R: 0}, "b", "src", []string{"x"}, 0.8, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			MaxResults: 10,
			SortBy:     hexxladb.SortByCoord,
		})
		if err != nil {
			return err
		}
		if len(results) < 3 {
			t.Fatalf("got %d results, want 3", len(results))
		}
		if results[0].Cell.RawContent != "a" {
			t.Errorf("first = %q, want 'a'", results[0].Cell.RawContent)
		}
		if results[2].Cell.RawContent != "c" {
			t.Errorf("last = %q, want 'c'", results[2].Cell.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// ── explain mode ──────────────────────────────────────────────────────────────

func TestQueryCells_Explain(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "database facts", "src", []string{"fact"}, 0.9, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			Query:      "database",
			Explain:    true,
			MaxResults: 5,
		})
		if err != nil {
			return err
		}
		if len(results) == 0 {
			t.Fatal("no results")
		}
		if results[0].Explanation == "" {
			t.Error("Explanation is empty, want non-empty when Explain=true")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// ── combined predicates ───────────────────────────────────────────────────────

func TestQueryCells_TagAndTemporalAndConfidence(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	// Matches all three conditions.
	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "pass", "src", []string{"fact"}, 0.9, base.Add(1*time.Hour))
	// Wrong tag.
	putQueryCell(t, db, hexxladb.Coord{Q: 1, R: 0}, "fail-tag", "src", []string{"draft"}, 0.9, base.Add(1*time.Hour))
	// Too old.
	putQueryCell(t, db, hexxladb.Coord{Q: 2, R: 0}, "fail-time", "src", []string{"fact"}, 0.9, base.Add(-10*24*time.Hour))
	// Low confidence.
	putQueryCell(t, db, hexxladb.Coord{Q: 3, R: 0}, "fail-conf", "src", []string{"fact"}, 0.3, base.Add(1*time.Hour))

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
			RequireTags:   []string{"fact"},
			After:         base,
			MinConfidence: 0.7,
			MaxResults:    10,
		})
		if err != nil {
			return err
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1", len(results))
		}
		if len(results) > 0 && results[0].Cell.RawContent != "pass" {
			t.Errorf("wrong cell: %s", results[0].Cell.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Ensure existing SearchCells tests still pass (backward compat via wrapper).
func TestSearchCells_StillWorks(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)

	putQueryCell(t, db, hexxladb.Coord{Q: 0, R: 0}, "database search", "src", []string{"fact"}, 0.9, time.Time{})

	ctx := context.Background()
	if err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.SearchCells(ctx, hexxladb.CellSearchConfig{Query: "database"})
		if err != nil {
			return err
		}
		if len(results) == 0 {
			t.Error("SearchCells returned 0 results after refactor")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCells_MaxScanRowsReturnsPartialResultsAndSignal(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)
	putQueryCell(t, db, hexxladb.Coord{Q: 0}, "first", "src", []string{"alpha"}, 1, time.Time{})
	putQueryCell(t, db, hexxladb.Coord{Q: 1}, "second", "src", []string{"alpha"}, 1, time.Time{})

	var results []hexxladb.CellQueryResult
	err := db.View(func(tx *hexxladb.Tx) error {
		var queryErr error
		results, queryErr = tx.QueryCells(t.Context(), hexxladb.CellQuery{
			RequireTags: []string{"alpha"},
			MaxScanRows: 1,
		})
		return queryErr
	})
	if !errors.Is(err, hexxladb.ErrQueryScanLimit) {
		t.Fatalf("QueryCells error = %v, want ErrQueryScanLimit", err)
	}
	if len(results) != 1 {
		t.Fatalf("partial results = %d, want 1", len(results))
	}
}

func TestQueryCells_MaxScanRowsCountsPhysicalMVCCRows(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "query-mvcc.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, content := range []string{"one", "two", "three"} {
		putQueryCell(t, db, hexxladb.Coord{}, content, "src", nil, 1, time.Time{})
	}

	var results []hexxladb.CellQueryResult
	err = db.View(func(tx *hexxladb.Tx) error {
		var queryErr error
		results, queryErr = tx.QueryCells(t.Context(), hexxladb.CellQuery{MaxScanRows: 2})
		return queryErr
	})
	if !errors.Is(err, hexxladb.ErrQueryScanLimit) {
		t.Fatalf("QueryCells error = %v, want ErrQueryScanLimit", err)
	}
	if len(results) != 1 || results[0].Cell.RawContent != "three" {
		t.Fatalf("partial MVCC results = %#v, want latest logical cell", results)
	}
}

func TestQueryCells_MaxScanRowsExactBoundaryIsComplete(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)
	putQueryCell(t, db, hexxladb.Coord{}, "only", "src", []string{"alpha"}, 1, time.Time{})

	tests := map[string]hexxladb.CellQuery{
		"full":   {MaxScanRows: 1},
		"source": {SourceID: "src", MaxScanRows: 1},
		"tag":    {RequireTags: []string{"alpha"}, MaxScanRows: 1},
		"radius": {Center: hexxladb.Coord{}, Radius: 1, MaxScanRows: 7},
	}
	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			err := db.View(func(tx *hexxladb.Tx) error {
				results, err := tx.QueryCells(t.Context(), query)
				if err != nil {
					return err
				}
				if len(results) != 1 || results[0].Cell.RawContent != "only" {
					t.Fatalf("results = %#v, want the only row", results)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("exact-boundary QueryCells error = %v, want nil", err)
			}
		})
	}
}

func TestQueryCells_MaxScanRowsValidation(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)
	for name, query := range map[string]hexxladb.CellQuery{
		"negative":  {MaxScanRows: -1},
		"embedding": {Embedding: []float32{1}, MaxScanRows: 1},
	} {
		t.Run(name, func(t *testing.T) {
			err := db.View(func(tx *hexxladb.Tx) error {
				_, err := tx.QueryCells(t.Context(), query)
				return err
			})
			if !errors.Is(err, hexxladb.ErrInvalidArgument) {
				t.Fatalf("QueryCells error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}
