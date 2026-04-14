package index

import (
	"bytes"
	"errors"
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestFacetKey_roundTrip(t *testing.T) {
	t.Parallel()
	p, err := lattice.Pack(lattice.Coord{Q: 2, R: -1})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []byte{0, 3, 5} {
		k, err := FacetKey(p, id)
		if err != nil {
			t.Fatal(err)
		}
		gotP, gotID, err := ParseFacetKey(k)
		if err != nil {
			t.Fatal(err)
		}
		if gotP != p || gotID != id {
			t.Fatalf("id=%d: got %+v %d want %+v %d", id, gotP, gotID, p, id)
		}
	}
}

func TestFacetKey_rejectsBadFacetID(t *testing.T) {
	t.Parallel()
	p, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	if err != nil {
		t.Fatal(err)
	}
	_, err = FacetKey(p, 6)
	if !errors.Is(err, ErrFacetIDOutOfRange) {
		t.Fatalf("got %v", err)
	}
}

func TestFacetKey_ordering(t *testing.T) {
	t.Parallel()
	a, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	if err != nil {
		t.Fatal(err)
	}
	b, err := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	if err != nil {
		t.Fatal(err)
	}
	ka0, err := FacetKey(a, 0)
	if err != nil {
		t.Fatal(err)
	}
	ka5, err := FacetKey(a, 5)
	if err != nil {
		t.Fatal(err)
	}
	kb0, err := FacetKey(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(ka0, ka5) >= 0 {
		t.Fatal("same cell: facet 0 should sort before facet 5")
	}
	if bytes.Compare(ka5, kb0) >= 0 {
		t.Fatal("facet 5 at a should sort before facet 0 at larger coord b")
	}
}

func TestFacetRangeLowerUpper(t *testing.T) {
	t.Parallel()
	p, err := lattice.Pack(lattice.Coord{Q: -1, R: 2})
	if err != nil {
		t.Fatal(err)
	}
	lo, err := FacetRangeLower(p)
	if err != nil {
		t.Fatal(err)
	}
	hi, err := FacetRangeUpper(p)
	if err != nil {
		t.Fatal(err)
	}
	wantLo, _ := FacetKey(p, 0)
	wantHi, _ := FacetKey(p, MaxFacetID)
	if !bytes.Equal(lo, wantLo) || !bytes.Equal(hi, wantHi) {
		t.Fatalf("lo=%q hi=%q", lo, hi)
	}
}
