package engine

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEngineClosePreservesAllFailures(t *testing.T) {
	for _, mode := range []string{"wal", "primary", "both"} {
		t.Run(mode, func(t *testing.T) {
			e, err := Open(filepath.Join(t.TempDir(), "close.db"), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = e.Close() })
			var paths []string
			if mode != "primary" {
				paths = append(paths, e.wal.Name())
				if err := e.wal.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "wal" {
				paths = append(paths, e.db.Name())
				if err := e.db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			err = e.Close()
			if !errors.Is(err, os.ErrClosed) {
				t.Fatalf("close error = %v", err)
			}
			causes := []error{err}
			if len(paths) == 2 {
				joined, ok := err.(interface{ Unwrap() []error })
				if !ok {
					t.Fatalf("multiple close errors were not joined: %v", err)
				}
				causes = joined.Unwrap()
			} else if reflect.TypeOf(err) != reflect.TypeFor[*os.PathError]() {
				t.Fatalf("single close error changed type: %T", err)
			}
			if len(causes) != len(paths) {
				t.Fatalf("close causes = %d, want %d", len(causes), len(paths))
			}
			for i, cause := range causes {
				pathErr, ok := errors.AsType[*os.PathError](cause)
				if !ok || pathErr.Path != paths[i] || !errors.Is(cause, os.ErrClosed) {
					t.Fatalf("close cause %d = %v", i, cause)
				}
			}
			if e.wal != nil || e.db != nil {
				t.Fatal("close retained file handles")
			}
			if err := e.Close(); err != nil {
				t.Fatalf("repeated close = %v", err)
			}
		})
	}
}

func TestEngine_writeReadReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "e.db")
	e, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte{0x37}, DefaultPageSize)
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
	header, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	header.LastWALSeq, header.NextPageID = 1, 2
	marker := e.encodeHeaderWALRecord(header, 1)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	payload := bytes.Repeat([]byte{0x99}, DefaultPageSize)
	rec := encodeWALRecord(1, 1, payload, DefaultPageSize)
	if err := os.WriteFile(WalPath(path), append(marker, rec...), 0o600); err != nil {
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
