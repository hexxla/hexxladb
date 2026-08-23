package hexxladb_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func openMaxValDB(t *testing.T, opts *hexxladb.Options) *hexxladb.DB {
	t.Helper()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "mvb.db"), opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func putMaxValCell(t *testing.T, db *hexxladb.DB, q, r int, content string) error {
	t.Helper()
	pk, err := lattice.Pack(lattice.Coord{Q: q, R: r})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	return db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: pk, RawContent: content})
	})
}

// TestMaxValueBytes_DefaultIs8192 verifies the default limit is 8192.
func TestMaxValueBytes_DefaultIs8192(t *testing.T) {
	t.Parallel()
	db := openMaxValDB(t, nil)
	if got := db.MaxValueBytes(); got != 8192 {
		t.Errorf("expected 8192, got %d", got)
	}
}

// TestMaxValueBytes_ExplicitLimit sets 2048 and verifies DB.MaxValueBytes reflects it.
func TestMaxValueBytes_ExplicitLimit(t *testing.T) {
	t.Parallel()
	db := openMaxValDB(t, &hexxladb.Options{MaxValueBytes: 2048})
	if got := db.MaxValueBytes(); got != 2048 {
		t.Errorf("expected 2048, got %d", got)
	}
}

// TestMaxValueBytes_InvalidRejected verifies unsupported sizes return ErrInvalidArgument.
func TestMaxValueBytes_InvalidRejected(t *testing.T) {
	t.Parallel()
	for _, bad := range []uint32{1, 100, 1000, 3000, 9000, 2_000_000} {
		_, err := hexxladb.Open(filepath.Join(t.TempDir(), "bad.db"), &hexxladb.Options{MaxValueBytes: bad})
		if !errors.Is(err, hexxladb.ErrInvalidArgument) {
			t.Errorf("MaxValueBytes=%d: expected ErrInvalidArgument, got %v", bad, err)
		}
		if err == nil || !strings.Contains(err.Error(), "1048576") {
			t.Errorf("MaxValueBytes=%d: diagnostic omits accepted values: %v", bad, err)
		}
	}
}

// TestMaxValueBytes_AllValidValues exercises every accepted value.
func TestMaxValueBytes_AllValidValues(t *testing.T) {
	t.Parallel()
	for _, v := range []uint32{512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, 1048576} {
		t.Run("limit", func(t *testing.T) {
			t.Parallel()
			db := openMaxValDB(t, &hexxladb.Options{MaxValueBytes: v})
			if got := db.MaxValueBytes(); got != v {
				t.Errorf("MaxValueBytes=%d: got %d", v, got)
			}
		})
	}
}

// TestMaxValueBytes_EnforcedOnWrite verifies a value larger than the limit is rejected.
func TestMaxValueBytes_EnforcedOnWrite(t *testing.T) {
	t.Parallel()
	db := openMaxValDB(t, &hexxladb.Options{MaxValueBytes: 512})

	// Build a cell record with encoded size > 512 bytes.
	content := strings.Repeat("x", 600)
	err := putMaxValCell(t, db, 0, 0, content)
	if err == nil {
		t.Fatal("expected error for oversized value, got nil")
	}
}

// TestMaxValueBytes_WithinLimitSucceeds verifies writes within the limit succeed.
func TestMaxValueBytes_WithinLimitSucceeds(t *testing.T) {
	t.Parallel()
	db := openMaxValDB(t, &hexxladb.Options{MaxValueBytes: 2048})
	if err := putMaxValCell(t, db, 0, 0, strings.Repeat("a", 100)); err != nil {
		t.Fatalf("expected success for small value, got: %v", err)
	}
}

// TestMaxValueBytes_PersistedAcrossReopen verifies the limit survives close/reopen.
func TestMaxValueBytes_PersistedAcrossReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	db1, err := hexxladb.Open(path, &hexxladb.Options{MaxValueBytes: 1024})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db2, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer func() { _ = db2.Close() }()

	if got := db2.MaxValueBytes(); got != 1024 {
		t.Errorf("after reopen: expected 1024, got %d", got)
	}
}

// TestMaxValueBytes_UpdateExistingDB verifies changing the limit on an existing db persists.
func TestMaxValueBytes_UpdateExistingDB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "update.db")

	db1, err := hexxladb.Open(path, &hexxladb.Options{MaxValueBytes: 2048})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen with a different limit.
	db2, err := hexxladb.Open(path, &hexxladb.Options{MaxValueBytes: 4096})
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if got := db2.MaxValueBytes(); got != 4096 {
		t.Errorf("after update: expected 4096, got %d", got)
	}
	_ = db2.Close()

	// Verify the updated limit is persisted.
	db3, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatalf("Second reopen: %v", err)
	}
	defer func() { _ = db3.Close() }()
	if got := db3.MaxValueBytes(); got != 4096 {
		t.Errorf("after second reopen: expected 4096, got %d", got)
	}
}

// TestMaxValueBytes_ClosedDBReturnsZero verifies MaxValueBytes on a nil/closed DB returns 0.
func TestMaxValueBytes_ClosedDBReturnsZero(t *testing.T) {
	t.Parallel()
	var db *hexxladb.DB
	if got := db.MaxValueBytes(); got != 0 {
		t.Errorf("nil DB: expected 0, got %d", got)
	}
}
