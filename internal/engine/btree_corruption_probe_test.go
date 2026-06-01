package engine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"
)

// probeEntrySize mirrors the on-disk per-entry cost used by leafSerializedSize.
func probeEntrySize(k, v []byte) int { return 4 + len(k) + len(v) }

// TestProbe_CascadingLeafSplit_AllPagesFit is the core validity probe for the
// suspected "leaf page full" corruption. It feeds cascadingLeafSplit entry sets
// generated the way the live engine does (a previously-fitting leaf page plus a
// single inserted entry, so total <= pageSize + maxEntry) and asserts the
// documented invariant: EVERY returned page must serialize within pageSize.
// A failure here reproduces the ErrCorruptTree("leaf page full") path in
// insertIntoLeafCascade.
//
// NOTE: cascadingLeafSplit is only ever called from insertIntoLeafCascade with
// the contents of one on-disk leaf (always <= pageSize) plus one new entry.
// Feeding it arbitrarily large inputs (e.g. 26 KiB) is not a real scenario and
// would exceed what any single split can rebalance; this probe models the real
// bound.
func TestProbe_CascadingLeafSplit_AllPagesFit(t *testing.T) {
	t.Parallel()
	for _, pageSize := range []int{4096, 8192, 16384, 65536} {
		t.Run(probeName(pageSize), func(t *testing.T) {
			t.Parallel()
			thr := inlineThreshold(pageSize) // max inline value size
			rng := rand.New(rand.NewPCG(uint64(pageSize), 2))
			for trial := range 20000 {
				keys, vals := probeFittingLeafPlusOne(rng, pageSize, thr)
				pages, err := cascadingLeafSplit(keys, vals, pageSize)
				if err != nil {
					t.Fatalf("trial %d: unexpected error: %v", trial, err)
				}
				for pi, p := range pages {
					sz := btreeHeaderSize
					for j := range p.keys {
						sz += probeEntrySize(p.keys[j], p.vals[j])
					}
					if sz > pageSize {
						t.Fatalf("trial %d: page %d serializes to %d > pageSize %d (entries=%d, pages=%d)",
							trial, pi, sz, pageSize, len(keys), len(pages))
					}
				}
			}
		})
	}
}

// probeName formats a subtest name for a page size.
func probeName(pageSize int) string { return fmt.Sprintf("page%d", pageSize) }

// probeFittingLeafPlusOne builds a key/value set that models a real overflow:
// a set of entries that fit within pageSize (a valid on-disk leaf), plus one
// additional entry that tips it over. Keys are strictly increasing.
func probeFittingLeafPlusOne(rng *rand.Rand, pageSize, thr int) (keys, vals [][]byte) {
	size := btreeHeaderSize
	idx := 0
	for {
		kl := 1 + rng.IntN(maxKeyBytes)
		vl := rng.IntN(thr + 1)
		k := make([]byte, max(kl, 3))
		k[0] = byte(idx >> 16)
		k[1] = byte(idx >> 8)
		k[2] = byte(idx)
		v := make([]byte, vl)
		entry := probeEntrySize(k, v)
		keys = append(keys, k)
		vals = append(vals, v)
		size += entry
		idx++
		// Stop once we have just crossed pageSize (the inserted entry that
		// triggers the split), with at least 2 entries.
		if size > pageSize && len(keys) >= 2 {
			return keys, vals
		}
		// Safety bound: a fitting leaf cannot hold more entries than this.
		if len(keys) > pageSize {
			return keys, vals
		}
	}
}

