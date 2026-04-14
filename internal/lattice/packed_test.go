package lattice_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestPackUnpack_roundTrip(t *testing.T) {
	t.Parallel()
	for _, c := range []lattice.Coord{
		{0, 0},
		{1, 0},
		{-1, 2},
		{lattice.MaxAxialAbs, 0},
		{-lattice.MaxAxialAbs, lattice.MaxAxialAbs},
	} {
		p, err := lattice.Pack(c)
		if err != nil {
			t.Fatalf("Pack(%+v): %v", c, err)
		}
		got, err := lattice.Unpack(p)
		if err != nil {
			t.Fatalf("Unpack(Pack(%+v)): %v", c, err)
		}
		if got != c {
			t.Fatalf("round trip got %+v want %+v", got, c)
		}
	}
}

func TestPack_outOfRange(t *testing.T) {
	t.Parallel()
	_, err := lattice.Pack(lattice.Coord{Q: lattice.MaxAxialAbs + 1, R: 0})
	if err == nil {
		t.Fatal("expected error for Q out of range")
	}
}

func TestPackedCoord_Compare_totalOrder(t *testing.T) {
	t.Parallel()
	a := lattice.Coord{Q: 1, R: 0}
	b := lattice.Coord{Q: 0, R: 1}
	pa, err := lattice.Pack(a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := lattice.Pack(b)
	if err != nil {
		t.Fatal(err)
	}
	if pa.Compare(pb) == 0 {
		t.Fatal("expected distinct keys")
	}
	if pb.Compare(pa) != -pa.Compare(pb) {
		t.Fatal("antisymmetry failed")
	}
}

func TestZigzag_inverse(t *testing.T) {
	t.Parallel()
	// property: unzigzag(zigzag(n)) == n for in-range int64 (tested via Pack bounds)
	c := lattice.Coord{Q: 100, R: -50}
	p, err := lattice.Pack(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := lattice.Unpack(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != c {
		t.Fatalf("got %+v want %+v", got, c)
	}
}
