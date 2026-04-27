package engine

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
)

// TestParametricPageSize_putGetDelete exercises BTree operations at every valid page size.
func TestParametricPageSize_putGetDelete(t *testing.T) {
	t.Parallel()
	for _, ps := range validPageSizes {
		t.Run(fmt.Sprintf("%dB", ps), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "ps.db")
			e, err := Open(path, &Options{PageSize: ps, UseFormatV2: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e.Close() }()

			if e.PageSizeInt() != int(ps) {
				t.Fatalf("PageSizeInt: got %d want %d", e.PageSizeInt(), ps)
			}

			bt := OpenBTree(e)
			const n = 200
			for i := range n {
				k := []byte("key-" + strconv.Itoa(i))
				v := []byte("val-" + strconv.Itoa(i))
				if err := bt.Put(k, v); err != nil {
					t.Fatalf("put i=%d: %v", i, err)
				}
			}
			for i := range n {
				k := []byte("key-" + strconv.Itoa(i))
				want := []byte("val-" + strconv.Itoa(i))
				got, ok, err := bt.Get(k)
				if err != nil || !ok || !bytes.Equal(got, want) {
					t.Fatalf("get i=%d: ok=%v got=%s err=%v", i, ok, got, err)
				}
			}
			// Delete every other key.
			for i := 0; i < n; i += 2 {
				k := []byte("key-" + strconv.Itoa(i))
				if err := bt.Delete(k); err != nil {
					t.Fatalf("delete i=%d: %v", i, err)
				}
			}
			// Verify remaining keys.
			for i := range n {
				k := []byte("key-" + strconv.Itoa(i))
				_, ok, err := bt.Get(k)
				if err != nil {
					t.Fatalf("get-after-delete i=%d: %v", i, err)
				}
				if i%2 == 0 && ok {
					t.Fatalf("key %s should have been deleted", k)
				}
				if i%2 != 0 && !ok {
					t.Fatalf("key %s should still exist", k)
				}
			}
		})
	}
}

// TestParametricPageSize_reopenPreservesPageSize verifies that page size is persisted in header
// and correctly bootstrapped on reopen.
func TestParametricPageSize_reopenPreservesPageSize(t *testing.T) {
	t.Parallel()
	for _, ps := range validPageSizes {
		t.Run(fmt.Sprintf("%dB", ps), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "reopen.db")
			e, err := Open(path, &Options{PageSize: ps})
			if err != nil {
				t.Fatal(err)
			}
			if err := e.WritePage(1, bytes.Repeat([]byte{0xaa}, int(ps))); err != nil {
				t.Fatal(err)
			}
			if err := e.Close(); err != nil {
				t.Fatal(err)
			}

			// Reopen with nil opts — page size should be read from header.
			e2, err := Open(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e2.Close() }()
			if e2.PageSizeInt() != int(ps) {
				t.Fatalf("reopen PageSizeInt: got %d want %d", e2.PageSizeInt(), ps)
			}
			got, err := e2.ReadPage(1)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, bytes.Repeat([]byte{0xaa}, int(ps))) {
				t.Fatal("page content mismatch after reopen")
			}
		})
	}
}

// TestParametricPageSize_walReplay tests WAL replay at different page sizes.
func TestParametricPageSize_walReplay(t *testing.T) {
	t.Parallel()
	for _, ps := range validPageSizes {
		t.Run(fmt.Sprintf("%dB", ps), func(t *testing.T) {
			t.Parallel()
			payload := bytes.Repeat([]byte{0x42}, int(ps))
			rec := encodeWALRecord(1, 1, payload, int(ps))
			var applied int
			_, err := parseAndReplayWAL(rec, 0, func(seq, pageID uint64, p []byte) error {
				applied++
				if !bytes.Equal(p, payload) {
					t.Fatal("payload mismatch in WAL replay")
				}
				return nil
			}, int(ps))
			if err != nil {
				t.Fatal(err)
			}
			if applied != 1 {
				t.Fatalf("applied %d records, want 1", applied)
			}
		})
	}
}
