package lattice_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestCoarsenCoord_basic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fine   lattice.Coord
		factor int
		want   lattice.Coord
	}{
		{lattice.Coord{Q: 0, R: 0}, 2, lattice.Coord{Q: 0, R: 0}},
		{lattice.Coord{Q: 1, R: 1}, 2, lattice.Coord{Q: 0, R: 0}},
		{lattice.Coord{Q: 2, R: 2}, 2, lattice.Coord{Q: 1, R: 1}},
		{lattice.Coord{Q: 3, R: 3}, 2, lattice.Coord{Q: 1, R: 1}},
		{lattice.Coord{Q: 4, R: 4}, 2, lattice.Coord{Q: 2, R: 2}},
		{lattice.Coord{Q: 5, R: 0}, 3, lattice.Coord{Q: 1, R: 0}},
		{lattice.Coord{Q: 6, R: 0}, 3, lattice.Coord{Q: 2, R: 0}},
	}
	for _, tc := range tests {
		got, err := lattice.CoarsenCoord(tc.fine, tc.factor)
		if err != nil {
			t.Fatalf("CoarsenCoord(%v, %d): %v", tc.fine, tc.factor, err)
		}
		if got != tc.want {
			t.Fatalf("CoarsenCoord(%v, %d) = %v, want %v", tc.fine, tc.factor, got, tc.want)
		}
	}
}

func TestCoarsenCoord_negativeCoords(t *testing.T) {
	t.Parallel()
	// Floor division: -1/2 = -1 (not 0 like truncation).
	got, err := lattice.CoarsenCoord(lattice.Coord{Q: -1, R: -1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := lattice.Coord{Q: -1, R: -1}
	if got != want {
		t.Fatalf("CoarsenCoord(-1,-1, 2) = %v, want %v", got, want)
	}

	got, err = lattice.CoarsenCoord(lattice.Coord{Q: -2, R: -2}, 2)
	if err != nil {
		t.Fatal(err)
	}
	want = lattice.Coord{Q: -1, R: -1}
	if got != want {
		t.Fatalf("CoarsenCoord(-2,-2, 2) = %v, want %v", got, want)
	}

	got, err = lattice.CoarsenCoord(lattice.Coord{Q: -3, R: 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	want = lattice.Coord{Q: -2, R: 0}
	if got != want {
		t.Fatalf("CoarsenCoord(-3,0, 2) = %v, want %v", got, want)
	}
}

func TestCoarsenCoord_invalidFactor(t *testing.T) {
	t.Parallel()
	_, err := lattice.CoarsenCoord(lattice.Coord{}, 1)
	if err == nil {
		t.Fatal("expected error for factor=1")
	}
	_, err = lattice.CoarsenCoord(lattice.Coord{}, 0)
	if err == nil {
		t.Fatal("expected error for factor=0")
	}
}

func TestRefineCoord_basic(t *testing.T) {
	t.Parallel()
	children, err := lattice.RefineCoord(lattice.Coord{Q: 1, R: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 4 {
		t.Fatalf("expected 4 children, got %d", len(children))
	}
	// Children should be (2,2), (2,3), (3,2), (3,3).
	want := map[lattice.Coord]bool{
		{Q: 2, R: 2}: true,
		{Q: 2, R: 3}: true,
		{Q: 3, R: 2}: true,
		{Q: 3, R: 3}: true,
	}
	for _, c := range children {
		if !want[c] {
			t.Fatalf("unexpected child %v", c)
		}
	}
}

func TestRefineCoord_factor3(t *testing.T) {
	t.Parallel()
	children, err := lattice.RefineCoord(lattice.Coord{Q: 0, R: 0}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 9 {
		t.Fatalf("expected 9 children, got %d", len(children))
	}
}

func TestRefineCoord_invalidFactor(t *testing.T) {
	t.Parallel()
	_, err := lattice.RefineCoord(lattice.Coord{}, 1)
	if err == nil {
		t.Fatal("expected error for factor=1")
	}
}

func TestCoarsenRefine_roundTrip(t *testing.T) {
	t.Parallel()
	// Every child of a coarsened coord should map back to the same parent.
	parent := lattice.Coord{Q: 3, R: -2}
	children, err := lattice.RefineCoord(parent, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		got, err := lattice.CoarsenCoord(child, 2)
		if err != nil {
			t.Fatal(err)
		}
		if got != parent {
			t.Fatalf("child %v coarsened to %v, want %v", child, got, parent)
		}
	}
}

func TestCoarsenRefine_roundTripNegative(t *testing.T) {
	t.Parallel()
	parent := lattice.Coord{Q: -2, R: -3}
	children, err := lattice.RefineCoord(parent, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		got, err := lattice.CoarsenCoord(child, 3)
		if err != nil {
			t.Fatal(err)
		}
		if got != parent {
			t.Fatalf("child %v coarsened to %v, want %v", child, got, parent)
		}
	}
}

func TestCoarsenMulti_basic(t *testing.T) {
	t.Parallel()
	// Two levels of factor-2: (8,8) → (4,4) → (2,2).
	got, err := lattice.CoarsenMulti(lattice.Coord{Q: 8, R: 8}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := lattice.Coord{Q: 2, R: 2}
	if got != want {
		t.Fatalf("CoarsenMulti(8,8, f=2, n=2) = %v, want %v", got, want)
	}
}

func TestCoarsenMulti_invalidArgs(t *testing.T) {
	t.Parallel()
	_, err := lattice.CoarsenMulti(lattice.Coord{}, 1, 1)
	if err == nil {
		t.Fatal("expected error for factor=1")
	}
	_, err = lattice.CoarsenMulti(lattice.Coord{}, 2, 0)
	if err == nil {
		t.Fatal("expected error for levels=0")
	}
}
