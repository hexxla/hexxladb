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
				if err := tx.putEmbeddingIndexState(embeddingIndexState{}); err != nil {
					return err
				}
				return corrupt(tx, coord)
			}); err != nil {
				t.Fatal(err)
			}

			err = db.View(func(tx *Tx) error {
				_, err := tx.SearchByEmbedding(t.Context(), []float32{1, 0}, EmbeddingSearchConfig{})
				return err
			})
			if !errors.Is(err, ErrCorruptDatabase) {
				t.Fatalf("SearchByEmbedding error = %v, want ErrCorruptDatabase", err)
			}
		})
	}
}

func TestSearchByEmbeddingFallsBackWhenGraphUnderfills(t *testing.T) {
	t.Parallel()
	db, err := Open(filepath.Join(t.TempDir(), "underfilled.db"), &Options{EmbeddingDimension: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	first, _ := Pack(Coord{Q: 1})
	second, _ := Pack(Coord{Q: 2})
	if err := db.Update(func(tx *Tx) error {
		if err := tx.PutEmbedding(first, []float32{1, 0}); err != nil {
			return err
		}
		if err := tx.putDirect(index.EmbedKey(second), encodeFloat32s([]float32{0, 1})); err != nil {
			return err
		}
		return (&txHNSWStorage{tx: tx}).PutHNSWMeta(&hnsw.Meta{M: hnsw.DefaultM, EfC: hnsw.DefaultEfConstruction, Count: 2})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *Tx) error {
		hits, stats, err := tx.SearchByEmbeddingWithStats(t.Context(), []float32{0, 1}, EmbeddingSearchConfig{MaxResults: 2, MinScore: 0.5})
		if err == nil && (stats.Path != EmbeddingSearchPathFlat || len(hits) != 1 || hits[0].Coord != second) {
			t.Errorf("search returned %v on path %q, want exact match at %v", hits, stats.Path, second)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteEmbeddingFallsBackWhenRepairDisconnectsGraph(t *testing.T) {
	t.Parallel()
	db, err := Open(filepath.Join(t.TempDir(), "delete-disconnect.db"), &Options{EmbeddingDimension: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	keys := make([]PackedCoord, 7)
	for i := range keys {
		keys[i], err = Pack(Coord{Q: i})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Update(func(tx *Tx) error {
		storage := &txHNSWStorage{tx: tx}
		if err := storage.PutHNSWEntry(keys[0]); err != nil {
			return err
		}
		if err := storage.PutHNSWMeta(&hnsw.Meta{M: 2, EfC: hnsw.DefaultEfConstruction, Count: 7}); err != nil {
			return err
		}
		for i, key := range keys {
			if err := tx.putDirect(index.EmbedKey(key), encodeFloat32s([]float32{1, 0})); err != nil {
				return err
			}
			var links []PackedCoord
			switch i {
			case 0:
				links = keys[1:5] // Only one vacancy after deleting keys[1].
			case 1:
				links = keys[5:] // Two coordinates depend on the removed node.
			}
			if err := storage.PutHNSWNode(&hnsw.Node{Coord: key, Neighbors: [][]PackedCoord{links}}); err != nil {
				return err
			}
		}
		if err := tx.putEmbeddingIndexState(embeddingIndexState{}); err != nil {
			return err
		}
		return tx.DeleteEmbedding(keys[1])
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *Tx) error {
		hits, stats, err := tx.SearchByEmbeddingWithStats(t.Context(), []float32{1, 0}, EmbeddingSearchConfig{MaxResults: 1})
		if err == nil && (stats.Path != EmbeddingSearchPathFlat || len(hits) != 1) {
			t.Errorf("search returned %v on path %q, want one exact result", hits, stats.Path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
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
				_, err = tx.SearchByEmbedding(t.Context(), []float32{1, 0}, EmbeddingSearchConfig{MaxResults: 1})
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
