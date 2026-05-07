package lattice_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestWalkRingsPacked_matchesWalkRings(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 0, R: 0}
	for maxR := range 6 {
		coords := lattice.WalkRings(nil, center, maxR)
		packed := lattice.WalkRingsPacked(center, maxR)
		if len(packed) != len(coords) {
			t.Fatalf("maxR=%d: len(packed)=%d != len(coords)=%d", maxR, len(packed), len(coords))
		}
		for i, c := range coords {
			p, err := lattice.Pack(c)
			if err != nil {
				t.Fatalf("maxR=%d coord[%d]: %v", maxR, i, err)
			}
			if p != packed[i] {
				t.Fatalf("maxR=%d coord[%d]: packed mismatch", maxR, i)
			}
		}
	}
}

func TestWalkRingsPacked_zeroMaxR(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 2, R: 3}
	packed := lattice.WalkRingsPacked(center, 0)
	if len(packed) != 1 {
		t.Fatalf("maxR=0: expected 1, got %d", len(packed))
	}
	p, _ := lattice.Pack(center)
	if packed[0] != p {
		t.Fatal("maxR=0: expected center packed")
	}
}

func TestWalkRingsPacked_negativeMaxR(t *testing.T) {
	t.Parallel()
	result := lattice.WalkRingsPacked(lattice.Coord{}, -1)
	if result != nil {
		t.Fatalf("expected nil for negative maxR, got len=%d", len(result))
	}
}

func TestWalkRingsCoordPacked_zeroMaxR(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 0, R: 0}
	pairs := lattice.WalkRingsCoordPacked(center, 0)
	if len(pairs) != 1 {
		t.Fatalf("maxR=0: expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Coord != center {
		t.Fatalf("maxR=0: expected center, got %v", pairs[0].Coord)
	}
}

func TestWalkRingsCoordPacked_negativeMaxR(t *testing.T) {
	t.Parallel()
	result := lattice.WalkRingsCoordPacked(lattice.Coord{}, -1)
	if result != nil {
		t.Fatalf("expected nil for negative maxR, got len=%d", len(result))
	}
}

func TestWalkRingsCoordPacked_pairs(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 1, R: -1}
	pairs := lattice.WalkRingsCoordPacked(center, 3)
	if len(pairs) == 0 {
		t.Fatal("expected non-empty")
	}
	for i, cp := range pairs {
		p, err := lattice.Pack(cp.Coord)
		if err != nil {
			t.Fatalf("pair[%d]: %v", i, err)
		}
		if p != cp.Packed {
			t.Fatalf("pair[%d]: packed mismatch", i)
		}
	}
	// First element should be center.
	if pairs[0].Coord != center {
		t.Fatalf("first pair should be center: got %v", pairs[0].Coord)
	}
}

func BenchmarkWalkRingsPacked(b *testing.B) {
	center := lattice.Coord{Q: 0, R: 0}
	for b.Loop() {
		_ = lattice.WalkRingsPacked(center, 32)
	}
}

func BenchmarkWalkRings(b *testing.B) {
	center := lattice.Coord{Q: 0, R: 0}
	for b.Loop() {
		_ = lattice.WalkRings(nil, center, 32)
	}
}
