package hexxladb

import (
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestPersistedEmbeddingCorruptionReturnsError(t *testing.T) {
	tests := map[string][]byte{
		"truncated":  {1, 2, 3},
		"oversized":  encodeFloat32s([]float32{1, 2, 3}),
		"not-finite": encodeFloat32s([]float32{1, float32(math.NaN())}),
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.db")
			options := &Options{
				EmbeddingDimension: 2,
				DistanceMetric:     DistanceCosine,
			}
			db, err := Open(path, options)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })

			coord, err := lattice.Pack(lattice.Coord{Q: 1, R: 2})
			if err != nil {
				t.Fatalf("Pack: %v", err)
			}
			if err := db.Update(func(tx *Tx) error {
				return tx.putDirect(index.EmbedKey(coord), encoded)
			}); err != nil {
				t.Fatalf("store corrupt embedding: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close before corruption read: %v", err)
			}
			db, err = Open(path, options)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}

			assertCorruptDatabase(t, db, func(tx *Tx) error {
				_, _, err := tx.GetEmbedding(coord)
				return err
			})
			assertCorruptDatabase(t, db, func(tx *Tx) error {
				_, err := tx.SearchByEmbedding([]float32{1, 2}, EmbeddingSearchConfig{})
				return err
			})
			assertCorruptDatabase(t, db, func(tx *Tx) error {
				_, _, err := (&txHNSWStorage{tx: tx}).GetEmbeddingVec(coord)
				return err
			})
		})
	}
}

func assertCorruptDatabase(t *testing.T, db *DB, operation func(*Tx) error) {
	t.Helper()
	err := db.View(operation)
	if !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("operation error = %v, want ErrCorruptDatabase", err)
	}
}
