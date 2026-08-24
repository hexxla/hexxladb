package engine

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// TestBTree_hexxladbLatticeChurnPruneShape matches hexxladb integration: WalkRings R=4 (61 coords),
// one transaction seeds all cells at seq 1, then 249 updates only on the center cell (seq 2..250),
// each commit writing __meta/commit-time then cell keys (see [Tx] MVCC ordering). Finally delete
// stale center versions like [hexxladb.DB.PruneCellVersions] (cell prefix scan, seq < beforeSeq).
func TestBTree_hexxladbLatticeChurnPruneShape(t *testing.T) {
	t.Parallel()
	const fillR = 4
	const lastVersion = 250
	const retainBehind uint64 = 8

	c0 := lattice.Coord{Q: 0, R: 0}
	p0, err := lattice.Pack(c0)
	if err != nil {
		t.Fatal(err)
	}
	coords := lattice.WalkRings(nil, c0, fillR)
	if len(coords) != 61 {
		t.Fatalf("coords: %d", len(coords))
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "lattice_churn.db")
	e, err := Open(path, &Options{UseFormatV2: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := OpenBTree(e)

	// Commit 1: meta + all 61 cells at seq 1.
	{
		wall := int64(1 * 1e9)
		mk := index.CommitTimeKey(wall, 1)
		if err := bt.Put(mk, nil); err != nil {
			t.Fatal(err)
		}
		for _, coord := range coords {
			packed, err := lattice.Pack(coord)
			if err != nil {
				t.Fatal(err)
			}
			ck := index.CellKeyWithVersion(packed, 1)
			if err := bt.Put(ck, []byte("v1")); err != nil {
				t.Fatal(err)
			}
		}
	}
	for v := uint64(2); v <= lastVersion; v++ {
		wall := int64(v * 1e9)
		mk := index.CommitTimeKey(wall, v)
		if err := bt.Put(mk, nil); err != nil {
			t.Fatalf("meta v=%d: %v", v, err)
		}
		ck := index.CellKeyWithVersion(p0, v)
		if err := bt.Put(ck, []byte("x")); err != nil {
			t.Fatalf("cell v=%d: %v", v, err)
		}
	}

	beforeSeq := lastVersion - retainBehind // 242
	// Latest per logical cell (cell prefix); then collect stale center versions only.
	latest := make(map[lattice.PackedCoord]uint64)
	err = bt.AscendRange([]byte(index.CellPrefix), nil, func(k, _ []byte) bool {
		p, seq, err := index.ParseCellVersionKey(k)
		if err != nil {
			return true
		}
		if cur, ok := latest[p]; !ok || seq > cur {
			latest[p] = seq
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	var toDelete [][]byte
	err = bt.AscendRange([]byte(index.CellPrefix), nil, func(k, _ []byte) bool {
		p, seq, err := index.ParseCellVersionKey(k)
		if err != nil {
			return true
		}
		if seq < beforeSeq && seq != latest[p] {
			toDelete = append(toDelete, append([]byte(nil), k...))
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range toDelete {
		if err := bt.Delete(k); err != nil {
			_, seq, _ := index.ParseCellVersionKey(k)
			t.Fatalf("delete seq=%d: %v", seq, err)
		}
	}

	got, ok, err := bt.Get(index.CellKeyWithVersion(p0, lastVersion))
	if err != nil || !ok {
		t.Fatalf("latest center: ok=%v err=%v", ok, err)
	}
	if string(got) != "x" {
		t.Fatalf("latest value: %s", got)
	}
}

func TestBTree_putGet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	e, err := Open(path, &Options{UseFormatV2: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := OpenBTree(e)
	if err := bt.Put([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	v, ok, err := bt.Get([]byte("a"))
	if err != nil || !ok || string(v) != "1" {
		t.Fatalf("Get: ok=%v v=%s err=%v", ok, v, err)
	}
}

func TestPointLookupHelpersMatchDecodedPages(t *testing.T) {
	t.Parallel()
	keys := [][]byte{[]byte("alpha"), []byte("bravo"), []byte("charlie")}
	values := [][]byte{[]byte("one"), []byte("two"), []byte("three")}
	leaf, err := buildLeafPage(DefaultPageSize, 0, 0, keys, values)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range append(slices.Clone(keys), []byte("missing")) {
		value, ok, err := lookupLeafValue(leaf, key)
		if err != nil {
			t.Fatal(err)
		}
		index := leafKeyIndex(keys, key)
		wantOK := index < len(keys) && bytes.Equal(keys[index], key)
		if ok != wantOK {
			t.Fatalf("leaf lookup %q: ok=%v want=%v", key, ok, wantOK)
		}
		if ok && !bytes.Equal(value, values[index]) {
			t.Fatalf("leaf lookup %q: value=%q want=%q", key, value, values[index])
		}
	}

	internal, err := buildInternalPage(DefaultPageSize, 0, []uint64{11, 22, 33, 44}, keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range [][]byte{[]byte("aardvark"), []byte("alpha"), []byte("beta"), []byte("charlie"), []byte("zulu")} {
		child, err := lookupInternalChild(internal, key)
		if err != nil {
			t.Fatal(err)
		}
		want := []uint64{11, 22, 33, 44}[internalPickChild(keys, key)]
		if child != want {
			t.Fatalf("internal lookup %q: child=%d want=%d", key, child, want)
		}
	}
}

func TestBTree_manySplits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "big.db")
	e, err := Open(path, &Options{UseFormatV2: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := OpenBTree(e)
	for i := range 500 {
		k := []byte("k" + strconv.Itoa(i))
		v := []byte("v" + strconv.Itoa(i))
		if err := bt.Put(k, v); err != nil {
			t.Fatalf("i=%d: %v", i, err)
		}
	}
	for i := range 500 {
		k := []byte("k" + strconv.Itoa(i))
		want := []byte("v" + strconv.Itoa(i))
		got, ok, err := bt.Get(k)
		if err != nil || !ok || !bytes.Equal(got, want) {
			t.Fatalf("i=%d ok=%v got=%s err=%v", i, ok, got, err)
		}
	}
}

func TestBTree_ascendRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "r.db")
	e, err := Open(path, &Options{UseFormatV2: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := OpenBTree(e)
	for _, s := range []string{"b", "d", "a", "c"} {
		if err := bt.Put([]byte(s), []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	var out []string
	err = bt.AscendRange([]byte("a"), []byte("c"), func(k, v []byte) bool {
		out = append(out, string(k))
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c"}
	if len(out) != len(want) {
		t.Fatalf("got %v want %v", out, want)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("got %v want %v", out, want)
		}
	}
}

func TestBTree_descendRange(t *testing.T) {
	t.Parallel()
	e, err := Open(filepath.Join(t.TempDir(), "descend.db"), &Options{UseFormatV2: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := OpenBTree(e)
	for i := range 500 {
		key := []byte(fmt.Sprintf("k%04d", i))
		if err := bt.Put(key, key); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	var out []string
	err = bt.DescendRange([]byte("k0123"), []byte("k0378"), func(k, v []byte) bool {
		if !bytes.Equal(k, v) {
			t.Fatalf("key/value mismatch: %q != %q", k, v)
		}
		out = append(out, string(k))
		return len(out) < 5
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"k0378", "k0377", "k0376", "k0375", "k0374"}
	if !slices.Equal(out, want) {
		t.Fatalf("got %v want %v", out, want)
	}
}

func TestBTree_delete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "del.db")
	e, err := Open(path, &Options{UseFormatV2: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := OpenBTree(e)
	for i := range 120 {
		k := []byte("k" + strconv.Itoa(i))
		v := []byte("v" + strconv.Itoa(i))
		if err := bt.Put(k, v); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	for i := range 120 {
		k := []byte("k" + strconv.Itoa(i))
		if err := bt.Delete(k); err != nil {
			t.Fatalf("delete %d: %v", i, err)
		}
	}
	hdr, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.BTreeRoot != 0 {
		t.Fatalf("expected empty tree, root=%d", hdr.BTreeRoot)
	}
}

// TestBTree_mvccShapedSequentialDelete stresses delete/rebalance where keys share a long
// common prefix and differ only in the final 8 bytes (like MVCC physical cell keys).
func TestBTree_mvccShapedSequentialDelete(t *testing.T) {
	t.Parallel()
	const n = 273
	p := lattice.PackedCoord{3, 9}
	dir := t.TempDir()
	path := filepath.Join(dir, "mvccish.db")
	e, err := Open(path, &Options{UseFormatV2: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := OpenBTree(e)
	for i := uint64(1); i <= n; i++ {
		k := index.CellKeyWithVersion(p, i)
		if err := bt.Put(k, []byte("v")); err != nil {
			t.Fatalf("put seq=%d: %v", i, err)
		}
	}
	// Delete the same stale prefix as MVCC prune would for retain window 27 @ n=273 (drops seq < 246).
	const retain uint64 = 27
	beforeSeq := uint64(n) - retain // 246
	for i := uint64(1); i < beforeSeq; i++ {
		k := index.CellKeyWithVersion(p, i)
		if err := bt.Delete(k); err != nil {
			t.Fatalf("delete seq=%d: %v", i, err)
		}
	}
	_, ok, err := bt.Get(index.CellKeyWithVersion(p, n))
	if err != nil || !ok {
		t.Fatalf("latest key missing ok=%v err=%v", ok, err)
	}
}

// MVCC commits write __meta/commit-time before cell/ rows (sorted key order). Matches [DB.Update].
func TestBTree_mvccPlusCommitTimeAlternatingThenPrune(t *testing.T) {
	t.Parallel()
	const n = 273
	p := lattice.PackedCoord{3, 9}
	dir := t.TempDir()
	path := filepath.Join(dir, "mvcc_meta.db")
	e, err := Open(path, &Options{UseFormatV2: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := OpenBTree(e)
	for i := uint64(1); i <= n; i++ {
		mk := index.CommitTimeKey(int64(i)*1e9, i)
		if err := bt.Put(mk, []byte{}); err != nil {
			t.Fatalf("put meta seq=%d: %v", i, err)
		}
		ck := index.CellKeyWithVersion(p, i)
		if err := bt.Put(ck, []byte("cell")); err != nil {
			t.Fatalf("put cell seq=%d: %v", i, err)
		}
	}
	const retain uint64 = 27
	beforeSeq := uint64(n) - retain
	for i := uint64(1); i < beforeSeq; i++ {
		k := index.CellKeyWithVersion(p, i)
		if err := bt.Delete(k); err != nil {
			t.Fatalf("delete seq=%d: %v", i, err)
		}
	}
}

func TestBTree_mvccShapedDeleteFirstStaleOnly(t *testing.T) {
	t.Parallel()
	const n = 273
	p := lattice.PackedCoord{3, 9}
	dir := t.TempDir()
	path := filepath.Join(dir, "mvcc_first.db")
	e, err := Open(path, &Options{UseFormatV2: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	bt := OpenBTree(e)
	for i := uint64(1); i <= n; i++ {
		k := index.CellKeyWithVersion(p, i)
		if err := bt.Put(k, []byte("v")); err != nil {
			t.Fatalf("put seq=%d: %v", i, err)
		}
	}
	if err := bt.Delete(index.CellKeyWithVersion(p, 1)); err != nil {
		t.Fatal(err)
	}
}
