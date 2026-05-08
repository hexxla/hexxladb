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

// --- Lazy iterator tests ---

func TestWalkRingsPackedSeq_matchesEager(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 0, R: 0}
	for maxR := range 6 {
		eager := lattice.WalkRingsPacked(center, maxR)
		var lazy []lattice.PackedCoord
		for cp := range lattice.WalkRingsPackedSeq(center, maxR) {
			lazy = append(lazy, cp.Packed)
		}
		if len(lazy) != len(eager) {
			t.Fatalf("maxR=%d: lazy len=%d eager len=%d", maxR, len(lazy), len(eager))
		}
		for i := range eager {
			if lazy[i] != eager[i] {
				t.Fatalf("maxR=%d idx=%d: lazy %v != eager %v", maxR, i, lazy[i], eager[i])
			}
		}
	}
}

func TestWalkRingsPackedSeq_negativeMaxR(t *testing.T) {
	t.Parallel()
	count := 0
	for range lattice.WalkRingsPackedSeq(lattice.Coord{}, -1) {
		count++
	}
	if count != 0 {
		t.Fatalf("expected 0 yields for negative maxR, got %d", count)
	}
}

func TestWalkRingsPackedSeq_earlyBreak(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 0, R: 0}
	const limit = 5
	count := 0
	for range lattice.WalkRingsPackedSeq(center, 10) {
		count++
		if count >= limit {
			break
		}
	}
	if count != limit {
		t.Fatalf("expected exactly %d yields before break, got %d", limit, count)
	}
}

func TestSpiralRangePackedSeq_matchesEager(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 2, R: -1}
	for minR := range 4 {
		for maxR := minR; maxR < minR+4; maxR++ {
			eager := lattice.SpiralRange(nil, center, minR, maxR)
			var lazy []lattice.Coord
			for cp := range lattice.SpiralRangePackedSeq(center, minR, maxR) {
				lazy = append(lazy, cp.Coord)
			}
			if len(lazy) != len(eager) {
				t.Fatalf("minR=%d maxR=%d: lazy len=%d eager len=%d", minR, maxR, len(lazy), len(eager))
			}
			for i := range eager {
				if lazy[i] != eager[i] {
					t.Fatalf("minR=%d maxR=%d idx=%d: lazy %v != eager %v", minR, maxR, i, lazy[i], eager[i])
				}
			}
		}
	}
}

func TestRingSeq_matchesRing(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 1, R: 2}
	for k := range 5 {
		eager := lattice.Ring(center, k)
		var lazy []lattice.Coord
		for c := range lattice.RingSeq(center, k) {
			lazy = append(lazy, c)
		}
		if len(lazy) != len(eager) {
			t.Fatalf("k=%d: lazy len=%d eager len=%d", k, len(lazy), len(eager))
		}
		for i := range eager {
			if lazy[i] != eager[i] {
				t.Fatalf("k=%d idx=%d: lazy %v != eager %v", k, i, lazy[i], eager[i])
			}
		}
	}
}

func BenchmarkWalkRingsPackedSeq_full(b *testing.B) {
	center := lattice.Coord{Q: 0, R: 0}
	for b.Loop() {
		for range lattice.WalkRingsPackedSeq(center, 32) {
		}
	}
}

func BenchmarkWalkRingsPackedSeq_budget16(b *testing.B) {
	// Simulate early-exit at budget=16: lazy saves all work beyond coord 16.
	center := lattice.Coord{Q: 0, R: 0}
	for b.Loop() {
		n := 0
		for range lattice.WalkRingsPackedSeq(center, 100) {
			n++
			if n >= 16 {
				break
			}
		}
	}
}

func BenchmarkWalkRingsPacked_r100(b *testing.B) {
	center := lattice.Coord{Q: 0, R: 0}
	for b.Loop() {
		_ = lattice.WalkRingsPacked(center, 100)
	}
}

func BenchmarkWalkRingsPackedSeq_r100_full(b *testing.B) {
	center := lattice.Coord{Q: 0, R: 0}
	for b.Loop() {
		for range lattice.WalkRingsPackedSeq(center, 100) {
		}
	}
}
