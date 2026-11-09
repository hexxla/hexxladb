package hexxladb

import (
	"errors"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
)

func TestEmbeddingInsertDetectsLostReachability(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "vectors.db"), &Options{EmbeddingDimension: 8, DistanceMetric: DistanceDotProduct})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rng := rand.New(rand.NewPCG(2, 43))
	target, err := Pack(Coord{Q: 37})
	if err != nil {
		t.Fatal(err)
	}
	var query []float32
	if err := db.Update(func(tx *Tx) error {
		for i := range 450 {
			vector := make([]float32, 8)
			for j := range vector {
				vector[j] = float32(rng.NormFloat64())
			}
			coord, err := Pack(Coord{Q: i + 1})
			if err != nil {
				return err
			}
			if coord == target {
				query = vector
			}
			if err := tx.PutEmbedding(coord, vector); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *Tx) error {
		hits, stats, err := tx.SearchByEmbeddingWithStats(t.Context(), query, EmbeddingSearchConfig{MaxResults: 40, EfSearch: 1000})
		if err != nil {
			return err
		}
		rank := slices.IndexFunc(hits, func(hit EmbeddingSearchResult) bool { return hit.Coord == target }) + 1
		if stats.Path != EmbeddingSearchPathFlat || rank != 29 {
			t.Fatalf("path %q, target rank %d, want flat search with rank 29", stats.Path, rank)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddingLegacyGraphUsesExactUntilRebuilt(t *testing.T) {
	for _, state := range []string{"missing", "v1"} {
		t.Run(state, func(t *testing.T) {
			db, _ := openInternalRebuildDB(t)
			if err := db.Update(func(tx *Tx) error {
				if state == "missing" {
					return tx.deleteDirect([]byte(index.HNSWStateKey))
				}
				old := make([]byte, embeddingIndexStateSize)
				old[0] = 1
				return tx.putDirect([]byte(index.HNSWStateKey), old)
			}); err != nil {
				t.Fatal(err)
			}
			path := db.eng.Path()
			reopen := func() {
				t.Helper()
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := OpenExisting(path, nil)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = reopened.Close() })
				db = reopened
			}
			reopen()
			assertPath := func(want EmbeddingSearchPath) {
				t.Helper()
				if err := db.View(func(tx *Tx) error {
					_, stats, err := tx.SearchByEmbeddingWithStats(t.Context(), []float32{1, 0, 0}, EmbeddingSearchConfig{MaxResults: 1})
					if err == nil && stats.Path != want {
						t.Fatalf("search path %q, want %q", stats.Path, want)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			assertPath(EmbeddingSearchPathFlat)
			if _, err := db.RebuildEmbeddingIndex(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			reopen()
			assertPath(EmbeddingSearchPathHNSW)
		})
	}
}

func TestEmbeddingRebuildSnapshotRechecksMemory(t *testing.T) {
	db, _ := openInternalRebuildDB(t)
	limits, err := db.preflightEmbeddingIndexRebuild(t.Context(), &EmbeddingIndexRebuildOptions{MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *Tx) error {
		for i := 2; i <= 1200; i++ {
			coord, err := Pack(Coord{Q: i})
			if err != nil {
				return err
			}
			if err := tx.PutEmbeddingWithOptions(coord, []float32{1, 0, 0}, EmbeddingWriteOptions{DeferIndexMaintenance: true}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.markEmbeddingIndexDirty(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := db.snapshotEmbeddingsForRebuild(t.Context(), limits); !errors.Is(err, ErrEmbeddingIndexTooLarge) {
		t.Fatalf("snapshot error = %v, want ErrEmbeddingIndexTooLarge", err)
	}
}
