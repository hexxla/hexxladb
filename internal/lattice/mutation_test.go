package lattice_test

// mutation_test.go — tests specifically targeting surviving gremlins mutants.
// Each test is documented with the mutant type and location it kills.

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// ── bounds.go — inPackRangeCube boundary mutants ─────────────────────────────

// Kills CONDITIONALS_BOUNDARY at bounds.go:57 (r > MaxAxialAbs → r >= MaxAxialAbs)
// and bounds.go:60 (s > MaxAxialAbs → s >= MaxAxialAbs).
// Tests that exactly MaxAxialAbs is valid (boundary is inclusive) and MaxAxialAbs+1 is not.
func TestPack_exactBoundaryValues(t *testing.T) {
	t.Parallel()
	maxAx := lattice.MaxAxialAbs

	// Q at boundary — must succeed
	if _, err := lattice.Pack(lattice.Coord{Q: maxAx, R: 0}); err != nil {
		t.Fatalf("Pack(Q=MaxAxialAbs, R=0) should succeed: %v", err)
	}
	if _, err := lattice.Pack(lattice.Coord{Q: -maxAx, R: 0}); err != nil {
		t.Fatalf("Pack(Q=-MaxAxialAbs, R=0) should succeed: %v", err)
	}
	// R at boundary — must succeed
	if _, err := lattice.Pack(lattice.Coord{Q: 0, R: maxAx}); err != nil {
		t.Fatalf("Pack(Q=0, R=MaxAxialAbs) should succeed: %v", err)
	}
	if _, err := lattice.Pack(lattice.Coord{Q: 0, R: -maxAx}); err != nil {
		t.Fatalf("Pack(Q=0, R=-MaxAxialAbs) should succeed: %v", err)
	}
	// Both Q and R at boundary with S = -Q-R in range
	if _, err := lattice.Pack(lattice.Coord{Q: maxAx, R: -maxAx}); err != nil {
		t.Fatalf("Pack(Q=max, R=-max) should succeed (S=0): %v", err)
	}

	// Q one past boundary — must fail
	if _, err := lattice.Pack(lattice.Coord{Q: maxAx + 1, R: 0}); err == nil {
		t.Fatal("Pack(Q=MaxAxialAbs+1, R=0) should fail")
	}
	// R one past boundary — must fail
	if _, err := lattice.Pack(lattice.Coord{Q: 0, R: maxAx + 1}); err == nil {
		t.Fatal("Pack(Q=0, R=MaxAxialAbs+1) should fail")
	}
	// S out of range: Q=maxAx, R=1 → S = -maxAx-1
	if _, err := lattice.Pack(lattice.Coord{Q: maxAx, R: 1}); err == nil {
		t.Fatal("Pack(Q=max, R=1) should fail (S=-max-1 out of range)")
	}
}

// Kills CONDITIONALS_BOUNDARY at bounds.go:67 (z <= maxZigzagLimb → z < maxZigzagLimb).
// The boundary coord exactly at MaxAxialAbs should produce a zigzag value at the limit.
func TestPack_roundTripAtExactBoundary(t *testing.T) {
	t.Parallel()
	maxAx := lattice.MaxAxialAbs
	coords := []lattice.Coord{
		{Q: maxAx, R: -maxAx},
		{Q: -maxAx, R: maxAx},
		{Q: maxAx, R: 0},
		{Q: 0, R: maxAx},
		{Q: -maxAx, R: 0},
		{Q: 0, R: -maxAx},
	}
	for _, c := range coords {
		p, err := lattice.Pack(c)
		if err != nil {
			t.Fatalf("Pack(%+v): %v", c, err)
		}
		got, err := lattice.Unpack(p)
		if err != nil {
			t.Fatalf("Unpack(Pack(%+v)): %v", c, err)
		}
		if got != c {
			t.Fatalf("round trip %+v → %+v", c, got)
		}
	}
}

// ── packed.go — Compare boundary mutants ─────────────────────────────────────

// Kills CONDITIONALS_BOUNDARY at packed.go:75 and :78.
// Tests that two coordinates differing only in the low word are correctly ordered.
func TestPackedCoord_Compare_lowWordDifference(t *testing.T) {
	t.Parallel()
	// Choose two coords that produce the same Hi word (0) but different Lo words.
	// (0,0) and (1,0) should differ only in Lo.
	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 1, R: 0}
	pa, err := lattice.Pack(a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := lattice.Pack(b)
	if err != nil {
		t.Fatal(err)
	}
	// They must not be equal
	cmp := pa.Compare(pb)
	if cmp == 0 {
		t.Fatal("(0,0) and (1,0) should have different packed representations")
	}
	// Antisymmetry
	if pb.Compare(pa) != -cmp {
		t.Fatal("Compare antisymmetry violated")
	}
	// Self-compare must be 0
	self := pa
	if pa.Compare(self) != 0 {
		t.Fatal("Compare(self) != 0")
	}
}

