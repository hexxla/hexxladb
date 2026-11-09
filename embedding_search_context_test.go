package hexxladb_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/hexxla/hexxladb"
)

// embeddingCancellationContext cancels a real context after several successful
// cancellation checks, placing cancellation inside the scan or graph traversal.
type embeddingCancellationContext struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int32
}

func (c *embeddingCancellationContext) Err() error {
	if c.checks.Add(1) == 8 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestEmbeddingSearchCancelsInFlight(t *testing.T) {
	for _, path := range []hexxladb.EmbeddingSearchPath{hexxladb.EmbeddingSearchPathFlat, hexxladb.EmbeddingSearchPathHNSW} {
		t.Run(string(path), func(t *testing.T) {
			db := openEmbeddingDB(t, 3, hexxladb.DistanceCosine)
			if err := db.Update(func(tx *hexxladb.Tx) error {
				for i := range 64 {
					coord := mustPackEmb(t, i, 0)
					if err := tx.PutEmbeddingWithOptions(coord, []float32{1, float32(i + 1), 1}, hexxladb.EmbeddingWriteOptions{DeferIndexMaintenance: true}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if path == hexxladb.EmbeddingSearchPathHNSW {
				if _, err := db.RebuildEmbeddingIndex(t.Context(), nil); err != nil {
					t.Fatal(err)
				}
			}
			query := []float32{1, 1, 1}
			cfg := hexxladb.EmbeddingSearchConfig{MaxResults: 10}
			if err := db.View(func(tx *hexxladb.Tx) error {
				_, stats, err := tx.SearchByEmbeddingWithStats(t.Context(), query, cfg)
				if err == nil && stats.Path != path {
					t.Fatalf("search path = %q, want %q", stats.Path, path)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for _, method := range []struct {
				name      string
				withStats bool
			}{{name: "results"}, {name: "with_stats", withStats: true}} {
				t.Run(method.name, func(t *testing.T) {
					base, cancel := context.WithCancel(t.Context())
					defer cancel()
					ctx := &embeddingCancellationContext{Context: base, cancel: cancel}
					err := db.View(func(tx *hexxladb.Tx) error {
						var hits []hexxladb.EmbeddingSearchResult
						var err error
						if method.withStats {
							hits, _, err = tx.SearchByEmbeddingWithStats(ctx, query, cfg)
						} else {
							hits, err = tx.SearchByEmbedding(ctx, query, cfg)
						}
						if hits != nil {
							t.Errorf("canceled search returned partial results: %+v", hits)
						}
						return err
					})
					if !errors.Is(err, context.Canceled) || ctx.checks.Load() < 8 {
						t.Fatalf("in-flight search: error=%v, cancellation checks=%d", err, ctx.checks.Load())
					}
				})
			}
		})
	}
}
