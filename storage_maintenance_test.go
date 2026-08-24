package hexxladb_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func maintenanceContent(seed uint64, size int) string {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(rng.Uint32())
	}
	return string(data)
}

func assertStorageAccounting(t *testing.T, stats hexxladb.StorageStats) {
	t.Helper()
	if stats.PageSize == 0 || stats.AllocatedPages == 0 || stats.ReachablePages == 0 {
		t.Fatalf("invalid storage stats: %#v", stats)
	}
	if stats.PrimaryBytes != stats.AllocatedPages*stats.PageSize {
		t.Fatalf("physical page accounting mismatch: %#v", stats)
	}
	if stats.LiveBytes != stats.ReachablePages*stats.PageSize {
		t.Fatalf("live page accounting mismatch: %#v", stats)
	}
	if stats.PrimaryBytes != stats.LiveBytes+stats.ReclaimableBytes {
		t.Fatalf("reclaimable page accounting mismatch: %#v", stats)
	}
}

func TestStorageMaintenanceRepresentativeChurn(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	srcPath := filepath.Join(t.TempDir(), "churn.db")
	db, err := hexxladb.Open(srcPath, &hexxladb.Options{
		EnableMVCC:       true,
		ChangelogEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const (
		cellCount   = 48
		generations = 4
	)
	keys := make([]hexxladb.PackedCoord, 0, cellCount)
	for i := range cellCount {
		key, err := hexxladb.Pack(hexxladb.Coord{Q: i, R: -i / 2})
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	for generation := range generations {
		if err := db.Update(func(tx *hexxladb.Tx) error {
			for i, key := range keys {
				if err := tx.PutCell(ctx, hexxladb.CellRecord{
					Key:        key,
					RawContent: maintenanceContent(uint64(generation*cellCount+i+1), 3000),
				}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	afterPuts, err := db.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	assertStorageAccounting(t, afterPuts)
	if afterPuts.ChangelogBytes == 0 {
		t.Fatalf("enabled changelog was not included in storage stats: %#v", afterPuts)
	}
	afterPutsMVCC, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Update(func(tx *hexxladb.Tx) error {
		for i := 0; i < len(keys); i += 2 {
			if err := tx.DeleteCell(ctx, keys[i]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	afterTombstones, err := db.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	assertStorageAccounting(t, afterTombstones)
	afterTombstonesMVCC, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	if afterTombstonesMVCC.VersionedRows <= afterPutsMVCC.VersionedRows {
		t.Fatalf("tombstones did not add physical versions: puts=%#v tombstones=%#v", afterPutsMVCC, afterTombstonesMVCC)
	}
	if afterTombstones.PrimaryBytes < afterPuts.PrimaryBytes {
		t.Fatalf("extend-only primary shrank after tombstones: puts=%#v tombstones=%#v", afterPuts, afterTombstones)
	}

	deleted, err := db.PruneCellVersions(afterTombstonesMVCC.CommitSeq+1, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if deleted == 0 {
		t.Fatal("representative prune removed no stale versions")
	}
	afterPrune, err := db.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	assertStorageAccounting(t, afterPrune)
	if afterPrune.PrimaryBytes < afterTombstones.PrimaryBytes {
		t.Fatalf("extend-only primary shrank during prune: tombstones=%#v prune=%#v", afterTombstones, afterPrune)
	}
	if afterPrune.ReclaimableBytes == 0 {
		t.Fatalf("prune exposed no reclaimable pages: %#v", afterPrune)
	}

	destPath := filepath.Join(t.TempDir(), "compacted.db")
	var progress []uint64
	if err := db.CompactWithOptions(ctx, destPath, &hexxladb.CompactOptions{
		BatchSize: 16,
		OnProgress: func(p hexxladb.CompactProgress) {
			progress = append(progress, p.CopiedKeys)
		},
	}); err != nil {
		t.Fatal(err)
	}
	if len(progress) < 2 {
		t.Fatalf("compaction did not report bounded batch progress: %v", progress)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] <= progress[i-1] || progress[i]-progress[i-1] > 16 {
			t.Fatalf("invalid compaction progress: %v", progress)
		}
	}

	dest, err := hexxladb.Open(destPath, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dest.Close() }()
	afterCompact, err := dest.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	assertStorageAccounting(t, afterCompact)
	if afterCompact.PrimaryBytes >= afterPrune.PrimaryBytes {
		t.Fatalf("compaction did not reduce primary: pruned=%#v compacted=%#v", afterPrune, afterCompact)
	}
	if afterCompact.ReclaimableBytes >= afterPrune.ReclaimableBytes {
		t.Fatalf("compaction did not reduce dead pages: pruned=%#v compacted=%#v", afterPrune, afterCompact)
	}

	t.Logf(
		"puts primary/live/dead=%d/%d/%d; tombstones=%d/%d/%d; prune=%d/%d/%d; compact=%d/%d/%d; changelog=%d; pruned_rows=%d",
		afterPuts.PrimaryBytes, afterPuts.LiveBytes, afterPuts.ReclaimableBytes,
		afterTombstones.PrimaryBytes, afterTombstones.LiveBytes, afterTombstones.ReclaimableBytes,
		afterPrune.PrimaryBytes, afterPrune.LiveBytes, afterPrune.ReclaimableBytes,
		afterCompact.PrimaryBytes, afterCompact.LiveBytes, afterCompact.ReclaimableBytes,
		afterPrune.ChangelogBytes,
		deleted,
	)
}

func TestCompactWithOptionsCancellationCanRetry(t *testing.T) {
	t.Parallel()
	db, _ := openCompactDB(t, &hexxladb.Options{EnableMVCC: true})
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for i := range 24 {
			key, err := hexxladb.Pack(hexxladb.Coord{Q: i, R: 0})
			if err != nil {
				return err
			}
			if err := tx.PutCell(t.Context(), hexxladb.CellRecord{Key: key, RawContent: maintenanceContent(uint64(i+1), 256)}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	destPath := filepath.Join(t.TempDir(), "retry.db")
	ctx, cancel := context.WithCancel(t.Context())
	var interruptedAt uint64
	err := db.CompactWithOptions(ctx, destPath, &hexxladb.CompactOptions{
		BatchSize: 2,
		OnProgress: func(p hexxladb.CompactProgress) {
			interruptedAt = p.CopiedKeys
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted compaction: got %v, want context cancellation", err)
	}
	if interruptedAt != 2 {
		t.Fatalf("first durable progress checkpoint = %d, want 2", interruptedAt)
	}
	for _, path := range []string{destPath, destPath + "-wal"} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("partial compaction artifact %q remains: %v", path, err)
		}
	}

	var resumedAt uint64
	if err := db.CompactWithOptions(t.Context(), destPath, &hexxladb.CompactOptions{
		BatchSize: 3,
		OnProgress: func(p hexxladb.CompactProgress) {
			if p.CopiedKeys <= resumedAt {
				t.Fatalf("non-monotonic resumed progress: before=%d after=%d", resumedAt, p.CopiedKeys)
			}
			resumedAt = p.CopiedKeys
		},
	}); err != nil {
		t.Fatal(err)
	}
	if resumedAt <= interruptedAt {
		t.Fatalf("retry did not complete beyond interruption: interrupted=%d resumed=%d", interruptedAt, resumedAt)
	}
}

func TestCompactWithOptionsRejectsUnboundedBatch(t *testing.T) {
	t.Parallel()
	db, _ := openCompactDB(t, nil)
	for _, batchSize := range []int{-1, 4097} {
		err := db.CompactWithOptions(t.Context(), filepath.Join(t.TempDir(), "invalid.db"), &hexxladb.CompactOptions{BatchSize: batchSize})
		if !errors.Is(err, hexxladb.ErrInvalidArgument) {
			t.Fatalf("BatchSize %d: got %v, want ErrInvalidArgument", batchSize, err)
		}
	}
}

func TestStorageStatsEncryptedChangelogAndClosedHandle(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "encrypted.db"), &hexxladb.Options{
		EncryptionKey:    []byte("0123456789abcdef0123456789abcdef"),
		ChangelogEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := hexxladb.Pack(hexxladb.Coord{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: key, RawContent: maintenanceContent(1, 3000)})
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := db.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	assertStorageAccounting(t, stats)
	if stats.ChangelogBytes == 0 {
		t.Fatalf("encrypted changelog size missing: %#v", stats)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.StorageStats(); !errors.Is(err, hexxladb.ErrDatabaseClosed) {
		t.Fatalf("closed StorageStats: got %v, want ErrDatabaseClosed", err)
	}
	var nilDB *hexxladb.DB
	if _, err := nilDB.StorageStats(); !errors.Is(err, hexxladb.ErrDatabaseClosed) {
		t.Fatalf("nil StorageStats: got %v, want ErrDatabaseClosed", err)
	}
}
