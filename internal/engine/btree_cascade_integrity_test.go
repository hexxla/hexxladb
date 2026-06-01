package engine

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"
)

// treeStats accumulates structural facts gathered during a full tree walk.
type treeStats struct {
	leafDepths []int
	keys       [][]byte // collected in in-order DFS (must be globally sorted)
	pages      int
}

// validateTree walks the entire B+ tree from the root and asserts every
// production invariant the cascading-split fix must uphold:
//   - every page parses and serializes within pageSize,
//   - every internal node has len(ptrs) == len(keys)+1,
//   - every child's on-disk parent pointer points at its actual parent
//     (catches orphaned pages from an incomplete cascade),
//   - all leaves sit at the same depth (balanced),
//   - keys are globally sorted and unique (in-order DFS order).
//
// It returns the number of keys discovered via top-down traversal.
func validateTree(t *testing.T, bt *BTree) int {
	t.Helper()
	hdr, err := bt.eng.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if hdr.BTreeRoot == 0 {
		return 0
	}
	st := &treeStats{}
	walkValidate(t, bt, hdr.BTreeRoot, 0, 0, st)

	// All leaves must be at the same depth.
	for i := 1; i < len(st.leafDepths); i++ {
		if st.leafDepths[i] != st.leafDepths[0] {
			t.Fatalf("unbalanced tree: leaf depths %d != %d", st.leafDepths[i], st.leafDepths[0])
		}
	}
	// Keys must be strictly ascending across the whole tree.
	for i := 1; i < len(st.keys); i++ {
		if bytes.Compare(st.keys[i-1], st.keys[i]) >= 0 {
			t.Fatalf("key order violated at %d: %x >= %x", i, st.keys[i-1], st.keys[i])
		}
	}
	return len(st.keys)
}

func walkValidate(t *testing.T, bt *BTree, pid, parent uint64, depth int, st *treeStats) {
	t.Helper()
	page, release, err := bt.eng.readPagePooled(pid)
	if err != nil {
		t.Fatalf("read page %d: %v", pid, err)
	}
	st.pages++
	gotParent := binary.BigEndian.Uint64(page[16:24])
	if gotParent != parent {
		release()
		t.Fatalf("page %d parent pointer = %d, want %d (orphaned/mislinked)", pid, gotParent, parent)
	}

	if page[5] == btreeKindLeaf {
		ld, perr := parseLeafPage(page)
		release()
		if perr != nil {
			t.Fatalf("parse leaf %d: %v", pid, perr)
		}
		if sz := leafSerializedSize(ld.keys, ld.vals); sz > bt.pageSize() {
			t.Fatalf("leaf %d serializes to %d > pageSize %d", pid, sz, bt.pageSize())
		}
		st.leafDepths = append(st.leafDepths, depth)
		for _, k := range ld.keys {
			st.keys = append(st.keys, append([]byte(nil), k...))
		}
		return
	}

	in, perr := parseInternalPage(page)
	release()
	if perr != nil {
		t.Fatalf("parse internal %d: %v", pid, perr)
	}
	if len(in.ptrs) != len(in.keys)+1 {
		t.Fatalf("internal %d: %d ptrs, %d keys (want ptrs=keys+1)", pid, len(in.ptrs), len(in.keys))
	}
	if sz := internalSerializedSize(in.keys); sz > bt.pageSize() {
		t.Fatalf("internal %d serializes to %d > pageSize %d", pid, sz, bt.pageSize())
	}
	for _, c := range in.ptrs {
		walkValidate(t, bt, c, pid, depth+1, st)
	}
}

