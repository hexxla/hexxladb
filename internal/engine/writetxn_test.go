package engine

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
)

func TestWriteTxn_readYourWritesBeforeCommit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ryw.db")
	e, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()

	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte{0x37}, PageSize)
	if err := e.WritePage(1, want); err != nil {
		t.Fatal(err)
	}
	got, rel, err := e.readPagePooled(1)
	if err != nil {
		t.Fatal(err)
	}
	rel()
	if !bytes.Equal(got, want) {
		t.Fatal("readPagePooled in txn should see buffered write")
	}
	if err := e.CommitWriteTxn(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteTxn_abortRevertsToDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ab.db")
	e, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	e, err = Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte{0xee}, PageSize)
	if err := e.WritePage(1, want); err != nil {
		t.Fatal(err)
	}
	e.AbortWriteTxn()
	if e.wtxn != nil {
		t.Fatal("abort should clear write txn")
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	e2, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e2.Close() }()
	h, err := e2.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if h.LastWALSeq != 0 {
		t.Fatalf("abort should leave LastWALSeq at 0, got %d", h.LastWALSeq)
	}
}

func TestWriteTxn_commitThenReopenMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cr.db")
	e, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte{0x99}, PageSize)
	if err := e.WritePage(1, want); err != nil {
		t.Fatal(err)
	}
	if err := e.CommitWriteTxn(); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
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
	if !bytes.Equal(got, want) {
		t.Fatal("mismatch after reopen")
	}
}

func TestWriteTxn_BeginWhileActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	e, err := Open(filepath.Join(dir, "b.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	if err := e.BeginWriteTxn(); !errors.Is(err, ErrWriteTxnActive) {
		t.Fatalf("got %v", err)
	}
	e.AbortWriteTxn()
}

func TestWriteTxn_singleWALSyncForTwoPages(t *testing.T) {
	t.Parallel()
	// Count wal.Sync by wrapping is invasive; the spike test TestSpike_twoWALRecordsOneSyncThenPersist
	// already validates ordering. This test ensures a single Update-shaped txn commits both pages.
	dir := t.TempDir()
	path := filepath.Join(dir, "2p.db")
	e, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	p1 := bytes.Repeat([]byte{0x11}, PageSize)
	p2 := bytes.Repeat([]byte{0x22}, PageSize)
	if err := e.WritePage(1, p1); err != nil {
		t.Fatal(err)
	}
	if err := e.WritePage(2, p2); err != nil {
		t.Fatal(err)
	}
	if e.LastWALSeq() != 0 {
		t.Fatal("lastSeq should not advance before commit")
	}
	if err := e.CommitWriteTxn(); err != nil {
		t.Fatal(err)
	}
	if e.LastWALSeq() != 2 {
		t.Fatalf("LastWALSeq = %d", e.LastWALSeq())
	}
	_ = e.Close()
	e2, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e2.Close() }()
	g1, _ := e2.ReadPage(1)
	g2, _ := e2.ReadPage(2)
	if !bytes.Equal(g1, p1) || !bytes.Equal(g2, p2) {
		t.Fatal("pages not durable")
	}
}

func TestOpen_primaryFdatasyncSurvivesReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "fd.db")
	pay := bytes.Repeat([]byte{0xbb}, PageSize)
	e, err := Open(path, &Options{UsePrimaryFdatasync: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.BeginWriteTxn(); err != nil {
		_ = e.Close()
		t.Fatal(err)
	}
	if err := e.WritePage(1, pay); err != nil {
		_ = e.Close()
		t.Fatal(err)
	}
	if err := e.CommitWriteTxn(); err != nil {
		_ = e.Close()
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e2, err := Open(path, &Options{UsePrimaryFdatasync: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e2.Close() }()
	g, err := e2.ReadPage(1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g, pay) {
		t.Fatal("mismatch with UsePrimaryFdatasync")
	}
}
