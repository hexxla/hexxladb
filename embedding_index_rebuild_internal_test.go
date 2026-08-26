package hexxladb

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestEmbeddingIndexRebuildRejectsInterveningMutation(t *testing.T) {
	db, coord := openInternalRebuildDB(t)
	var hookErr error
	db.embeddingRebuildFaults = &embeddingRebuildFaultHooks{beforePublish: func() {
		hookErr = db.Update(func(tx *Tx) error {
			other, err := lattice.Pack(lattice.Coord{Q: 2})
			if err != nil {
				return err
			}
			return tx.PutEmbeddingWithOptions(other, []float32{0, 1, 0}, EmbeddingWriteOptions{DeferIndexMaintenance: true})
		})
	}}
	_, err := db.RebuildEmbeddingIndex(t.Context(), nil)
	db.embeddingRebuildFaults = nil
	if hookErr != nil {
		t.Fatalf("intervening mutation: %v", hookErr)
	}
	if !errors.Is(err, ErrEmbeddingIndexChanged) {
		t.Fatalf("error = %v, want ErrEmbeddingIndexChanged", err)
	}
	if err := db.View(func(tx *Tx) error {
		_, stats, err := tx.SearchByEmbeddingWithStats([]float32{1, 0, 0}, EmbeddingSearchConfig{})
		if err != nil {
			return err
		}
		if stats.Path != EmbeddingSearchPathFlat {
			t.Fatalf("path = %q, want flat", stats.Path)
		}
		_, found, err := tx.GetEmbedding(coord)
		if err != nil || !found {
			t.Fatalf("original embedding found/error = %v/%v", found, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddingIndexRebuildPublishAbortRetainsOldGraph(t *testing.T) {
	db, _ := openInternalRebuildDB(t)
	injected := errors.New("injected rebuild publish failure")
	db.embeddingRebuildFaults = &embeddingRebuildFaultHooks{beforePublish: func() {
		db.commitFaults = &commitFaultHooks{beforeEngineCommit: func() error { return injected }}
	}}
	_, err := db.RebuildEmbeddingIndex(t.Context(), nil)
	db.embeddingRebuildFaults = nil
	db.commitFaults = nil
	if !errors.Is(err, injected) {
		t.Fatalf("error = %v, want injected failure", err)
	}
	if err := db.View(func(tx *Tx) error {
		if _, found, err := (&txHNSWStorage{tx: tx}).GetHNSWMeta(); err != nil {
			return fmt.Errorf("read old graph meta: %w", err)
		} else if !found {
			return errors.New("old graph meta not found")
		}
		_, stats, err := tx.SearchByEmbeddingWithStats([]float32{1, 0, 0}, EmbeddingSearchConfig{})
		if err != nil {
			return err
		}
		if stats.Path != EmbeddingSearchPathFlat {
			t.Fatalf("path = %q, want flat after aborted publish", stats.Path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddingIndexCorruptStateFailsClosed(t *testing.T) {
	db, _ := openInternalRebuildDB(t)
	if err := db.Update(func(tx *Tx) error {
		return tx.putDirect([]byte(index.HNSWStateKey), []byte("invalid"))
	}); err != nil {
		t.Fatal(err)
	}
	err := db.View(func(tx *Tx) error {
		_, _, err := tx.SearchByEmbeddingWithStats([]float32{1, 0, 0}, EmbeddingSearchConfig{})
		return err
	})
	if !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("error = %v, want ErrCorruptDatabase", err)
	}
}

func TestEmbeddingIndexRevisionOverflowFailsClosed(t *testing.T) {
	db, coord := openInternalRebuildDB(t)
	if err := db.Update(func(tx *Tx) error {
		return tx.putEmbeddingIndexState(embeddingIndexState{revision: math.MaxUint64})
	}); err != nil {
		t.Fatal(err)
	}
	err := db.Update(func(tx *Tx) error {
		return tx.PutEmbeddingWithOptions(coord, []float32{0, 1, 0}, EmbeddingWriteOptions{DeferIndexMaintenance: true})
	})
	if !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("error = %v, want ErrCorruptDatabase", err)
	}
}

func TestEstimateEmbeddingIndexRebuildBytes(t *testing.T) {
	const count = 20_000
	got := estimateEmbeddingIndexRebuildBytes(count, 32)
	want := uint64(count) * (uint64(56<<10) + 32*24)
	if got != want {
		t.Fatalf("estimate = %d, want %d", got, want)
	}
}

func openInternalRebuildDB(t *testing.T) (*DB, lattice.PackedCoord) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "internal-rebuild.db"), &Options{EnableMVCC: true, EmbeddingDimension: 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	coord, err := lattice.Pack(lattice.Coord{Q: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *Tx) error {
		return tx.PutEmbedding(coord, []float32{1, 0, 0})
	}); err != nil {
		t.Fatal(err)
	}
	return db, coord
}