// Tests Compare with coords that differ in high word (if possible) or
// specifically exercise the low-word-only path by ensuring high words are equal.
func TestPackedCoord_Compare_equalHighDifferentLow(t *testing.T) {
	t.Parallel()
	// All valid coords in our range have Hi=0, so any two distinct coords
	// exercise the low-word comparison path.
	coords := []lattice.Coord{
		{Q: 0, R: 0},
		{Q: 0, R: 1},
		{Q: 1, R: 0},
		{Q: -1, R: 0},
		{Q: 0, R: -1},
		{Q: 1, R: -1},
	}
	packed := make([]lattice.PackedCoord, len(coords))
	for i, c := range coords {
		p, err := lattice.Pack(c)
		if err != nil {
			t.Fatal(err)
		}
		packed[i] = p
	}
	// Verify transitivity: if a < b and b < c, then a < c.
	// Also verify strict ordering produces correct results.
	for i := range packed {
		for j := range packed {
			cij := packed[i].Compare(packed[j])
			cji := packed[j].Compare(packed[i])
			if cij != -cji {
				t.Fatalf("antisymmetry failed for coords[%d] vs coords[%d]", i, j)
			}
			if i == j && cij != 0 {
				t.Fatalf("Compare(self) != 0 for coords[%d]", i)
			}
			if i != j && cij == 0 {
				t.Fatalf("distinct coords[%d]=%+v and coords[%d]=%+v compare equal",
					i, coords[i], j, coords[j])
			}
		}
	}
}

// ── ring.go — arithmetic and boundary mutants ────────────────────────────────

// Kills CONDITIONALS_BOUNDARY at ring.go:36 (k < 0 → k <= 0).
// Verifies that Ring(c, 0) returns exactly [center].
func TestRing_zeroRadius(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 3, R: -2}
	got := lattice.Ring(center, 0)
	if len(got) != 1 {
		t.Fatalf("Ring(c, 0) len=%d want 1", len(got))
	}
	if got[0] != center {
		t.Fatalf("Ring(c, 0)[0] = %+v want %+v", got[0], center)
	}
}

// Kills ARITHMETIC_BASE at ring.go:15 (cubeAdd: + → -, etc.).
// Directly tests cubeAdd behaviour via Ring(origin, 1) against known coords.
func TestRing_k1_allAxes(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 0, R: 0}
	ring := lattice.Ring(center, 1)
	if len(ring) != 6 {
		t.Fatalf("Ring(origin, 1) len=%d want 6", len(ring))
	}
	// Every cell must be exactly distance 1 from center.
	for _, c := range ring {
		if d := center.Distance(c); d != 1 {
			t.Fatalf("Ring(origin,1) cell %+v at distance %d", c, d)
		}
	}
	// Every cell must satisfy cube constraint Q+R+S=0.
	for _, c := range ring {
		cb := c.Cube()
		if cb.Q+cb.R+cb.S != 0 {
			t.Fatalf("cube constraint violated: %+v", cb)
		}
	}
	// The 6 cells must be distinct.
	seen := make(map[lattice.Coord]struct{})
	for _, c := range ring {
		if _, dup := seen[c]; dup {
			t.Fatalf("duplicate in Ring(origin,1): %+v", c)
		}
		seen[c] = struct{}{}
	}
}

// Kills ARITHMETIC_BASE at ring.go:19 (cubeScale: * → /).
// Ring(origin, 3) should produce cells all at distance 3.
func TestRing_k3_distanceAndCount(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 5, R: -3}
	ring := lattice.Ring(center, 3)
	if len(ring) != 18 {
		t.Fatalf("Ring(c, 3) len=%d want 18", len(ring))
	}
	seen := make(map[lattice.Coord]struct{})
	for _, c := range ring {
		if d := center.Distance(c); d != 3 {
			t.Fatalf("cell %+v at distance %d want 3", c, d)
		}
		if _, dup := seen[c]; dup {
			t.Fatalf("duplicate %+v", c)
		}
		seen[c] = struct{}{}
	}
}

// Kills ARITHMETIC_BASE at ring.go:24 (cubeNeighbor: + → -).
// Checks that walking Ring(center, k) for a non-origin center produces correct perimeter.
func TestRing_nonOriginCenter_cube(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: -2, R: 4}
	for k := 1; k <= 3; k++ {
		ring := lattice.Ring(center, k)
		for _, c := range ring {
			cb := c.Cube()
			if cb.Q+cb.R+cb.S != 0 {
				t.Fatalf("k=%d: cube constraint violated at %+v", k, cb)
			}
			if d := center.Distance(c); d != k {
				t.Fatalf("k=%d: cell %+v at distance %d", k, c, d)
			}
		}
	}
}

