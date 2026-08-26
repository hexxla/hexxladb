package hexxladb_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestEmbeddingIndexDeferredRebuildLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deferred.db")
	opts := &hexxladb.Options{EnableMVCC: true, EmbeddingDimension: 3}
	db, err := hexxladb.Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	coord := rebuildCoord(t, 1)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(t.Context(), hexxladb.CellRecord{Key: coord, RawContent: "deferred"}); err != nil {
			return err
		}
		return tx.PutEmbeddingWithOptions(coord, []float32{1, 0, 0}, hexxladb.EmbeddingWriteOptions{DeferIndexMaintenance: true})
	}); err != nil {
		t.Fatal(err)
	}
	assertEmbeddingSearchPath(t, db, hexxladb.EmbeddingSearchPathFlat, 1)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = hexxladb.Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	assertEmbeddingSearchPath(t, db, hexxladb.EmbeddingSearchPathFlat, 1)

	stats, err := db.RebuildEmbeddingIndex(t.Context(), &hexxladb.EmbeddingIndexRebuildOptions{MaxVectors: 10})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Vectors != 1 || stats.Revision == 0 || stats.EstimatedPeakBytes == 0 {
		t.Fatalf("unexpected rebuild stats: %+v", stats)
	}
	assertEmbeddingSearchPath(t, db, hexxladb.EmbeddingSearchPathHNSW, 1)

	second := rebuildCoord(t, 2)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(t.Context(), hexxladb.CellRecord{Key: second}); err != nil {
			return err
		}
		return tx.PutEmbedding(second, []float32{0, 1, 0})
	}); err != nil {
		t.Fatal(err)
	}
	assertEmbeddingSearchPath(t, db, hexxladb.EmbeddingSearchPathHNSW, 2)
}

func TestEmbeddingIndexRebuildRejectsInvalidResourceBudget(t *testing.T) {
	db := openRebuildDB(t, "budget.db")
	_, err := db.RebuildEmbeddingIndex(t.Context(), &hexxladb.EmbeddingIndexRebuildOptions{MaxMemoryBytes: 1})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
}

func TestEmbeddingIndexRebuildLimitDoesNotInvalidateCurrentGraph(t *testing.T) {
	db := openRebuildDB(t, "limit.db")
	putRebuildVectors(t, db, 2, false)
	assertEmbeddingSearchPath(t, db, hexxladb.EmbeddingSearchPathHNSW, 2)

	_, err := db.RebuildEmbeddingIndex(t.Context(), &hexxladb.EmbeddingIndexRebuildOptions{MaxVectors: 1})
	if !errors.Is(err, hexxladb.ErrEmbeddingIndexTooLarge) {
		t.Fatalf("error = %v, want ErrEmbeddingIndexTooLarge", err)
	}
	assertEmbeddingSearchPath(t, db, hexxladb.EmbeddingSearchPathHNSW, 2)
}

func TestEmbeddingIndexCanceledRebuildRetainsFlatFallback(t *testing.T) {
	db := openRebuildDB(t, "cancel.db")
	putRebuildVectors(t, db, 2, true)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.RebuildEmbeddingIndex(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	assertEmbeddingSearchPath(t, db, hexxladb.EmbeddingSearchPathFlat, 2)
}

func TestEmbeddingIndexRebuildPublishesEmptyGraph(t *testing.T) {
	db := openRebuildDB(t, "empty.db")
	coord := rebuildCoord(t, 1)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutEmbeddingWithOptions(coord, []float32{1, 0, 0}, hexxladb.EmbeddingWriteOptions{DeferIndexMaintenance: true}); err != nil {
			return err
		}
		return tx.DeleteEmbedding(coord)
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := db.RebuildEmbeddingIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Vectors != 0 {
		t.Fatalf("vectors = %d, want 0", stats.Vectors)
	}
	assertEmbeddingSearchPath(t, db, hexxladb.EmbeddingSearchPathFlat, 0)
}

func TestCompactPreservesDirtyEmbeddingIndexFallback(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "compact-source.db")
	destinationPath := filepath.Join(dir, "compact-destination.db")
	db, err := hexxladb.Open(sourcePath, &hexxladb.Options{EnableMVCC: true, EmbeddingDimension: 3})
	if err != nil {
		t.Fatal(err)
	}
	putRebuildVectors(t, db, 2, true)
	if err := db.Compact(t.Context(), destinationPath); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	compacted, err := hexxladb.Open(destinationPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = compacted.Close() })
	assertEmbeddingSearchPath(t, compacted, hexxladb.EmbeddingSearchPathFlat, 2)
}

func TestMigrationPreservesDirtyEmbeddingIndexFallback(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "migration-source.db")
	destinationPath := filepath.Join(dir, "migration-destination.db")
	db, err := hexxladb.Open(sourcePath, &hexxladb.Options{EmbeddingDimension: 3})
	if err != nil {
		t.Fatal(err)
	}
	putRebuildVectors(t, db, 2, true)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := hexxladb.MigrateV1ToV2(t.Context(), sourcePath, destinationPath, nil); err != nil {
		t.Fatal(err)
	}

	migrated, err := hexxladb.Open(destinationPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migrated.Close() })
	assertEmbeddingSearchPath(t, migrated, hexxladb.EmbeddingSearchPathFlat, 2)
}

func openRebuildDB(t *testing.T, name string) *hexxladb.DB {
	t.Helper()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), name), &hexxladb.Options{EnableMVCC: true, EmbeddingDimension: 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func putRebuildVectors(t *testing.T, db *hexxladb.DB, count int, deferred bool) {
	t.Helper()
	err := db.Update(func(tx *hexxladb.Tx) error {
		for i := range count {
			coord := rebuildCoord(t, i+1)
			if err := tx.PutCell(t.Context(), hexxladb.CellRecord{Key: coord}); err != nil {
				return err
			}
			if err := tx.PutEmbeddingWithOptions(coord, []float32{float32(i + 1), 1, 0}, hexxladb.EmbeddingWriteOptions{DeferIndexMaintenance: deferred}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertEmbeddingSearchPath(t *testing.T, db *hexxladb.DB, want hexxladb.EmbeddingSearchPath, wantResults int) {
	t.Helper()
	err := db.View(func(tx *hexxladb.Tx) error {
		results, stats, err := tx.SearchByEmbeddingWithStats([]float32{1, 0, 0}, hexxladb.EmbeddingSearchConfig{MaxResults: 10})
		if err != nil {
			return err
		}
		if stats.Path != want || len(results) != wantResults {
			t.Fatalf("path/results = %q/%d, want %q/%d", stats.Path, len(results), want, wantResults)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func rebuildCoord(t *testing.T, q int) hexxladb.PackedCoord {
	t.Helper()
	coord, err := hexxladb.Pack(hexxladb.Coord{Q: q})
	if err != nil {
		t.Fatal(err)
	}
	return coord
}
