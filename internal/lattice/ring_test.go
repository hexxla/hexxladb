package lattice_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// Golden order for Ring at origin, k=1: positive-q start, then CCW perimeter.
var ring1Golden = []lattice.Coord{
	{1, 0},
	{1, -1},
	{0, -1},
	{-1, 0},
	{-1, 1},
	{0, 1},
}

// Ring k=2 at origin (first cell +q, then Red Blob walk).
var ring2Golden = []lattice.Coord{
	{2, 0},
	{2, -1},
	{2, -2},
	{1, -2},
	{0, -2},
	{-1, -1},
	{-2, 0},
	{-2, 1},
	{-2, 2},
	{-1, 2},
	{0, 2},
	{1, 1},
}

func TestRing_goldenK1K2(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 0, R: 0}
	got1 := lattice.Ring(center, 1)
	if len(got1) != len(ring1Golden) {
		t.Fatalf("ring1 len %d want %d", len(got1), len(ring1Golden))
	}
	for i := range ring1Golden {
		if got1[i] != ring1Golden[i] {
			t.Fatalf("ring1[%d] = %+v want %+v", i, got1[i], ring1Golden[i])
		}
	}
	got2 := lattice.Ring(center, 2)
	if len(got2) != len(ring2Golden) {
		t.Fatalf("ring2 len %d want %d", len(got2), len(ring2Golden))
	}
	for i := range ring2Golden {
		if got2[i] != ring2Golden[i] {
			t.Fatalf("ring2[%d] = %+v want %+v", i, got2[i], ring2Golden[i])
		}
	}
}

func TestRing_distanceShellCount(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 3, R: -2}
	for k := 1; k <= 4; k++ {
		ring := lattice.Ring(center, k)
		if len(ring) != 6*k {
			t.Fatalf("k=%d: len=%d want %d", k, len(ring), 6*k)
		}
		seen := make(map[lattice.Coord]struct{}, len(ring))
		for _, c := range ring {
			if d := center.Distance(c); d != k {
				t.Fatalf("k=%d: cell %+v distance %d", k, c, d)
			}
			if _, ok := seen[c]; ok {
				t.Fatalf("k=%d: duplicate %+v", k, c)
			}
			seen[c] = struct{}{}
		}
	}
}

func TestWalkRings_ballCount(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 0, R: 0}
	for maxR := 0; maxR <= 5; maxR++ {
		var buf []lattice.Coord
		buf = lattice.WalkRings(buf, center, maxR)
		want := 1 + 3*maxR*(maxR+1)
		if len(buf) != want {
			t.Fatalf("maxR=%d: len=%d want %d (ball formula)", maxR, len(buf), want)
		}
	}
}

func TestCoord_Neighbors(t *testing.T) {
	t.Parallel()
	c := lattice.Coord{Q: 2, R: -1}
	n := c.Neighbors()
	for _, x := range n {
		if c.Distance(x) != 1 {
			t.Fatalf("neighbor %+v distance %d", x, c.Distance(x))
		}
	}
	// Order matches ring1 around origin shifted — spot-check against ring1Golden + c
	c0 := lattice.Coord{Q: 0, R: 0}
	ring := lattice.Ring(c0, 1)
	for i := range n {
		want := lattice.Coord{Q: c.Q + ring[i].Q, R: c.R + ring[i].R}
		if n[i] != want {
			t.Fatalf("Neighbors[%d] = %+v want %+v", i, n[i], want)
		}
	}
}
