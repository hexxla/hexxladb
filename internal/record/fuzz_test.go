package record

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func FuzzDecodeCell(f *testing.F) {
	key, err := lattice.Pack(lattice.Coord{Q: 1, R: -1})
	if err != nil {
		f.Fatal(err)
	}
	valid, err := EncodeCell(CellRecord{
		Key:        key,
		RawContent: "x",
		Provenance: ProvenanceWire{SourceID: "s", Confidence: 1, CreatedAt: 1, UpdatedAt: 2},
		Validity:   ValidityWire{},
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)

	f.Fuzz(func(t *testing.T, data []byte) {
		t.Helper()
		_, _ = DecodeCell(data)
	})
}
