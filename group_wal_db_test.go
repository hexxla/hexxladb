package hexxladb_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// TestGroupWAL_concurrentUpdatesPreserveWrites verifies that concurrent callers remain
// serialized through durable commit finalization. A second writer must not build on
// unflushed B+ tree state from the first writer.
func TestGroupWAL_concurrentUpdatesPreserveWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "gw.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{
		EnableMVCC:           true,
		GroupWALMaxBatchWait: 50 * time.Millisecond,
		PageCacheSize:        -1,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	firstKey, err := hexxladb.Pack(hexxladb.Coord{Q: 7, R: 8})
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := hexxladb.Pack(hexxladb.Coord{Q: 8, R: 8})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	unblock := make(chan struct{})
	started := make(chan struct{})
	g2cb := make(chan struct{})
	firstReturned := make(chan struct{})
	var overlapped bool

	var wg sync.WaitGroup
	var firstErr, secondErr error
	wg.Go(func() {
		firstErr = db.Update(func(tx *hexxladb.Tx) error {
			if err := tx.PutCell(ctx, hexxladb.CellRecord{Key: firstKey, RawContent: "first"}); err != nil {
				return err
			}
			close(started)
			<-unblock
			return nil
		})
		close(firstReturned)
	})

	<-started
	wg.Go(func() {
		secondErr = db.Update(func(tx *hexxladb.Tx) error {
			select {
			case <-firstReturned:
			default:
				overlapped = true
			}
			close(g2cb)
			return tx.PutCell(ctx, hexxladb.CellRecord{Key: secondKey, RawContent: "second"})
		})
	})

	close(unblock)
	select {
	case <-g2cb:
	case <-time.After(3 * time.Second):
		t.Fatal("second Update did not reach its callback after the first callback was released")
	}
	wg.Wait()
	if firstErr != nil || secondErr != nil {
		t.Fatalf("Update: first=%v second=%v", firstErr, secondErr)
	}
	if overlapped {
		t.Fatal("second Update callback entered before the first Update returned")
	}
	assertGroupWALCells(t, db, firstKey, secondKey, 2)

	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	db, err = hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true, PageCacheSize: -1})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db.Close() }()
	assertGroupWALCells(t, db, firstKey, secondKey, 2)
}

func assertGroupWALCells(t *testing.T, db *hexxladb.DB, firstKey, secondKey hexxladb.PackedCoord, wantSeq uint64) {
	t.Helper()
	stats, err := db.StatsMVCC()
	if err != nil {
		t.Fatalf("StatsMVCC: %v", err)
	}
	if stats.CommitSeq != wantSeq {
		t.Errorf("CommitSeq: got %d want %d", stats.CommitSeq, wantSeq)
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		for _, key := range []hexxladb.PackedCoord{firstKey, secondKey} {
			if _, ok, err := tx.GetCell(key); err != nil {
				return err
			} else if !ok {
				t.Errorf("GetCell(%v): not found", key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("View: %v", err)
	}
}

// TestGroupWAL_concurrentUpdatesNoRace runs several writers with Group WAL + race detector.
func TestGroupWAL_concurrentUpdatesNoRace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "gw2.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	var wg sync.WaitGroup
	errOnce := make(chan error, 1)
	for g := range 4 {
		wg.Go(func() {
			for i := range 8 {
				p := lattice.PackedCoord{uint64(g), uint64(i)}
				rec := record.CellRecord{Key: p, RawContent: "x"}
				if err := db.Update(func(tx *hexxladb.Tx) error {
					return tx.PutCell(ctx, rec)
				}); err != nil {
					select {
					case errOnce <- err:
					default:
					}
					return
				}
			}
		})
	}
	wg.Wait()
	select {
	case e := <-errOnce:
		t.Fatalf("Update: %v", e)
	default:
	}
}
