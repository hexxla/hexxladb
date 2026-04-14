package hexxladb_test

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
)

func TestOpen_close_roundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	db2, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	if err := db2.Close(); err != nil {
		t.Fatalf("Close again: %v", err)
	}
}

func TestDB_Close_noOp(t *testing.T) {
	t.Parallel()
	var db *hexxladb.DB
	if err := db.Close(); err != nil {
		t.Fatalf("Close nil *DB: %v", err)
	}
	var zero hexxladb.DB
	if err := zero.Close(); err != nil {
		t.Fatalf("Close zero DB: %v", err)
	}
}

func TestDB_ViewUpdate_kv(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "kv.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.Put([]byte("k1"), []byte("v1"))
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		if tx.Writable() {
			t.Fatal("View tx should be read-only")
		}
		v, ok, err := tx.Get([]byte("k1"))
		if err != nil || !ok || string(v) != "v1" {
			t.Fatalf("Get: ok=%v v=%q err=%v", ok, v, err)
		}
		return tx.Put([]byte("x"), []byte("y"))
	})
	if !errors.Is(err, hexxladb.ErrTxReadOnly) {
		t.Fatalf("View Put: want ErrTxReadOnly, got %v", err)
	}
	err = db.Update(func(tx *hexxladb.Tx) error {
		if !tx.Writable() {
			t.Fatal("Update tx should be writable")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Batch(func(tx *hexxladb.Tx) error {
		return tx.Put([]byte("k2"), []byte("v2"))
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		v, ok, err := tx.Get([]byte("k2"))
		if err != nil || !ok || string(v) != "v2" {
			t.Fatalf("after Batch Put: ok=%v v=%q err=%v", ok, v, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDB_ViewClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "c.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	err = db.View(func(_ *hexxladb.Tx) error { return nil })
	if !errors.Is(err, hexxladb.ErrDatabaseClosed) {
		t.Fatalf("View: want ErrDatabaseClosed, got %v", err)
	}
	err = db.Batch(func(_ *hexxladb.Tx) error { return nil })
	if !errors.Is(err, hexxladb.ErrDatabaseClosed) {
		t.Fatalf("Batch: want ErrDatabaseClosed, got %v", err)
	}
}

func TestDB_UpdateNilFn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "n.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(nil); !errors.Is(err, hexxladb.ErrNilCallback) {
		t.Fatalf("got %v", err)
	}
	if err := db.Batch(nil); !errors.Is(err, hexxladb.ErrNilCallback) {
		t.Fatalf("Batch nil: got %v", err)
	}
	if err := db.View(nil); !errors.Is(err, hexxladb.ErrNilCallback) {
		t.Fatalf("got %v", err)
	}
}

func TestDB_concurrentReaders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "conc.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.Put([]byte("x"), []byte("1"))
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_ = db.View(func(tx *hexxladb.Tx) error {
				_, _, _ = tx.Get([]byte("x"))
				return nil
			})
		})
	}
	wg.Wait()
}

func TestDB_updateBlocksView(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "block.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		_ = db.Update(func(_ *hexxladb.Tx) error {
			close(started)
			<-done
			return nil
		})
	}()
	<-started
	ch := make(chan error, 1)
	go func() {
		ch <- db.View(func(_ *hexxladb.Tx) error { return nil })
	}()
	select {
	case err := <-ch:
		t.Fatalf("View should block during Update: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(done)
	if err := <-ch; err != nil {
		t.Fatal(err)
	}
}

func TestDB_batchBlocksView(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bblock.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		_ = db.Batch(func(_ *hexxladb.Tx) error {
			close(started)
			<-done
			return nil
		})
	}()
	<-started
	ch := make(chan error, 1)
	go func() {
		ch <- db.View(func(_ *hexxladb.Tx) error { return nil })
	}()
	select {
	case err := <-ch:
		t.Fatalf("View should block during Batch: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(done)
	if err := <-ch; err != nil {
		t.Fatal(err)
	}
}
