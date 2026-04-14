package engine

import (
	"path/filepath"
	"strconv"
	"testing"
)

func benchBTreeFilled(b *testing.B, n int) (*Engine, *BTree, [][]byte) {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "bench.db")
	e, err := Open(path, nil)
	if err != nil {
		b.Fatal(err)
	}
	bt := OpenBTree(e)
	keys := make([][]byte, n)
	for i := range n {
		k := []byte("k" + strconv.Itoa(i))
		v := []byte("v" + strconv.Itoa(i))
		keys[i] = k
		if err := bt.Put(k, v); err != nil {
			b.Fatalf("put %d: %v", i, err)
		}
	}
	return e, bt, keys
}

func BenchmarkBTreeGet(b *testing.B) {
	e, bt, keys := benchBTreeFilled(b, 500)
	b.Cleanup(func() { _ = e.Close() })

	b.ResetTimer()
	for b.Loop() {
		_, _, _ = bt.Get(keys[251])
	}
}

func BenchmarkBTreePutUpdate(b *testing.B) {
	e, bt, _ := benchBTreeFilled(b, 100)
	b.Cleanup(func() { _ = e.Close() })
	k := []byte("kupd")
	v := []byte("vnew")

	b.ResetTimer()
	for b.Loop() {
		_ = bt.Put(k, v)
	}
}

func BenchmarkBTreeAscendRange(b *testing.B) {
	e, bt, _ := benchBTreeFilled(b, 200)
	b.Cleanup(func() { _ = e.Close() })

	b.ResetTimer()
	for b.Loop() {
		_ = bt.AscendRange([]byte("k10"), []byte("k20"), func(_, _ []byte) bool {
			return true
		})
	}
}
