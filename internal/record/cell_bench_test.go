package record

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func BenchmarkEncodeDecodeCell(b *testing.B) {
	key, err := lattice.Pack(lattice.Coord{Q: 2, R: -1})
	if err != nil {
		b.Fatal(err)
	}
	rec := CellRecord{
		Key:        key,
		RawContent: "hello",
		Provenance: ProvenanceWire{SourceID: "src", Confidence: 1, CreatedAt: 1, UpdatedAt: 2},
		Validity:   ValidityWire{},
		Tags:       []string{"a", "b"},
	}
	data, err := EncodeCell(rec)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = DecodeCell(data)
	}
}
