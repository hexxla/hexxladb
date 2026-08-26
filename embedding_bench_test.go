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

// randomVec returns a random unit vector of the given dimension.
func randomVec(rng *rand.Rand, dim int) []float32 {
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

func setupBenchDB(b *testing.B, n, dim int) (*hexxladb.DB, []lattice.PackedCoord, [][]float32) {
	b.Helper()
	path := filepath.Join(b.TempDir(), "bench.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{
		EmbeddingDimension: uint16(dim),
		DistanceMetric:     hexxladb.DistanceCosine,
		PageSize:           4096,
		MaxValueBytes:      65536,
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	rng := rand.New(rand.NewPCG(42, 0))
	coords := make([]lattice.PackedCoord, n)
	vecs := make([][]float32, n)

	// Insert in batches of 500 to avoid huge transactions.
	batch := 500
	for start := 0; start < n; start += batch {
		end := min(start+batch, n)
		if err := db.Update(func(tx *hexxladb.Tx) error {
			for i := start; i < end; i++ {
				q := i / 1000
				r := i % 1000
				p, pErr := lattice.Pack(lattice.Coord{Q: q, R: r})
				if pErr != nil {
					return pErr
				}
				coords[i] = p
				vecs[i] = randomVec(rng, dim)
				if err := tx.PutCell(ctx, record.CellRecord{Key: p, RawContent: "bench"}); err != nil {
					return err
				}
				if err := tx.PutEmbedding(p, vecs[i]); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			b.Fatal(err)
		}
	}
	return db, coords, vecs
}

func BenchmarkSearchByEmbedding_HNSW(b *testing.B) {
	for _, tc := range []struct {
		name string
		n    int
		dim  int
	}{
		{"500_32d", 500, 32},
		{"200_64d", 200, 64},
		{"100_128d", 100, 128},
	} {
		b.Run(tc.name, func(b *testing.B) {
			db, _, vecs := setupBenchDB(b, tc.n, tc.dim)
			query := vecs[0]
			b.ResetTimer()
			for b.Loop() {
				if err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.SearchByEmbedding(query, hexxladb.EmbeddingSearchConfig{MaxResults: 10})
					return err
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkQueryCells_Embedding(b *testing.B) {
	db, _, vecs := setupBenchDB(b, 500, 32)
	query := vecs[0]
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if err := db.View(func(tx *hexxladb.Tx) error {
			_, err := tx.QueryCells(ctx, hexxladb.CellQuery{
				Embedding:  query,
				MaxResults: 10,
			})
			return err
		}); err != nil {
			b.Fatal(err)
		}
	}
}
