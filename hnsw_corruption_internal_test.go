package hexxladb

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/hnsw"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestSearchByEmbeddingRejectsCorruptHNSWStorage(t *testing.T) {
	t.Parallel()
	for name, corrupt := range map[string]func(*Tx, lattice.PackedCoord) error{
		"meta": func(tx *Tx, _ lattice.PackedCoord) error {
			data := append(hnsw.EncodeMeta(&hnsw.Meta{M: hnsw.DefaultM, EfC: hnsw.DefaultEfConstruction, Count: 1}), 0)
			return tx.putDirect([]byte(index.HNSWMetaKey), data)
		},
		"entry": func(tx *Tx, coord lattice.PackedCoord) error {
			storage := &txHNSWStorage{tx: tx}
			if err := storage.PutHNSWMeta(&hnsw.Meta{M: hnsw.DefaultM, EfC: hnsw.DefaultEfConstruction, Count: 1}); err != nil {
				return err
			}
			return tx.putDirect([]byte(index.HNSWEntryKey), make([]byte, 15))
		},
		"node": func(tx *Tx, coord lattice.PackedCoord) error {
			storage := &txHNSWStorage{tx: tx}
			if err := storage.PutHNSWMeta(&hnsw.Meta{M: hnsw.DefaultM, EfC: hnsw.DefaultEfConstruction, Count: 1}); err != nil {
				return err
			}
			if err := storage.PutHNSWEntry(coord); err != nil {
				return err
			}
			return tx.putDirect(index.HNSWNodeKey(coord), []byte{0, 0})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, err := Open(filepath.Join(t.TempDir(), "hnsw.db"), &Options{
				EmbeddingDimension: 2,
				DistanceMetric:     DistanceCosine,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			coord, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
			if err := db.Update(func(tx *Tx) error {
				if err := tx.putDirect(index.EmbedKey(coord), encodeFloat32s([]float32{1, 0})); err != nil {
					return err
				}
				return corrupt(tx, coord)
			}); err != nil {
				t.Fatal(err)
			}

			err = db.View(func(tx *Tx) error {
				_, err := tx.SearchByEmbedding([]float32{1, 0}, EmbeddingSearchConfig{})
				return err
			})
			if !errors.Is(err, ErrCorruptDatabase) {
				t.Fatalf("SearchByEmbedding error = %v, want ErrCorruptDatabase", err)
			}
		})
	}
}

func TestEmbeddingChurnFailureAbortsStagedVector(t *testing.T) {
	t.Parallel()
	db, err := Open(filepath.Join(t.TempDir(), "churn.db"), &Options{EmbeddingDimension: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	target, _ := Pack(Coord{Q: 1})
	other, _ := Pack(Coord{Q: 2})
	if err := db.Update(func(tx *Tx) error {
		if err := tx.PutEmbedding(target, []float32{1, 0}); err != nil {
			return err
		}
		return tx.PutEmbedding(other, []float32{0, 1})
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"replace", "delete"} {
		t.Run(name, func(t *testing.T) {
			err := db.Update(func(tx *Tx) error {
				if err := tx.putDirect(index.HNSWNodeKey(other), []byte{0, 0}); err != nil {
					return err
				}
				var writeErr error
				if name == "replace" {
					writeErr = tx.PutEmbedding(target, []float32{0.5, 0.5})
				} else {
					writeErr = tx.DeleteEmbedding(target)
				}
				if !errors.Is(writeErr, ErrCorruptDatabase) {
					t.Errorf("write error = %v, want ErrCorruptDatabase", writeErr)
				}
				return nil
			})
			if !errors.Is(err, ErrCorruptDatabase) {
				t.Fatalf("Update = %v, want ErrCorruptDatabase", err)
			}
			if err := db.View(func(tx *Tx) error {
				vec, found, err := tx.GetEmbedding(target)
				if err != nil || !found || len(vec) != 2 || vec[0] != 1 || vec[1] != 0 {
					t.Errorf("target embedding = %v, %t, %v", vec, found, err)
				}
				_, err = tx.SearchByEmbedding([]float32{1, 0}, EmbeddingSearchConfig{MaxResults: 1})
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
