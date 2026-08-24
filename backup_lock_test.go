package hexxladb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

type cancelAfterChecksContext struct {
	context.Context
	remaining atomic.Int64
}

func newCancelAfterChecksContext(parent context.Context, checks int64) *cancelAfterChecksContext {
	ctx := &cancelAfterChecksContext{Context: parent}
	ctx.remaining.Store(checks)
	return ctx
}

func (ctx *cancelAfterChecksContext) Err() error {
	if ctx.remaining.Add(-1) < 0 {
		return context.Canceled
	}
	return ctx.Context.Err()
}

func TestBackupToHoldsReadLockUntilCaptureCompletes(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	db, err := Open(sourcePath, &Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	captureStarted := make(chan struct{})
	releaseCapture := make(chan struct{})
	backupDone := make(chan error, 1)
	go func() {
		backupDone <- db.backupTo(t.Context(), backupPath, func() {
			if db.mu.TryLock() {
				db.mu.Unlock()
				t.Error("backup capture did not hold the database read lock")
			}
			close(captureStarted)
			<-releaseCapture
		})
	}()
	<-captureStarted

	key, err := Pack(Coord{Q: 1})
	if err != nil {
		t.Fatal(err)
	}
	updateDone := make(chan error, 1)
	go func() {
		updateDone <- db.Update(func(tx *Tx) error {
			return tx.PutCell(t.Context(), CellRecord{Key: key, RawContent: "after-backup"})
		})
	}()
	select {
	case err := <-updateDone:
		t.Fatalf("write completed while backup capture held its read lock: %v", err)
	default:
	}

	close(releaseCapture)
	if err := <-backupDone; err != nil {
		t.Fatal(err)
	}
	if err := <-updateDone; err != nil {
		t.Fatal(err)
	}

	restored, err := Open(backupPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close() //nolint:errcheck // test cleanup
	if err := restored.View(func(tx *Tx) error {
		if _, ok, err := tx.GetCell(key); err != nil {
			return err
		} else if ok {
			t.Fatal("backup included a write that waited for the capture lock")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCopyBackupFilesCancellationRemovesPartialSet(t *testing.T) {
	dir := t.TempDir()
	sourcePrimary := filepath.Join(dir, "source.db")
	db, err := Open(sourcePrimary, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := os.Truncate(sourcePrimary, backupCopyBufferSize+1); err != nil {
		t.Fatal(err)
	}
	destPrimary := filepath.Join(dir, "backup.db")

	ctx := newCancelAfterChecksContext(t.Context(), 4)
	if err := db.BackupTo(ctx, destPrimary); !errors.Is(err, context.Canceled) {
		t.Fatalf("BackupTo error=%v, want context.Canceled", err)
	}
	for _, suffix := range []string{"", "-wal"} {
		if _, err := os.Stat(destPrimary + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canceled copy left component %q: %v", destPrimary+suffix, err)
		}
	}
}
