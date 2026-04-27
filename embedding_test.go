package hexxladb_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func mustPackEmb(t *testing.T, q, r int) lattice.PackedCoord {
	t.Helper()
	p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func openEmbeddingDB(t *testing.T, dim uint16, metric hexxladb.DistanceMetric) *hexxladb.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "emb.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{
		EmbeddingDimension: dim,
		DistanceMetric:     metric,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestEmbedding_PutGetRoundTrip(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)
	coord := mustPackEmb(t, 1, 2)
	want := []float32{0.1, 0.2, 0.3}

	err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutEmbedding(coord, want)
	})
	if err != nil {
		t.Fatalf("PutEmbedding: %v", err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetEmbedding(coord)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("expected embedding, got not-found")
		}
		if len(got) != len(want) {
			t.Fatalf("len: want %d, got %d", len(want), len(got))
		}
		for i := range want {
			if math.Abs(float64(got[i]-want[i])) > 1e-7 {
				t.Fatalf("vec[%d]: want %f, got %f", i, want[i], got[i])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("View: %v", err)
	}
}

func TestEmbedding_GetMissing(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)
	coord := mustPackEmb(t, 10, 20)

	err := db.View(func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetEmbedding(coord)
		if err != nil {
			return err
		}
		if ok {
			t.Fatal("expected not-found")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("View: %v", err)
	}
}

func TestEmbedding_DeleteRemovesEmbedding(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)
	coord := mustPackEmb(t, 1, 2)

	err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutEmbedding(coord, []float32{1, 2, 3}); err != nil {
			return err
		}
		return tx.DeleteEmbedding(coord)
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetEmbedding(coord)
		if err != nil {
			return err
		}
		if ok {
			t.Fatal("expected not-found after delete")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEmbedding_DimensionMismatch(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)
	coord := mustPackEmb(t, 1, 2)

	err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutEmbedding(coord, []float32{1, 2}) // wrong dim
	})
	if !errors.Is(err, hexxladb.ErrEmbeddingDimension) {
		t.Fatalf("expected ErrEmbeddingDimension, got %v", err)
	}
}

func TestEmbedding_DisabledDB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "no_emb.db")
	db, err := hexxladb.Open(path, nil) // no embedding dim
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	coord := mustPackEmb(t, 1, 2)
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutEmbedding(coord, []float32{1, 2, 3})
	})
	if !errors.Is(err, hexxladb.ErrEmbeddingsDisabled) {
		t.Fatalf("expected ErrEmbeddingsDisabled, got %v", err)
	}
}

