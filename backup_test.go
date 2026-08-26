package hexxladb_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func putBackupCell(t *testing.T, db *hexxladb.DB, coord hexxladb.Coord, content string) hexxladb.PackedCoord {
	t.Helper()
	key, err := hexxladb.Pack(coord)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), hexxladb.CellRecord{
			Key:        key,
			RawContent: content,
		})
	}); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestBackupToActiveMVCCRestoresCapturedSnapshot(t *testing.T) {
	t.Parallel()
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	db, err := hexxladb.Open(sourcePath, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	key := putBackupCell(t, db, hexxladb.Coord{Q: 1, R: -1}, "first")
	firstStats, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	putBackupCell(t, db, hexxladb.Coord{Q: 1, R: -1}, "captured")

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := db.BackupTo(t.Context(), backupPath); err != nil {
		t.Fatal(err)
	}
	putBackupCell(t, db, hexxladb.Coord{Q: 1, R: -1}, "after-backup")

	restored, err := hexxladb.Open(backupPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })

	if err := restored.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(key)
		if err != nil {
			return err
		}
		if !ok || rec.RawContent != "captured" {
			t.Fatalf("restored latest cell: ok=%v content=%q", ok, rec.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := restored.ViewAt(firstStats.CommitSeq, func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(key)
		if err != nil {
			return err
		}
		if !ok || rec.RawContent != "first" {
			t.Fatalf("restored historical cell: ok=%v content=%q", ok, rec.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	report, err := restored.HealthCheck(t.Context(), hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		t.Fatal(err)
	}
	if report.CellCount != 1 {
		t.Fatalf("restored health cell count=%d, want 1", report.CellCount)
	}
}

func TestBackupToEncryptedChangelogUsesDestinationSidecar(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	customChangelogPath := filepath.Join(dir, "audit", "source.log")
	if err := os.Mkdir(filepath.Dir(customChangelogPath), 0o700); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x42}, 32)
	db, err := hexxladb.Open(sourcePath, &hexxladb.Options{
		EnableMVCC:       true,
		ChangelogEnabled: true,
		ChangelogPath:    customChangelogPath,
		ChangelogLazy:    true,
		EncryptionKey:    key,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cellKey := putBackupCell(t, db, hexxladb.Coord{Q: 2, R: -1}, "encrypted")

	backupPath := filepath.Join(dir, "backup.db")
	if err := db.BackupTo(t.Context(), backupPath); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{backupPath, backupPath + "-wal", backupPath + "-changelog"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("backup component %q: %v", path, err)
		}
	}

	wrongKey := bytes.Repeat([]byte{0x24}, 32)
	if _, err := hexxladb.Open(backupPath, &hexxladb.Options{EncryptionKey: wrongKey}); !errors.Is(err, hexxladb.ErrEncryptionKeyMismatch) {
		t.Fatalf("wrong-key restore error=%v, want ErrEncryptionKeyMismatch", err)
	}

	restored, err := hexxladb.Open(backupPath, &hexxladb.Options{
		ChangelogEnabled: true,
		EncryptionKey:    key,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	storage, err := restored.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	if storage.PhysicalPageSize <= storage.PageSize {
		t.Fatalf("restored encrypted page sizes = %d/%d, want authenticated envelope", storage.PageSize, storage.PhysicalPageSize)
	}
	records, err := restored.ReadChangelogSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatalf("restored changelog=%#v", records)
	}
	for _, rec := range records {
		if rec.Op != hexxladb.ChangelogOpPutCell || !bytes.Equal(rec.Key, records[0].Key) || rec.Hash != records[0].Hash {
			t.Fatalf("restored changelog contains non-equivalent redelivery: %#v", records)
		}
	}
	if err := restored.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(cellKey)
		if err != nil {
			return err
		}
		if !ok || rec.RawContent != "encrypted" {
			t.Fatalf("restored encrypted cell: ok=%v content=%q", ok, rec.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.HealthCheck(t.Context(), hexxladb.DefaultHealthCheckConfig()); err != nil {
		t.Fatal(err)
	}
}

func TestBackupToUsesOpenDescriptorsAfterPathReplacement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := hexxladb.Open(sourcePath, &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cellKey := putBackupCell(t, db, hexxladb.Coord{Q: 3, R: -2}, "open-inode")

	if err := os.Rename(sourcePath, sourcePath+".moved"); err != nil {
		t.Skipf("platform does not permit renaming an open database: %v", err)
	}
	if err := os.Rename(sourcePath+"-changelog", sourcePath+"-changelog.moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("replacement primary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath+"-changelog", []byte("replacement changelog"), 0o600); err != nil {
		t.Fatal(err)
	}

	backupPath := filepath.Join(dir, "backup.db")
	if err := db.BackupTo(t.Context(), backupPath); err != nil {
		t.Fatal(err)
	}
	restored, err := hexxladb.Open(backupPath, &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	if err := restored.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(cellKey)
		if err != nil {
			return err
		}
		if !ok || rec.RawContent != "open-inode" {
			t.Fatalf("restored open-descriptor cell: ok=%v content=%q", ok, rec.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	records, err := restored.ReadChangelogSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Op != hexxladb.ChangelogOpPutCell {
		t.Fatalf("restored open-descriptor changelog=%#v", records)
	}
}

func TestBackupToDestinationCollisionPreservesExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := hexxladb.Open(sourcePath, &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	putBackupCell(t, db, hexxladb.Coord{}, "source")

	for _, suffix := range []string{"", "-wal", "-changelog"} {
		t.Run(suffix, func(t *testing.T) {
			backupPath := filepath.Join(t.TempDir(), "backup.db")
			collisionPath := backupPath + suffix
			want := []byte("existing")
			if err := os.WriteFile(collisionPath, want, 0o600); err != nil {
				t.Fatal(err)
			}
			err := db.BackupTo(t.Context(), backupPath)
			if !errors.Is(err, os.ErrExist) {
				t.Fatalf("BackupTo collision error=%v, want os.ErrExist", err)
			}
			got, err := os.ReadFile(collisionPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("existing destination changed: got %q, want %q", got, want)
			}
			for _, otherSuffix := range []string{"", "-wal", "-changelog"} {
				otherPath := backupPath + otherSuffix
				if otherPath == collisionPath {
					continue
				}
				if _, err := os.Stat(otherPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed backup left component %q: %v", otherPath, err)
				}
			}
		})
	}
}

func TestBackupToCanceledContextLeavesNoFiles(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "source.db"), &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := db.BackupTo(ctx, backupPath); !errors.Is(err, context.Canceled) {
		t.Fatalf("BackupTo canceled error=%v, want context.Canceled", err)
	}
	for _, suffix := range []string{"", "-wal", "-changelog"} {
		if _, err := os.Stat(backupPath + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canceled backup left component %q: %v", backupPath+suffix, err)
		}
	}
}

func TestBackupToClosedDatabase(t *testing.T) {
	t.Parallel()
	var nilDB *hexxladb.DB
	if err := nilDB.BackupTo(t.Context(), filepath.Join(t.TempDir(), "nil.db")); !errors.Is(err, hexxladb.ErrDatabaseClosed) {
		t.Fatalf("nil BackupTo error=%v, want ErrDatabaseClosed", err)
	}

	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "source.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.BackupTo(t.Context(), filepath.Join(t.TempDir(), "closed.db")); !errors.Is(err, hexxladb.ErrDatabaseClosed) {
		t.Fatalf("closed BackupTo error=%v, want ErrDatabaseClosed", err)
	}
}
