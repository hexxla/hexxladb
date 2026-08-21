package lattice_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestSuperHexChildrenMapToParent(t *testing.T) {
	t.Parallel()
	for parentQ := -20; parentQ <= 20; parentQ++ {
		for parentR := -20; parentR <= 20; parentR++ {
			parent := lattice.Coord{Q: parentQ, R: parentR}
			children := lattice.SuperHexChildren(parent)
			if len(children) != 7 {
				t.Fatalf("parent %v has %d children, want 7", parent, len(children))
			}
			seen := make(map[lattice.Coord]struct{}, 7)
			for _, child := range children {
				if got := lattice.SuperHexParent(child); got != parent {
					t.Fatalf("parent(%v) = %v, want %v", child, got, parent)
				}
				if _, duplicate := seen[child]; duplicate {
					t.Fatalf("parent %v repeats child %v", parent, child)
				}
				seen[child] = struct{}{}
			}
		}
	}
}

func TestSuperHexHierarchyPartitionsFineGrid(t *testing.T) {
	t.Parallel()
	for q := -100; q <= 100; q++ {
		for r := -100; r <= 100; r++ {
			fine := lattice.Coord{Q: q, R: r}
			parent := lattice.SuperHexParent(fine)
			found := false
			for _, child := range lattice.SuperHexChildren(parent) {
				if child == fine {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("fine coordinate %v is absent from parent %v children", fine, parent)
			}
		}
	}
}

func TestSuperHexParentAndCenterAtLevel(t *testing.T) {
	t.Parallel()
	parent := lattice.Coord{Q: -3, R: 5}
	for level := 1; level <= 6; level++ {
		center := lattice.SuperHexCenterAtLevel(parent, level)
		if got := lattice.SuperHexParentAtLevel(center, level); got != parent {
			t.Fatalf("level %d parent(center(%v)) = %v, want %v", level, parent, got, parent)
		}
	}
}
