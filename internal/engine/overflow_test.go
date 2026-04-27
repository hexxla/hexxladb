package engine

import (
	"bytes"
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"
)

func openTestDB(t *testing.T, opts *Options) *Engine {
	t.Helper()
	dir := t.TempDir()
	eng, err := Open(filepath.Join(dir, "overflow.db"), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

func TestOverflow_InlineThreshold(t *testing.T) {
	t.Parallel()
	for _, ps := range validPageSizes {
		got := inlineThreshold(int(ps))
		if got < 64 {
			t.Errorf("inlineThreshold(%d) = %d; want >= 64", ps, got)
		}
		// Must be less than pageSize itself.
		if got >= int(ps) {
			t.Errorf("inlineThreshold(%d) = %d; want < %d", ps, got, ps)
		}
	}
}

func TestOverflow_StubRoundTrip(t *testing.T) {
	t.Parallel()
	stub := encodeOverflowStub(123456, 42)
	if !isOverflowStub(stub) {
		t.Fatal("isOverflowStub returned false for valid stub")
	}
	logLen, firstPage := decodeOverflowStub(stub)
	if logLen != 123456 {
		t.Fatalf("logicalLen: got %d want 123456", logLen)
	}
	if firstPage != 42 {
		t.Fatalf("firstPageID: got %d want 42", firstPage)
	}
}

func TestOverflow_NotStub(t *testing.T) {
	t.Parallel()
	// Too short.
	if isOverflowStub([]byte{0x01, 2, 3}) {
		t.Fatal("short slice should not be a stub")
	}
	// Wrong marker.
	bad := make([]byte, overflowStubLen)
	bad[0] = 0x00
	if isOverflowStub(bad) {
		t.Fatal("wrong marker should not be a stub")
	}
}

func TestOverflow_PutGetRoundTrip(t *testing.T) {
	t.Parallel()
	for _, ps := range validPageSizes {
		t.Run(pageSizeName(ps), func(t *testing.T) {
			t.Parallel()
			eng := openTestDB(t, &Options{PageSize: ps, MaxValueBytes: 131072})
			bt := &BTree{eng: eng}

			key := []byte("overflow-key")
			valSize := inlineThreshold(int(ps)) + 500
			val := make([]byte, valSize)
			if _, err := rand.Read(val); err != nil {
				t.Fatal(err)
			}

			if err := bt.Put(key, val); err != nil {
				t.Fatalf("Put: %v", err)
			}

			got, ok, err := bt.Get(key)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if !ok {
				t.Fatal("Get: not found")
			}
			if !bytes.Equal(got, val) {
				t.Fatalf("Get: value mismatch; got %d bytes, want %d", len(got), len(val))
			}
		})
	}
}

func TestOverflow_MultiPageChain(t *testing.T) {
	t.Parallel()
	// Use smallest page size to force more overflow pages.
	eng := openTestDB(t, &Options{PageSize: DefaultPageSize, MaxValueBytes: 131072})
	bt := &BTree{eng: eng}

	key := []byte("big-val")
	// 50 KiB value → multiple overflow pages at 4 KiB page size.
	val := []byte(strings.Repeat("X", 50*1024))

	if err := bt.Put(key, val); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok, err := bt.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get: not found")
	}
	if !bytes.Equal(got, val) {
		t.Fatalf("Get: mismatch; got %d bytes, want %d", len(got), len(val))
	}
}

func TestOverflow_Overwrite(t *testing.T) {
	t.Parallel()
	eng := openTestDB(t, &Options{PageSize: DefaultPageSize, MaxValueBytes: 65536})
	bt := &BTree{eng: eng}

	key := []byte("overwrite-key")
	threshold := inlineThreshold(int(DefaultPageSize))

	// First: overflow value.
	val1 := make([]byte, threshold+100)
	for i := range val1 {
		val1[i] = 'A'
	}
	if err := bt.Put(key, val1); err != nil {
		t.Fatal(err)
	}

	// Second: different overflow value (old chain freed).
	val2 := make([]byte, threshold+200)
	for i := range val2 {
		val2[i] = 'B'
	}
	if err := bt.Put(key, val2); err != nil {
		t.Fatal(err)
	}

	got, ok, err := bt.Get(key)
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(got, val2) {
		t.Fatalf("overwrite: got %d bytes starting with %c, want %d bytes starting with B", len(got), got[0], len(val2))
	}
}

func TestOverflow_OverwriteInlineToOverflow(t *testing.T) {
	t.Parallel()
	eng := openTestDB(t, &Options{PageSize: DefaultPageSize, MaxValueBytes: 65536})
	bt := &BTree{eng: eng}

	key := []byte("grow-key")

	// Start inline.
	if err := bt.Put(key, []byte("small")); err != nil {
		t.Fatal(err)
	}

	// Overwrite with overflow-sized value.
	big := make([]byte, inlineThreshold(int(DefaultPageSize))+100)
	for i := range big {
		big[i] = 'Z'
	}
	if err := bt.Put(key, big); err != nil {
		t.Fatal(err)
	}

	got, ok, err := bt.Get(key)
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(got, big) {
		t.Fatal("inline→overflow overwrite mismatch")
	}
}

func TestOverflow_Delete(t *testing.T) {
	t.Parallel()
	eng := openTestDB(t, &Options{PageSize: DefaultPageSize, MaxValueBytes: 65536})
	bt := &BTree{eng: eng}

	key := []byte("del-key")
	val := make([]byte, inlineThreshold(int(DefaultPageSize))+100)
	for i := range val {
		val[i] = 'D'
	}

	if err := bt.Put(key, val); err != nil {
		t.Fatal(err)
	}
	if err := bt.Delete(key); err != nil {
		t.Fatal(err)
	}

	_, ok, err := bt.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("key still found after delete")
	}
}

