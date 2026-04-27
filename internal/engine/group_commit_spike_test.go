package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestSpike_twoWALRecordsOneSyncThenPersist exercises the safe pattern: append multiple
// redo records, sync the WAL once, then apply primary pages in sequence (same fsync
// pattern as WritePage after the shared WAL sync). This is the building block for future
// group commit if btree I/O can defer primary application until after a WAL batch.
func TestSpike_twoWALRecordsOneSyncThenPersist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "gc.db")

	e, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	p1 := bytes.Repeat([]byte{0x11}, e.pageSize)
	p2 := bytes.Repeat([]byte{0x22}, e.pageSize)

	rec1 := encodeWALRecordWithMAC(1, 1, p1, e.walMACKey, e.walMACEnabled, e.pageSize)
	rec2 := encodeWALRecordWithMAC(2, 2, p2, e.walMACKey, e.walMACEnabled, e.pageSize)

	if _, err := e.wal.Write(rec1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.wal.Write(rec2); err != nil {
		t.Fatal(err)
	}
	if err := e.wal.Sync(); err != nil {
		t.Fatal(err)
	}

	if err := e.persistRedoPage(1, 1, p1); err != nil {
		t.Fatal(err)
	}
	if err := e.persistRedoPage(2, 2, p2); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	e2, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e2.Close() })

	got1, err := e2.ReadPage(1)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := e2.ReadPage(2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got1, p1) {
		t.Fatal("page 1 mismatch")
	}
	if !bytes.Equal(got2, p2) {
		t.Fatal("page 2 mismatch")
	}
}

// TestReplay_restoresStalePrimaryWhenWALAhead simulates WAL containing a newer redo for a
// page than what is on disk (header still behind that record until replay): recovery must
// apply the trailing WAL record and refresh the primary.
func TestReplay_restoresStalePrimaryWhenWALAhead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wal_ahead.db")

	oldP := bytes.Repeat([]byte{0xaa}, DefaultPageSize)
	newP := bytes.Repeat([]byte{0xbb}, DefaultPageSize)

	e, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.WritePage(1, oldP); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	rec2 := encodeWALRecord(2, 1, newP, DefaultPageSize)
	wf, err := os.OpenFile(WalPath(path), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wf.Write(rec2); err != nil {
		_ = wf.Close()
		t.Fatal(err)
	}
	if err := wf.Close(); err != nil {
		t.Fatal(err)
	}

	e2, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e2.Close() }()

	got, err := e2.ReadPage(1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newP) {
		t.Fatal("expected replay of seq 2 to restore newer page payload")
	}
	if e2.LastWALSeq() != 2 {
		t.Fatalf("LastWALSeq want 2 got %d", e2.LastWALSeq())
	}
}