func TestEmbedding_DeleteCellCascade(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)
	coord := mustPackEmb(t, 1, 2)
	ctx := context.Background()

	err := db.Update(func(tx *hexxladb.Tx) error {
		rec := record.CellRecord{
			Key:        coord,
			RawContent: "hello",
		}
		if err := tx.PutCell(ctx, rec); err != nil {
			return err
		}
		return tx.PutEmbedding(coord, []float32{0.1, 0.2, 0.3})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify embedding exists.
	err = db.View(func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetEmbedding(coord)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("expected embedding before delete")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Delete cell — should cascade to embedding.
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, coord)
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetEmbedding(coord)
		if err != nil {
			return err
		}
		if ok {
			t.Fatal("expected embedding removed after DeleteCell cascade")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEmbedding_SearchByEmbedding(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)

	// Insert 5 embeddings.
	vecs := [][]float32{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
		{0.9, 0.1, 0},
		{0.8, 0.2, 0},
	}
	coords := make([]lattice.PackedCoord, len(vecs))
	for i := range vecs {
		coords[i] = mustPackEmb(t, i+1, 0)
	}
	err := db.Update(func(tx *hexxladb.Tx) error {
		for i, v := range vecs {
			if err := tx.PutEmbedding(coords[i], v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Search for vector closest to [1,0,0].
	query := []float32{1, 0, 0}
	var results []hexxladb.EmbeddingSearchResult
	err = db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchByEmbedding(query, hexxladb.EmbeddingSearchConfig{MaxResults: 3})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// First result should be coord[0] (exact match).
	if results[0].Coord != coords[0] {
		t.Fatalf("top result: want coord %v, got %v", coords[0], results[0].Coord)
	}
	if math.Abs(results[0].Score-1.0) > 1e-6 {
		t.Fatalf("top score: want ~1.0, got %f", results[0].Score)
	}
	// Scores should be descending.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score+1e-9 {
			t.Fatalf("results not descending at %d: %f > %f", i, results[i].Score, results[i-1].Score)
		}
	}
}

func TestEmbedding_SearchByEmbedding_EmptyDB(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)

	err := db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.SearchByEmbedding([]float32{1, 0, 0}, hexxladb.EmbeddingSearchConfig{})
		if err != nil {
			return err
		}
		if results != nil {
			t.Fatalf("expected nil results, got %d", len(results))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEmbedding_SearchByEmbedding_MinScore(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)

	err := db.Update(func(tx *hexxladb.Tx) error {
		// Identical to query.
		if err := tx.PutEmbedding(mustPackEmb(t, 1, 0), []float32{1, 0, 0}); err != nil {
			return err
		}
		// Orthogonal to query (cosine ~ 0).
		return tx.PutEmbedding(mustPackEmb(t, 2, 0), []float32{0, 1, 0})
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.SearchByEmbedding([]float32{1, 0, 0}, hexxladb.EmbeddingSearchConfig{
			MaxResults: 10,
			MinScore:   0.5,
		})
		if err != nil {
			return err
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result above minScore, got %d", len(results))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEmbedding_ReindexEmbeddings(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)
	ctx := context.Background()
	coord1 := mustPackEmb(t, 1, 0)
	coord2 := mustPackEmb(t, 2, 0)

	// Insert cells and initial embeddings.
	err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, record.CellRecord{Key: coord1, RawContent: "a"}); err != nil {
			return err
		}
		if err := tx.PutCell(ctx, record.CellRecord{Key: coord2, RawContent: "b"}); err != nil {
			return err
		}
		if err := tx.PutEmbedding(coord1, []float32{1, 0, 0}); err != nil {
			return err
		}
		return tx.PutEmbedding(coord2, []float32{0, 1, 0})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Reindex with new embeddings.
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.ReindexEmbeddings(ctx, func(_ context.Context, rec record.CellRecord) ([]float32, error) {
			if string(rec.RawContent) == "a" {
				return []float32{0, 0, 1}, nil // changed
			}
			return []float32{0, 1, 0}, nil // unchanged
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify updated embedding.
	err = db.View(func(tx *hexxladb.Tx) error {
		vec, ok, err := tx.GetEmbedding(coord1)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("expected embedding for coord1")
		}
		if math.Abs(float64(vec[2]-1.0)) > 1e-7 {
			t.Fatalf("expected reindexed vec[2]=1.0, got %f", vec[2])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEmbedding_ReindexSkipRemoves(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)
	ctx := context.Background()
	coord := mustPackEmb(t, 1, 0)

	err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, record.CellRecord{Key: coord, RawContent: "x"}); err != nil {
			return err
		}
		return tx.PutEmbedding(coord, []float32{1, 0, 0})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Reindex returning nil — should remove the embedding.
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.ReindexEmbeddings(ctx, func(_ context.Context, _ record.CellRecord) ([]float32, error) {
			return nil, nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetEmbedding(coord)
		if err != nil {
			return err
		}
		if ok {
			t.Fatal("expected embedding removed after reindex skip")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEmbedding_DBAccessors(t *testing.T) {
	t.Parallel()
	db := openEmbeddingDB(t, 384, hexxladb.DistanceL2)
	if db.EmbeddingDimension() != 384 {
		t.Fatalf("EmbeddingDimension: want 384, got %d", db.EmbeddingDimension())
	}
	if db.EmbeddingMetric() != hexxladb.DistanceL2 {
		t.Fatalf("EmbeddingMetric: want L2, got %d", db.EmbeddingMetric())
	}
}

func TestEmbedding_PersistedAcrossReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	// Create DB with embeddings.
	db, err := hexxladb.Open(path, &hexxladb.Options{
		EmbeddingDimension: 3,
		DistanceMetric:     hexxladb.DistanceDotProduct,
	})
	if err != nil {
		t.Fatal(err)
	}
	coord := mustPackEmb(t, 1, 2)
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutEmbedding(coord, []float32{0.5, 0.6, 0.7})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen and verify.
	db2, err := hexxladb.Open(path, &hexxladb.Options{
		EmbeddingDimension: 3,
		DistanceMetric:     hexxladb.DistanceDotProduct,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db2.Close() }()

	if db2.EmbeddingDimension() != 3 {
		t.Fatalf("after reopen: EmbeddingDimension: want 3, got %d", db2.EmbeddingDimension())
	}
	if db2.EmbeddingMetric() != hexxladb.DistanceDotProduct {
		t.Fatalf("after reopen: EmbeddingMetric: want DotProduct, got %d", db2.EmbeddingMetric())
	}
	err = db2.View(func(tx *hexxladb.Tx) error {
		vec, ok, err := tx.GetEmbedding(coord)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("expected embedding after reopen")
		}
		if math.Abs(float64(vec[0]-0.5)) > 1e-7 {
			t.Fatalf("vec[0]: want 0.5, got %f", vec[0])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEmbedding_DimensionMismatchOnReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "mismatch.db")

	db, err := hexxladb.Open(path, &hexxladb.Options{EmbeddingDimension: 128})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = hexxladb.Open(path, &hexxladb.Options{EmbeddingDimension: 256})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}
