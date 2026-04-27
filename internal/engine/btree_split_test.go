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

// TestLeafSplitIndex_RightHalfFits verifies that leafSplitIndex returns a split
// point where both halves fit within pageSize. This is a regression test for
// the leaf-page-full bug: when large-ish inline values are distributed such
// that the right half after splitting still exceeds pageSize.
func TestLeafSplitIndex_RightHalfFits(t *testing.T) {
	t.Parallel()
	const pageSize = 4096

	// Construct a key/val set where the naive pageSize/2 threshold leaves the
	// right half overflowing. Each entry is ~1100 bytes (well under inlineThreshold
	// of ~3772, so stored inline), but two entries together = 2264 bytes > 2048
	// which is pageSize/2. With 4 such entries the total = 4464 > 4096 (triggers
	// split). With minKeysPerPage=2, leafSplitIndex must still ensure right fits.
	entryVal := incompressibleBytes(1050)
	entryKey := make([]byte, 18)

	const n = 4
	keys := make([][]byte, n)
	vals := make([][]byte, n)
	for i := range n {
		k := make([]byte, 18)
		k[17] = byte(i)
		keys[i] = k
		vals[i] = entryVal
	}

	// Verify the total does overflow (precondition for the split being triggered).
	total := leafSerializedSize(keys, vals)
	if total <= pageSize {
		t.Fatalf("precondition failed: total=%d should exceed pageSize=%d", total, pageSize)
	}

	mid := leafSplitIndex(keys, vals, pageSize)

	if mid < 1 || mid >= n {
		t.Fatalf("mid=%d out of range [1,%d)", mid, n)
	}

	leftSize := leafSerializedSize(keys[:mid], vals[:mid])
	rightSize := leafSerializedSize(keys[mid:], vals[mid:])

	if leftSize > pageSize {
		t.Errorf("left half size %d > pageSize %d (mid=%d, n=%d, entryVal=%d)",
			leftSize, pageSize, mid, n, len(entryVal))
	}
	if rightSize > pageSize {
		t.Errorf("right half size %d > pageSize %d (mid=%d, n=%d, entryVal=%d)",
			rightSize, pageSize, mid, n, len(entryVal))
	}
	_ = entryKey
}

// TestLeafSplitIndex_SingleLargeEntry covers the case where even 2 entries
// together overflow but each fits alone — mid must be 1.
func TestLeafSplitIndex_SingleLargeEntry(t *testing.T) {
	t.Parallel()
	const pageSize = 4096

	// Two entries each ~2100 bytes inline (under inlineThreshold ~3772 at 4096).
	// Together: 64 + 2*(4+18+2100) = 4508 > 4096. Each alone: 64+2122=2186 < 4096.
	entryVal := incompressibleBytes(2050)
	keys := [][]byte{make([]byte, 18), make([]byte, 18)}
	keys[1][17] = 1
	vals := [][]byte{entryVal, entryVal}

	total := leafSerializedSize(keys, vals)
	if total <= pageSize {
		t.Fatalf("precondition: total=%d should exceed pageSize=%d", total, pageSize)
	}

	mid := leafSplitIndex(keys, vals, pageSize)
	leftSize := leafSerializedSize(keys[:mid], vals[:mid])
	rightSize := leafSerializedSize(keys[mid:], vals[mid:])

	if leftSize > pageSize {
		t.Errorf("left size %d > pageSize %d", leftSize, pageSize)
	}
	if rightSize > pageSize {
		t.Errorf("right size %d > pageSize %d", rightSize, pageSize)
	}
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
