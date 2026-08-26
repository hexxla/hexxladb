package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkAuthenticatedOverflowChurn(b *testing.B) {
	value := incompressibleTestValue(20 << 10)
	for _, reuse := range []bool{false, true} {
		name := "extend_only_control"
		if reuse {
			name = "generation_safe_reuse"
		}
		b.Run(name, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "churn.db")
			opts := authenticatedRecoveryTestOptions()
			opts.MaxValueBytes = 32 << 10
			opts.disablePageReuse = !reuse
			e, err := Open(path, opts)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = e.Close() }()
			tree := OpenBTree(e)
			commitTreeMutation(b, e, func() error { return tree.Put([]byte("key"), value) })
			before, err := os.Stat(path)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				commitTreeMutation(b, e, func() error { return tree.Delete([]byte("key")) })
				commitTreeMutation(b, e, func() error { return tree.Put([]byte("key"), value) })
			}
			b.StopTimer()
			after, err := os.Stat(path)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(after.Size()-before.Size())/float64(b.N), "primary-B/op")
		})
	}
}
