package engine

import (
	"bytes"
	"crypto/sha256"
	"runtime"
	"testing"
)

func benchmarkCompressionInputs() []struct {
	name  string
	input []byte
} {
	incompressible := make([]byte, 1024)
	digest := sha256.Sum256([]byte("hexxladb deterministic compression benchmark"))
	for offset := 0; offset < len(incompressible); offset += len(digest) {
		copy(incompressible[offset:], digest[:])
		digest = sha256.Sum256(digest[:])
	}
	return []struct {
		name  string
		input []byte
	}{
		{name: "compressible_1KiB", input: bytes.Repeat([]byte("hexxladb"), 128)},
		{name: "incompressible_1KiB", input: incompressible},
	}
}

func BenchmarkCompressValue(b *testing.B) {
	for _, tc := range benchmarkCompressionInputs() {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var compressed []byte
			for b.Loop() {
				compressed = compressValue(tc.input)
			}
			runtime.KeepAlive(compressed)
		})
	}
}

func BenchmarkDecompressValue(b *testing.B) {
	input := benchmarkCompressionInputs()[0].input
	compressed := compressValue(input)
	if !isCompressedValue(compressed) {
		b.Fatal("benchmark input did not compress")
	}
	b.ReportAllocs()
	var decompressed []byte
	for b.Loop() {
		var err error
		decompressed, err = decompressValue(compressed)
		if err != nil {
			b.Fatal(err)
		}
	}
	runtime.KeepAlive(decompressed)
}
