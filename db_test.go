package hexxladb_test

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
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

func TestDB_ReadChangelogSince_disabled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "c.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ReadChangelogSince(0, 10)
	if !errors.Is(err, hexxladb.ErrChangelogDisabled) {
		t.Fatalf("got %v", err)
	}
}

func TestDB_ReadChangelogSince_PutCell(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "chg.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(context.Background(), record.CellRecord{Key: p, RawContent: "x"})
	}); err != nil {
		t.Fatal(err)
	}
	recs, err := db.ReadChangelogSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if recs[0].Op != hexxladb.ChangelogOpPutCell {
		t.Fatalf("op=%d", recs[0].Op)
	}
	more, err := db.ReadChangelogSince(recs[0].Seq, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 0 {
		t.Fatalf("expected no tail after seq, got %d", len(more))
	}
}

func TestDB_ReadChangelogSince_ResolveSeam_emits_OpResolveSeam(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "chg_resolve.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	p0, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	if err != nil {
		t.Fatal(err)
	}
	p1, err := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	if err != nil {
		t.Fatal(err)
	}
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()

	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutSeam(context.Background(), record.SeamRecord{
			ID: id, CellA: p0, CellB: p1, SeamType: "t",
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.ResolveSeam(id, "done", "note")
	}); err != nil {
		t.Fatal(err)
	}

	recs, err := db.ReadChangelogSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 changelog records, got %d", len(recs))
	}
	if recs[0].Op != hexxladb.ChangelogOpPutSeam {
		t.Fatalf("first op=%d want PutSeam", recs[0].Op)
	}
	if recs[1].Op != hexxladb.ChangelogOpResolveSeam {
		t.Fatalf("second op=%d want ResolveSeam", recs[1].Op)
	}
}

func TestDB_changelog_resumeAfterReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "chg_reopen.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	p, err := lattice.Pack(lattice.Coord{Q: 1, R: -1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(context.Background(), record.CellRecord{Key: p, RawContent: "a"})
	}); err != nil {
		t.Fatal(err)
	}
	recs1, err := db.ReadChangelogSince(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs1) != 1 {
		t.Fatalf("before close: want 1 changelog record, got %d", len(recs1))
	}
	lastSeq := recs1[len(recs1)-1].Seq
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := hexxladb.Open(path, &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	fromStart, err := db2.ReadChangelogSince(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromStart) != 1 || fromStart[0].Seq != lastSeq {
		t.Fatalf("after reopen ReadSince(0): got %d records, want seq %d", len(fromStart), lastSeq)
	}
	tail, err := db2.ReadChangelogSince(lastSeq, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 0 {
		t.Fatalf("after reopen ReadSince(lastSeq): want empty, got %d", len(tail))
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
