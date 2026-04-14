package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEngine_writeReadReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "e.db")
	e, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte{0x37}, PageSize)
	if err := e.WritePage(1, want); err != nil {
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
	got, err := e2.ReadPage(1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("ReadPage mismatch after reopen")
	}
}

func TestOpen_replaysPendingWAL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wal.db")
	e, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	payload := bytes.Repeat([]byte{0x99}, PageSize)
	rec := encodeWALRecord(1, 1, payload)
	if err := os.WriteFile(WalPath(path), rec, 0o600); err != nil {
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
	if !bytes.Equal(got, payload) {
		t.Fatal("WAL replay did not restore page 1")
	}
}
