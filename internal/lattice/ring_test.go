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

func TestSpiralRange_equalsWalkRings(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 0, R: 0}
	for maxR := 0; maxR <= 5; maxR++ {
		got := lattice.SpiralRange(nil, center, 0, maxR)
		want := lattice.WalkRings(nil, center, maxR)
		if len(got) != len(want) {
			t.Fatalf("maxR=%d: SpiralRange len=%d WalkRings len=%d", maxR, len(got), len(want))
		}
		gotSet := make(map[lattice.Coord]struct{}, len(got))
		for _, c := range got {
			gotSet[c] = struct{}{}
		}
		for _, c := range want {
			if _, ok := gotSet[c]; !ok {
				t.Fatalf("maxR=%d: WalkRings cell %v missing from SpiralRange", maxR, c)
			}
		}
	}
}

func TestSpiralRange_annular(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 1, R: -2}
	got := lattice.SpiralRange(nil, center, 2, 4)
	for _, c := range got {
		d := center.Distance(c)
		if d < 2 || d > 4 {
			t.Fatalf("annular [2,4]: cell %v has distance %d", c, d)
		}
	}
	// Count: sum of 6k for k in [2,4] = 12+18+24 = 54
	if len(got) != 54 {
		t.Fatalf("annular [2,4]: expected 54 cells, got %d", len(got))
	}
}

func TestSpiralRange_edgeCases(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{}
	// maxR < 0 → nil
	if lattice.SpiralRange(nil, center, 0, -1) != nil {
		t.Fatal("maxR=-1: expected nil")
	}
	// minR > maxR → unchanged
	if lattice.SpiralRange(nil, center, 5, 3) != nil {
		t.Fatal("minR>maxR: expected nil")
	}
	// minR=maxR=0 → just center
	got := lattice.SpiralRange(nil, center, 0, 0)
	if len(got) != 1 || got[0] != center {
		t.Fatalf("minR=maxR=0: expected [center], got %v", got)
	}
	// minR=maxR=2 → one ring
	got2 := lattice.SpiralRange(nil, center, 2, 2)
	if len(got2) != 12 {
		t.Fatalf("minR=maxR=2: expected 12 cells, got %d", len(got2))
	}
}

func TestSpiralRange_noCenter(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{}
	// minR=1 → center must NOT be included
	got := lattice.SpiralRange(nil, center, 1, 3)
	for _, c := range got {
		if c == center {
			t.Fatal("minR=1: center should not be included")
		}
	}
	// Ring sizes: 6+12+18 = 36 total cells.
	if len(got) != 36 {
		t.Fatalf("minR=1 maxR=3: expected 36 cells, got %d", len(got))
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
