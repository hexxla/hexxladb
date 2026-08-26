package engine

import (
	"math/rand/v2"
	"path/filepath"
	"testing"
)

// incompressibleBytes returns n pseudo-random bytes that DEFLATE cannot shrink.
func incompressibleBytes(n int) []byte {
	rng := rand.New(rand.NewPCG(0xdeadbeef, 0xcafe))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

// TestBTree_LargeInlineValues_NoLeafPageFull is an integration regression test.
// It inserts entries with large inline values (~1050 bytes each) until the B+ tree
// must split leaves. Before the fix, this triggered "leaf page full" on the split.
func TestBTree_LargeInlineValues_NoLeafPageFull(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "btree_large.db")
	eng, err := Open(path, &Options{PageSize: 4096})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng.Close() }()

	bt := OpenBTree(eng)

	// Insert enough large-value entries to force multiple leaf splits.
	// Value is 1050 bytes of incompressible data — inline (under inlineThreshold ~3772
	// after failed compression) so it exercises the split path directly without overflow.
	val := incompressibleBytes(1050)
	for i := range 20 {
		key := make([]byte, 18)
		key[0] = byte(i >> 8)
		key[1] = byte(i)
		if err := bt.Put(key, val); err != nil {
			t.Fatalf("Put i=%d: %v", i, err)
		}
	}

	// Verify all keys are readable.
	for i := range 20 {
		key := make([]byte, 18)
		key[0] = byte(i >> 8)
		key[1] = byte(i)
		got, ok, err := bt.Get(key)
		if err != nil {
			t.Fatalf("Get i=%d: %v", i, err)
		}
		if !ok {
			t.Errorf("Get i=%d: not found", i)
		}
		if len(got) != len(val) {
			t.Errorf("Get i=%d: len=%d want %d", i, len(got), len(val))
		}
	}
}