// TestProbe_PutAdversarialSizes_NoCorruptTree drives the full BTree.Put path
// with adversarial variable-size inline values and asserts no ErrCorruptTree is
// returned and all data round-trips. This exercises the live insert path
// (insertAt -> insertLeafWithCascade -> cascadingLeafSplit).
func TestProbe_PutAdversarialSizes_NoCorruptTree(t *testing.T) {
	t.Parallel()
	const pageSize = 4096
	eng := openTestDB(t, &Options{PageSize: pageSize})
	bt := OpenBTree(eng)
	thr := inlineThreshold(pageSize)

	rng := rand.New(rand.NewPCG(7, 11))
	const n = 5000
	want := make(map[string]int, n)
	for i := range n {
		key := fmt.Appendf(nil, "k%012d", rng.IntN(n)) // collisions force updates
		// Alternate tiny and near-threshold values to stress the split point.
		var vl int
		if i%2 == 0 {
			vl = thr - rng.IntN(64)
		} else {
			vl = 1 + rng.IntN(32)
		}
		val := make([]byte, vl)
		val[0] = byte(i)
		if err := bt.Put(key, val); err != nil {
			if errors.Is(err, ErrCorruptTree) {
				t.Fatalf("i=%d: REPRODUCED ErrCorruptTree on Put(len=%d): %v", i, vl, err)
			}
			t.Fatalf("i=%d: Put failed: %v", i, err)
		}
		want[string(key)] = vl
	}

	for k, vl := range want {
		got, ok, err := bt.Get([]byte(k))
		if err != nil {
			t.Fatalf("Get(%q): %v", k, err)
		}
		if !ok {
			t.Fatalf("Get(%q): missing", k)
		}
		if len(got) != vl {
			t.Fatalf("Get(%q): len=%d want %d", k, len(got), vl)
		}
	}
}

// TestProbe_PutLargeKeysNearThreshold targets the theoretical worst case for a
// no-valid-split leaf: maximum-size keys combined with near-threshold values,
// which maximizes per-entry size and shrinks the valid split window.
func TestProbe_PutLargeKeysNearThreshold(t *testing.T) {
	t.Parallel()
	for _, pageSize := range []int{4096, 8192, 16384, 65536} {
		t.Run(fmt.Sprintf("page%d", pageSize), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "probe.db")
			eng, err := Open(path, &Options{PageSize: uint32(pageSize), MaxValueBytes: 65536})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = eng.Close() }()
			bt := OpenBTree(eng)
			thr := inlineThreshold(pageSize)

			for i := range 400 {
				key := make([]byte, maxKeyBytes)
				key[0] = byte(i >> 8)
				key[1] = byte(i)
				val := make([]byte, thr) // exactly at inline threshold
				if err := bt.Put(key, val); err != nil {
					if errors.Is(err, ErrCorruptTree) {
						t.Fatalf("page=%d i=%d: REPRODUCED ErrCorruptTree (key=%d val=%d): %v",
							pageSize, i, maxKeyBytes, thr, err)
					}
					t.Fatalf("page=%d i=%d: Put failed: %v", pageSize, i, err)
				}
			}
		})
	}
}

// TestProbe_CompressMagicCollision is a regression test for values whose first
// byte equals the compression magic (0xFE). Such values are stored raw when they
// do not compress, but were previously misread as a compression envelope on read
// and failed with "flate: corrupt input". They must round-trip byte-for-byte.
func TestProbe_CompressMagicCollision(t *testing.T) {
	t.Parallel()
	eng := openTestDB(t, &Options{PageSize: 4096})
	bt := OpenBTree(eng)

	rng := rand.New(rand.NewPCG(99, 100))
	// Cover the boundary around compressHeaderSize and compressMinInput.
	sizes := []int{6, 7, 20, 63, 64, 65, 200, 1000}
	for si, n := range sizes {
		// Incompressible payload starting with the compression magic byte.
		val := make([]byte, n)
		val[0] = compressMagic
		for j := 1; j < n; j++ {
			val[j] = byte(rng.Uint32())
		}
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(si))
		if err := bt.Put(key, val); err != nil {
			t.Fatalf("size=%d: Put: %v", n, err)
		}
		got, ok, err := bt.Get(key)
		if err != nil {
			t.Fatalf("size=%d: Get: %v", n, err)
		}
		if !ok {
			t.Fatalf("size=%d: Get: missing", n)
		}
		if !bytes.Equal(got, val) {
			t.Fatalf("size=%d: round-trip mismatch", n)
		}
	}
}
