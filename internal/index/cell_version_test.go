package index_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestCellVersionKeyEncodingAndBounds(t *testing.T) {
	t.Parallel()
	coord, err := lattice.Pack(lattice.Coord{Q: 2, R: -3})
	if err != nil {
		t.Fatal(err)
	}
	low, high := index.CellVersionScanBounds(coord)
	for _, seq := range []uint64{0, 1, 0xdeadbeefcafebabe, math.MaxUint64} {
		key := index.CellKeyWithVersion(coord, seq)
		want := binary.BigEndian.AppendUint64(index.CellKey(coord), seq)
		if !bytes.Equal(key, want) || bytes.Compare(key, low) < 0 || bytes.Compare(key, high) > 0 {
			t.Fatalf("invalid physical key for %x: %x", seq, key)
		}
		gotCoord, gotSeq, err := index.ParseCellVersionKey(key)
		if err != nil || gotCoord != coord || gotSeq != seq {
			t.Fatalf("parse physical key: got %v, %x, %v", gotCoord, gotSeq, err)
		}
		gotSeq, ok := index.ParseCommitSeqFromCellKey(key)
		if !ok || gotSeq != seq {
			t.Fatalf("parse sequence: got %x, %t, want %x", gotSeq, ok, seq)
		}
	}
}
