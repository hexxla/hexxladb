package lattice_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestCoord_Cube_constraint(t *testing.T) {
	t.Parallel()
	c := lattice.Coord{Q: 2, R: -1}
	cb := c.Cube()
	if cb.Q+cb.R+cb.S != 0 {
		t.Fatalf("cube sum = %d, want 0", cb.Q+cb.R+cb.S)
	}
}

func TestCoord_Distance_symmetry(t *testing.T) {
	t.Parallel()
	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 3, R: -2}
	if a.Distance(b) != b.Distance(a) {
		t.Fatalf("asymmetric distance %d vs %d", a.Distance(b), b.Distance(a))
	}
}

func TestCoord_Distance_self(t *testing.T) {
	t.Parallel()
	c := lattice.Coord{Q: -5, R: 7}
	if c.Distance(c) != 0 {
		t.Fatalf("Distance(self) = %d, want 0", c.Distance(c))
	}
}

func TestCoord_Distance_neighbors(t *testing.T) {
	t.Parallel()
	// Neighbors on axial hex grid are at distance 1.
	center := lattice.Coord{Q: 0, R: 0}
	neighbors := []lattice.Coord{
		{1, 0}, {1, -1}, {0, -1}, {-1, 0}, {-1, 1}, {0, 1},
	}
	for _, n := range neighbors {
		if d := center.Distance(n); d != 1 {
			t.Fatalf("Distance(center, %+v) = %d, want 1", n, d)
		}
	}
}