func TestOverflow_AscendRange(t *testing.T) {
	t.Parallel()
	eng := openTestDB(t, &Options{PageSize: DefaultPageSize, MaxValueBytes: 65536})
	bt := &BTree{eng: eng}

	threshold := inlineThreshold(int(DefaultPageSize))

	// Insert mix of inline and overflow entries.
	inlineKey := []byte("aaa")
	inlineVal := []byte("small")
	overflowKey := []byte("bbb")
	overflowVal := make([]byte, threshold+50)
	for i := range overflowVal {
		overflowVal[i] = 'O'
	}

	if err := bt.Put(inlineKey, inlineVal); err != nil {
		t.Fatal(err)
	}
	if err := bt.Put(overflowKey, overflowVal); err != nil {
		t.Fatal(err)
	}

	var count int
	err := bt.AscendRange(nil, nil, func(k, v []byte) bool {
		count++
		if bytes.Equal(k, overflowKey) {
			if !bytes.Equal(v, overflowVal) {
				t.Errorf("AscendRange: overflow value mismatch; got %d bytes want %d", len(v), len(overflowVal))
			}
		} else if bytes.Equal(k, inlineKey) {
			if !bytes.Equal(v, inlineVal) {
				t.Errorf("AscendRange: inline value mismatch")
			}
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("AscendRange: got %d entries want 2", count)
	}
}

func TestOverflow_ManyKeys(t *testing.T) {
	t.Parallel()
	eng := openTestDB(t, &Options{PageSize: DefaultPageSize, MaxValueBytes: 65536})
	bt := &BTree{eng: eng}

	threshold := inlineThreshold(int(DefaultPageSize))
	n := 20
	type kv struct {
		key, val []byte
	}
	entries := make([]kv, n)
	for i := range n {
		key := []byte{byte('a' + i)}
		val := make([]byte, threshold+100+i*50)
		for j := range val {
			val[j] = byte('A' + i%26)
		}
		entries[i] = kv{key, val}
		if err := bt.Put(key, val); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	for _, e := range entries {
		got, ok, err := bt.Get(e.key)
		if err != nil || !ok {
			t.Fatalf("Get(%q): ok=%v err=%v", e.key, ok, err)
		}
		if !bytes.Equal(got, e.val) {
			t.Fatalf("Get(%q): mismatch; got %d bytes want %d", e.key, len(got), len(e.val))
		}
	}
}

func pageSizeName(ps uint32) string {
	switch ps {
	case 4096:
		return "4K"
	case 8192:
		return "8K"
	case 16384:
		return "16K"
	case 65536:
		return "64K"
	default:
		return "unknown"
	}
}
