package hexxladb_test

import (
	"context"
	"math"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// regressionVec returns a random unit vector of the given dimension.
func regressionVec(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	var norm float64
	for i := range v {
		f := rng.NormFloat64()
		v[i] = float32(f)
		norm += f * f
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] /= float32(norm)
	}
	return v
}

// TestPutEmbedding_HighCount_32d is a regression test for the B+ tree
// leaf-page-full error that occurs when inserting >500 embeddings at 32
// dimensions on a default (4096-byte) page size. The HNSW graph accumulates
// dense layer-0 neighbor lists that, combined with the embed/ values, trigger
// the leaf split path with entries large enough to overflow the right half.
func TestPutEmbedding_HighCount_32d(t *testing.T) {
	t.Parallel()
	const dim = 32
	const n = 600

	path := filepath.Join(t.TempDir(), "regression_32d.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{
		EmbeddingDimension: dim,
		DistanceMetric:     hexxladb.DistanceCosine,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	rng := rand.New(rand.NewPCG(42, 7))

	for i := range n {
		q := i / 100
		r := i % 100
		pk, err := lattice.Pack(lattice.Coord{Q: q, R: r})
		if err != nil {
			t.Fatal(err)
		}
		vec := regressionVec(rng, dim)
		if err := db.Update(func(tx *hexxladb.Tx) error {
			if err := tx.PutCell(ctx, record.CellRecord{Key: pk, RawContent: "x"}); err != nil {
				return err
			}
			return tx.PutEmbedding(pk, vec)
		}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
}

// TestPutEmbedding_HighCount_128d is the same regression test at 128 dimensions
// where the failure was reported at >100 entries.
func TestPutEmbedding_HighCount_128d(t *testing.T) {
	t.Parallel()
	const dim = 128
	const n = 150

	path := filepath.Join(t.TempDir(), "regression_128d.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{
		EmbeddingDimension: dim,
		DistanceMetric:     hexxladb.DistanceCosine,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	rng := rand.New(rand.NewPCG(42, 13))

	for i := range n {
		q := i / 50
		r := i % 50
		pk, err := lattice.Pack(lattice.Coord{Q: q, R: r})
		if err != nil {
			t.Fatal(err)
		}
		vec := regressionVec(rng, dim)
		if err := db.Update(func(tx *hexxladb.Tx) error {
			if err := tx.PutCell(ctx, record.CellRecord{Key: pk, RawContent: "x"}); err != nil {
				return err
			}
			return tx.PutEmbedding(pk, vec)
		}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
}