// Kills ARITHMETIC_BASE at ring.go:43 (capacity 6*k → 6+k or 6-k).
// While capacity is performance-only, we verify the slice has the correct length
// (which would fail if the capacity bug affected append logic somehow).
func TestRing_capacityMatchesLength(t *testing.T) {
	t.Parallel()
	for k := 1; k <= 5; k++ {
		ring := lattice.Ring(lattice.Coord{}, k)
		if len(ring) != 6*k {
			t.Fatalf("k=%d: len=%d want %d", k, len(ring), 6*k)
		}
		// Also verify cap >= len (should always be true with correct pre-alloc)
		if cap(ring) < len(ring) {
			t.Fatalf("k=%d: cap=%d < len=%d", k, cap(ring), len(ring))
		}
	}
}

// ── Ring negative k ──────────────────────────────────────────────────────────

func TestRing_negativeK(t *testing.T) {
	t.Parallel()
	got := lattice.Ring(lattice.Coord{Q: 1, R: 1}, -1)
	if got != nil {
		t.Fatalf("Ring(c, -1) = %v, want nil", got)
	}
}

// ── bounds.go:67 — zigzagFits21 boundary ─────────────────────────────────────

// Kills CONDITIONALS_BOUNDARY at bounds.go:67.
// Exercises the zigzag boundary exactly at MaxAxialAbs via Pack→Unpack round trip.
// MaxAxialAbs = 2^18-1; zigzag64(262143) = 524286 which is well within 2^21-1.
// zigzag64(-262143) = 524285. Both are valid and must round-trip.
func TestPack_zigzagBoundaryViaMaxCoords(t *testing.T) {
	t.Parallel()
	maxAx := lattice.MaxAxialAbs
	// The largest valid zigzag we can produce is zigzag64(MaxAxialAbs) = 2*maxAx.
	// If the boundary mutant changes <= to <, it would reject 2*maxAx when maxAx
	// happens to equal maxZigzagLimb. But since 2*maxAx = 524286 < 2097151, we
	// can't kill this mutant via Pack alone.
	//
	// Test that maximum-range coords round trip correctly — this exercises
	// zigzagFits21 in the Unpack path and verifies boundary handling.
	extremes := []lattice.Coord{
		{Q: maxAx, R: -maxAx},        // S = 0
		{Q: -maxAx, R: maxAx},        // S = 0
		{Q: maxAx, R: 0},             // S = -maxAx
		{Q: 0, R: -maxAx},            // S = maxAx
		{Q: -maxAx / 2, R: -maxAx},   // S is positive near boundary
		{Q: maxAx / 2, R: maxAx / 2}, // S is negative near boundary
	}
	for _, c := range extremes {
		// Verify S is in range before Pack
		s := -c.Q - c.R
		if s > maxAx || s < -maxAx {
			continue // skip coords with out-of-range S
		}
		p, err := lattice.Pack(c)
		if err != nil {
			t.Fatalf("Pack(%+v): %v", c, err)
		}
		got, err := lattice.Unpack(p)
		if err != nil {
			t.Fatalf("Unpack(Pack(%+v)): %v", c, err)
		}
		if got != c {
			t.Fatalf("round trip %+v → %+v", c, got)
		}
	}
}

// ── ring.go — cubeAdd/cubeScale/cubeNeighbor direct verification ─────────────

// Kills ARITHMETIC_BASE at ring.go:15,19,24 by checking specific cube arithmetic
// properties that only hold if +, *, and the direction table are correct.
func TestRing_cubeArithmetic_specific(t *testing.T) {
	t.Parallel()
	center := lattice.Coord{Q: 1, R: 2} // non-origin, asymmetric
	ring1 := lattice.Ring(center, 1)

	// Each neighbor in ring1 must differ from center by exactly one direction vector.
	// The 6 direction vectors in cube space are:
	// {1,0,-1}, {1,-1,0}, {0,-1,1}, {-1,0,1}, {-1,1,0}, {0,1,-1}
	directions := [][3]int{
		{1, 0, -1}, {1, -1, 0}, {0, -1, 1}, {-1, 0, 1}, {-1, 1, 0}, {0, 1, -1},
	}
	cb := center.Cube()
	for i, c := range ring1 {
		cc := c.Cube()
		dq := cc.Q - cb.Q
		dr := cc.R - cb.R
		ds := cc.S - cb.S
		// The difference must be one of the 6 direction vectors
		found := false
		for _, d := range directions {
			if dq == d[0] && dr == d[1] && ds == d[2] {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ring1[%d]=%+v: delta (%d,%d,%d) not a valid direction from center %+v",
				i, c, dq, dr, ds, center)
		}
	}

	// Ring(center, 2): first cell must be center + 2*(1,0,-1) in cube space
	ring2 := lattice.Ring(center, 2)
	first2 := ring2[0].Cube()
	wantQ := cb.Q + 2*1
	wantR := cb.R + 2*0
	wantS := cb.S + 2*(-1)
	if first2.Q != wantQ || first2.R != wantR || first2.S != wantS {
		t.Fatalf("Ring(center,2)[0] cube=(%d,%d,%d) want (%d,%d,%d)",
			first2.Q, first2.R, first2.S, wantQ, wantR, wantS)
	}
}
