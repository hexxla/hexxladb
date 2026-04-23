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

// TestGroupWAL_secondUpdateCanOverlapFirstWait checks that a second [DB.Update] can run (and
// reach its callback) while the first is blocked in the group-WAL wait after releasing [db.mu].
func TestGroupWAL_secondUpdateCanOverlapFirstWait(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "gw.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	p := lattice.PackedCoord{7, 8}
	rec := record.CellRecord{Key: p, RawContent: "a"}
	ctx := context.Background()

	unblock := make(chan struct{})
	started := make(chan struct{})
	g2cb := make(chan struct{})

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Go(func() {
		errCh <- db.Update(func(tx *hexxladb.Tx) error {
			if err := tx.PutCell(ctx, rec); err != nil {
				return err
			}
			close(started)
			<-unblock
			return nil
		})
	})

	<-started
	wg.Go(func() {
		errCh <- db.Update(func(tx *hexxladb.Tx) error {
			close(g2cb)
			return nil
		})
	})

	// Let G1 finish its callback and enter the async group-WAL wait (releases [db.mu]).
	close(unblock)
	select {
	case <-g2cb:
	case <-time.After(3 * time.Second):
		t.Fatal("second Update did not reach its callback while the first was in group-WAL wait")
	}
	wg.Wait()
	if e := <-errCh; e != nil {
		t.Fatalf("Update: %v", e)
	}
	if e := <-errCh; e != nil {
		t.Fatalf("Update: %v", e)
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
