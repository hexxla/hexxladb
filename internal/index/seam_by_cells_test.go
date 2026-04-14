package index

import (
	"bytes"
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestSeamByCellsKey_roundTrip(t *testing.T) {
	t.Parallel()
	a, err := lattice.Pack(lattice.Coord{Q: 1, R: -1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := lattice.Pack(lattice.Coord{Q: 2, R: -1})
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := record.CanonicalCellPair(a, b)
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	key, err := SeamByCellsKey(lo, hi, id)
	if err != nil {
		t.Fatal(err)
	}
	glo, ghi, gid, err := ParseSeamByCellsKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if glo != lo || ghi != hi || gid != id {
		t.Fatalf("got %v %v %q", glo, ghi, gid)
	}
}

func TestSeamByCellsKey_lexOrderMatchesPackedCompare(t *testing.T) {
	t.Parallel()
	a, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	b, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	lo, hi := record.CanonicalCellPair(a, b)
	k1, err := SeamByCellsKey(lo, hi, "00000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := SeamByCellsKey(lo, hi, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	if string(k1) >= string(k2) {
		t.Fatal("expected ulid lex order")
	}
}

func TestSeamByCellsRangeLoFixed_bounds(t *testing.T) {
	t.Parallel()
	p, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	from, to := SeamByCellsRangeLoFixed(p)
	if string(from) >= string(to) {
		t.Fatalf("from >= to: %q %q", from, to)
	}
	k, _ := SeamByCellsKey(p, p, "00000000000000000000000000")
	if string(k) >= string(SeamByCellsScanUpperBound()) {
		t.Fatal("scan upper bound should sort after keys")
	}
}

func TestPackedKeyDec(t *testing.T) {
	t.Parallel()
	p, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	b := packedCoordToKeyBytes(p)
	d, ok := packedKeyBytesDec(b)
	if !ok {
		t.Fatal("expected dec")
	}
	if bytes.Equal(b, d) {
		t.Fatal("expected pred")
	}
}
