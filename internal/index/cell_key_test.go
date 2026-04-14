package index_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestCellKey_roundTrip(t *testing.T) {
	t.Parallel()
	c := lattice.Coord{Q: 3, R: -2}
	p, err := lattice.Pack(c)
	if err != nil {
		t.Fatal(err)
	}
	k := index.CellKey(p)
	got, err := index.ParseCellKey(k)
	if err != nil {
		t.Fatal(err)
	}
	if got != p {
		t.Fatalf("got %+v want %+v", got, p)
	}
}

func TestCellKey_orderMatchesPackedCoordCompare(t *testing.T) {
	t.Parallel()
	coords := []lattice.Coord{
		{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: -1, R: 1}, {Q: 2, R: -3},
	}
	var keys [][]byte
	for _, c := range coords {
		p, err := lattice.Pack(c)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, index.CellKey(p))
	}
	slices.SortFunc(keys, bytes.Compare)
	var prev lattice.PackedCoord
	for i, k := range keys {
		p, err := index.ParseCellKey(k)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && prev.Compare(p) >= 0 {
			t.Fatalf("order mismatch at %d: %v vs %v", i, prev, p)
		}
		prev = p
	}
}
