package engine

import (
	"bytes"
	"path/filepath"
	"strconv"
	"testing"
)

func TestBTree_putGet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	e, err := Open(path, nil)
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
	e, err := Open(path, nil)
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
	e, err := Open(path, nil)
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
	e, err := Open(path, nil)
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