// putGetProbe inserts entries through the full BTree.Put path, then verifies
// every key is reachable top-down (Get), the tree is structurally valid, and an
// in-order scan returns exactly the inserted set.
func putGetProbe(t *testing.T, pageSize int, gen func(i int) (key, val []byte), n int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "integrity.db")
	eng, err := Open(path, &Options{PageSize: uint32(pageSize), MaxValueBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	bt := OpenBTree(eng)

	want := make(map[string][]byte, n)
	for i := range n {
		k, v := gen(i)
		if err := bt.Put(k, v); err != nil {
			t.Fatalf("Put i=%d (key=%dB val=%dB): %v", i, len(k), len(v), err)
		}
		want[string(k)] = append([]byte(nil), v...)
	}

	// Structural validation (balanced, no orphans, all pages fit).
	got := validateTree(t, bt)
	if got != len(want) {
		t.Fatalf("top-down key count = %d, want %d (orphaned pages?)", got, len(want))
	}

	// Top-down reachability: every key resolvable via Get.
	for k, v := range want {
		gv, ok, err := bt.Get([]byte(k))
		if err != nil {
			t.Fatalf("Get(%x): %v", k, err)
		}
		if !ok {
			t.Fatalf("Get(%x): missing (unreachable from root)", k)
		}
		if !bytes.Equal(gv, v) {
			t.Fatalf("Get(%x): value mismatch len got=%d want=%d", k, len(gv), len(v))
		}
	}

	// In-order scan must return exactly the inserted set, sorted.
	var scanned int
	var last []byte
	if err := bt.AscendRange(nil, nil, func(k, v []byte) bool {
		if last != nil && bytes.Compare(last, k) >= 0 {
			t.Fatalf("scan order violated: %x >= %x", last, k)
		}
		last = append([]byte(nil), k...)
		if _, ok := want[string(k)]; !ok {
			t.Fatalf("scan returned unknown key %x", k)
		}
		scanned++
		return true
	}); err != nil {
		t.Fatalf("AscendRange: %v", err)
	}
	if scanned != len(want) {
		t.Fatalf("scan returned %d keys, want %d", scanned, len(want))
	}

	// Persistence: reopen and re-verify a sample + structure.
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}
	eng2, err := Open(path, &Options{PageSize: uint32(pageSize), MaxValueBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng2.Close() }()
	bt2 := OpenBTree(eng2)
	if got := validateTree(t, bt2); got != len(want) {
		t.Fatalf("after reopen: key count = %d, want %d", got, len(want))
	}
}

// TestCascadeIntegrity_SequentialLargeValues drives ascending-key inserts with
// large inline values, forcing deep multi-level splits and root growth.
func TestCascadeIntegrity_SequentialLargeValues(t *testing.T) {
	t.Parallel()
	val := incompressibleBytes(1200) // inline at 4096 (< inlineThreshold ~1365)
	putGetProbe(t, 4096, func(i int) ([]byte, []byte) {
		k := make([]byte, 12)
		binary.BigEndian.PutUint64(k[2:10], uint64(i))
		return k, val
	}, 4000)
}

// TestCascadeIntegrity_RandomHighVariance stresses the greedy split with random
// keys and wildly varying value sizes (tiny up to near the inline threshold).
func TestCascadeIntegrity_RandomHighVariance(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(0xABCD, 0x1234))
	thr := inlineThreshold(4096)
	putGetProbe(t, 4096, func(i int) ([]byte, []byte) {
		// Unique 16-byte key derived from i, plus randomized high bytes.
		k := make([]byte, 16)
		binary.BigEndian.PutUint64(k[0:8], rng.Uint64())
		binary.BigEndian.PutUint64(k[8:16], uint64(i)) // guarantees uniqueness
		vl := 1 + rng.IntN(thr)
		v := make([]byte, vl)
		for j := range v {
			v[j] = byte(rng.Uint32())
		}
		return k, v
	}, 3000)
}

// TestCascadeIntegrity_MaxKeysNearThreshold combines maximum-size keys with
// near-threshold values — the worst case for the valid-split window — across
// every supported page size.
func TestCascadeIntegrity_MaxKeysNearThreshold(t *testing.T) {
	t.Parallel()
	for _, ps := range validPageSizes {
		ps := int(ps)
		t.Run(fmt.Sprintf("page%d", ps), func(t *testing.T) {
			t.Parallel()
			thr := inlineThreshold(ps)
			val := incompressibleBytes(thr)
			putGetProbe(t, ps, func(i int) ([]byte, []byte) {
				k := make([]byte, maxKeyBytes)
				binary.BigEndian.PutUint64(k[0:8], uint64(i))
				return k, val
			}, 600)
		})
	}
}
