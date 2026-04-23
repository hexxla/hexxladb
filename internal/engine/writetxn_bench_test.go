package engine

import (
	"bytes"
	"path/filepath"
	"strconv"
	"testing"
)

func BenchmarkCommitWriteTxn_multiPage(b *testing.B) {
	tmpl := bytes.Repeat([]byte{0x5a}, PageSize)
	for _, nPages := range []int{1, 8, 32} {
		b.Run(strconv.Itoa(nPages), func(b *testing.B) {
			b.ReportAllocs()
			dir := b.TempDir()
			path := filepath.Join(dir, "bench.db")
			e, err := Open(path, nil)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = e.Close() })
			for b.Loop() {
				if err := e.BeginWriteTxn(); err != nil {
					b.Fatal(err)
				}
				for pid := 1; pid <= nPages; pid++ {
					if err := e.WritePage(uint64(pid), tmpl); err != nil {
						b.Fatal(err)
					}
				}
				if err := e.CommitWriteTxn(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
