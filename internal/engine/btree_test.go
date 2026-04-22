package engine

import (
	"bytes"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

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
