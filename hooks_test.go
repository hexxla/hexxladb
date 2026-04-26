package hexxladb_test

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func newULID() string {
	return ulid.MustNew(ulid.Now(), rand.Reader).String()
}

func openHookDB(t *testing.T, opts *hexxladb.Options) *hexxladb.DB {
	t.Helper()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "h.db"), opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return db
}

func packCoord(t *testing.T, q, r int) lattice.PackedCoord {
	t.Helper()
	pk, err := lattice.Pack(lattice.Coord{Q: q, R: r})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	return pk
}

// TestHook_AfterPutCell_CalledOnWrite verifies the hook is invoked for each PutCell.
func TestHook_AfterPutCell_CalledOnWrite(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	db := openHookDB(t, &hexxladb.Options{
		AfterPutCell: hexxladb.AfterPutCellHookFunc(func(_ context.Context, _ record.CellRecord) error {
			calls.Add(1)
			return nil
		}),
	})

	ctx := t.Context()
	for i := range 3 {
		pk := packCoord(t, i, 0)
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(ctx, record.CellRecord{Key: pk, RawContent: "x"})
		}); err != nil {
			t.Fatalf("PutCell %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 hook calls, got %d", got)
	}
}

// TestHook_AfterPutCell_ReceivesRecord verifies the hook sees the written record.
func TestHook_AfterPutCell_ReceivesRecord(t *testing.T) {
	t.Parallel()
	var gotContent string
	db := openHookDB(t, &hexxladb.Options{
		AfterPutCell: hexxladb.AfterPutCellHookFunc(func(_ context.Context, rec record.CellRecord) error {
			gotContent = rec.RawContent
			return nil
		}),
	})

	pk := packCoord(t, 0, 0)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: pk, RawContent: "hello-hook"})
	}); err != nil {
		t.Fatalf("PutCell: %v", err)
	}
	if gotContent != "hello-hook" {
		t.Errorf("hook received content %q, want %q", gotContent, "hello-hook")
	}
}

// TestHook_AfterPutCell_ErrorSurfaced verifies hook errors propagate from PutCell.
func TestHook_AfterPutCell_ErrorSurfaced(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("hook error")
	db := openHookDB(t, &hexxladb.Options{
		AfterPutCell: hexxladb.AfterPutCellHookFunc(func(_ context.Context, _ record.CellRecord) error {
			return sentinel
		}),
	})

	pk := packCoord(t, 0, 0)
	err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: pk, RawContent: "x"})
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

// TestHook_AfterPutCell_NilHookNoOp verifies nil hook doesn't panic.
func TestHook_AfterPutCell_NilHookNoOp(t *testing.T) {
	t.Parallel()
	db := openHookDB(t, nil)
	pk := packCoord(t, 0, 0)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: pk, RawContent: "x"})
	}); err != nil {
		t.Fatalf("PutCell with nil hook: %v", err)
	}
}

// TestHook_AfterPutCell_MVCCPath verifies the hook fires on the MVCC write path too.
func TestHook_AfterPutCell_MVCCPath(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	db := openHookDB(t, &hexxladb.Options{
		EnableMVCC: true,
		AfterPutCell: hexxladb.AfterPutCellHookFunc(func(_ context.Context, _ record.CellRecord) error {
			calls.Add(1)
			return nil
		}),
	})

	pk := packCoord(t, 0, 0)
	for range 2 {
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(t.Context(), record.CellRecord{Key: pk, RawContent: "mvcc"})
		}); err != nil {
			t.Fatalf("PutCell: %v", err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 hook calls on MVCC path, got %d", got)
	}
}

// TestHook_AfterPutSeam_CalledOnPutSeam verifies the seam hook fires on PutSeam.
func TestHook_AfterPutSeam_CalledOnPutSeam(t *testing.T) {
	t.Parallel()
	var gotType string
	db := openHookDB(t, &hexxladb.Options{
		AfterPutSeam: hexxladb.AfterPutSeamHookFunc(func(_ context.Context, rec record.SeamRecord) error {
			gotType = rec.SeamType
			return nil
		}),
	})

	pkA := packCoord(t, 0, 0)
	pkB := packCoord(t, 1, 0)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutSeam(t.Context(), record.SeamRecord{
			ID:       newULID(),
			CellA:    pkA,
			CellB:    pkB,
			SeamType: "test-seam",
		})
	}); err != nil {
		t.Fatalf("PutSeam: %v", err)
	}
	if gotType != "test-seam" {
		t.Errorf("hook got seam type %q, want %q", gotType, "test-seam")
	}
}

// TestHook_AfterPutSeam_ErrorSurfaced verifies seam hook errors propagate.
func TestHook_AfterPutSeam_ErrorSurfaced(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("seam hook error")
	db := openHookDB(t, &hexxladb.Options{
		AfterPutSeam: hexxladb.AfterPutSeamHookFunc(func(_ context.Context, _ record.SeamRecord) error {
			return sentinel
		}),
	})

	pkA := packCoord(t, 0, 0)
	pkB := packCoord(t, 1, 0)
	err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutSeam(t.Context(), record.SeamRecord{
			ID: newULID(), CellA: pkA, CellB: pkB, SeamType: "x",
		})
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error from seam hook, got %v", err)
	}
}

// TestHook_AfterPutSeam_MarkConflict verifies the seam hook fires via MarkConflict.
func TestHook_AfterPutSeam_MarkConflict(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	db := openHookDB(t, &hexxladb.Options{
		AfterPutSeam: hexxladb.AfterPutSeamHookFunc(func(_ context.Context, _ record.SeamRecord) error {
			calls.Add(1)
			return nil
		}),
	})

	coordA := hexxladb.Coord{Q: 0, R: 0}
	coordB := hexxladb.Coord{Q: 1, R: 0}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.MarkConflict(coordA, coordB, "reason")
	}); err != nil {
		t.Fatalf("MarkConflict: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 seam hook call from MarkConflict, got %d", got)
	}
}

// TestHook_BothHooks verifies cell and seam hooks can coexist.
func TestHook_BothHooks(t *testing.T) {
	t.Parallel()
	var cellCalls, seamCalls atomic.Int64
	db := openHookDB(t, &hexxladb.Options{
		AfterPutCell: hexxladb.AfterPutCellHookFunc(func(_ context.Context, _ record.CellRecord) error {
			cellCalls.Add(1)
			return nil
		}),
		AfterPutSeam: hexxladb.AfterPutSeamHookFunc(func(_ context.Context, _ record.SeamRecord) error {
			seamCalls.Add(1)
			return nil
		}),
	})
	ctx := t.Context()
	pkA := packCoord(t, 0, 0)
	pkB := packCoord(t, 1, 0)

	_ = db.Update(func(tx *hexxladb.Tx) error {
		_ = tx.PutCell(ctx, record.CellRecord{Key: pkA, RawContent: "a"})
		return tx.PutCell(ctx, record.CellRecord{Key: pkB, RawContent: "b"})
	})
	_ = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutSeam(ctx, record.SeamRecord{
			ID: newULID(), CellA: pkA, CellB: pkB, SeamType: "x",
		})
	})
	if c := cellCalls.Load(); c != 2 {
		t.Errorf("cell calls: want 2, got %d", c)
	}
	if s := seamCalls.Load(); s != 1 {
		t.Errorf("seam calls: want 1, got %d", s)
	}
}
