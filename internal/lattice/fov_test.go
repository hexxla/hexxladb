package lattice_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestFieldOfView_noOpaque(t *testing.T) {
	t.Parallel()
	// With no opaque cells, FOV should return all cells in radius.
	visible := lattice.FieldOfView(lattice.Coord{Q: 0, R: 0}, 2, func(lattice.Coord) bool { return false })
	// 3*4+6+1 = 19 cells in radius 2.
	if len(visible) != 19 {
		t.Fatalf("expected 19 visible cells, got %d", len(visible))
	}
}

func TestFieldOfView_allOpaque(t *testing.T) {
	t.Parallel()
	origin := lattice.Coord{Q: 0, R: 0}
	// All neighbors are opaque → only origin + ring 1 visible (ring 1 is opaque but still seen).
	// Ring 2 should be fully blocked.
	visible := lattice.FieldOfView(origin, 3, func(c lattice.Coord) bool {
		return origin.Distance(c) == 1
	})
	visSet := make(map[lattice.Coord]struct{}, len(visible))
	for _, c := range visible {
		visSet[c] = struct{}{}
	}
	// Origin visible.
	if _, ok := visSet[origin]; !ok {
		t.Fatal("origin should be visible")
	}
	// All 6 neighbors visible (opaque cells are seen).
	for _, nb := range origin.Neighbors() {
		if _, ok := visSet[nb]; !ok {
			t.Fatalf("neighbor %v should be visible (opaque but at ring 1)", nb)
		}
	}
	// No cell at distance ≥ 2 should be visible.
	for _, c := range visible {
		if origin.Distance(c) > 1 {
			t.Fatalf("cell %v at distance %d should be blocked", c, origin.Distance(c))
		}
	}
}

func TestFieldOfView_partialBlock(t *testing.T) {
	t.Parallel()
	origin := lattice.Coord{Q: 0, R: 0}
	// Wall along +Q axis at Q=1: blocks cells directly behind it.
	wall := lattice.Coord{Q: 1, R: 0}
	visible := lattice.FieldOfView(origin, 3, func(c lattice.Coord) bool {
		return c == wall
	})
	visSet := make(map[lattice.Coord]struct{}, len(visible))
	for _, c := range visible {
		visSet[c] = struct{}{}
	}
	// The wall cell itself should be visible.
	if _, ok := visSet[wall]; !ok {
		t.Fatal("wall should be visible")
	}
	// Cell directly behind wall (Q=2,R=0) should be blocked.
	behind := lattice.Coord{Q: 2, R: 0}
	if _, ok := visSet[behind]; ok {
		t.Fatalf("cell %v behind wall should be blocked", behind)
	}
	// Cell at Q=3,R=0 should also be blocked.
	farBehind := lattice.Coord{Q: 3, R: 0}
	if _, ok := visSet[farBehind]; ok {
		t.Fatalf("cell %v far behind wall should be blocked", farBehind)
	}
}

func TestFieldOfView_radius0(t *testing.T) {
	t.Parallel()
	visible := lattice.FieldOfView(lattice.Coord{Q: 5, R: 5}, 0, func(lattice.Coord) bool { return false })
	if len(visible) != 1 {
		t.Fatalf("expected 1 cell for radius 0, got %d", len(visible))
	}
	if visible[0] != (lattice.Coord{Q: 5, R: 5}) {
		t.Fatalf("expected origin, got %v", visible[0])
	}
}

func TestFieldOfView_negativeRadius(t *testing.T) {
	t.Parallel()
	visible := lattice.FieldOfView(lattice.Coord{}, -1, func(lattice.Coord) bool { return false })
	if visible != nil {
		t.Fatal("expected nil for negative radius")
	}
}

func TestFieldOfView_nilOpaque(t *testing.T) {
	t.Parallel()
	visible := lattice.FieldOfView(lattice.Coord{}, 3, nil)
	if visible != nil {
		t.Fatal("expected nil for nil opaque")
	}
}

func TestFieldOfView_fewerThanFull(t *testing.T) {
	t.Parallel()
	origin := lattice.Coord{Q: 0, R: 0}
	// Block 3 of the 6 neighbors → some ring-2 cells should be blocked.
	blockers := map[lattice.Coord]bool{
		{Q: 1, R: 0}:  true,
		{Q: 0, R: 1}:  true,
		{Q: -1, R: 1}: true,
	}
	visible := lattice.FieldOfView(origin, 2, func(c lattice.Coord) bool {
		return blockers[c]
	})
	full := 3*4 + 6 + 1 // 19 cells in radius 2
	if len(visible) >= full {
		t.Fatalf("expected fewer than %d cells with 3 blockers, got %d", full, len(visible))
	}
	if len(visible) < 7 {
		// At minimum: origin + 6 neighbors (blockers are visible).
		t.Fatalf("expected at least 7 visible cells, got %d", len(visible))
	}
}

func TestFieldOfView_offCenter(t *testing.T) {
	t.Parallel()
	origin := lattice.Coord{Q: 10, R: -5}
	visible := lattice.FieldOfView(origin, 1, func(lattice.Coord) bool { return false })
	// Radius 1: 7 cells (origin + 6 neighbors).
	if len(visible) != 7 {
		t.Fatalf("expected 7 cells at radius 1, got %d", len(visible))
	}
}

func TestHexLine_samePoint(t *testing.T) {
	t.Parallel()
	line := lattice.HexLine(lattice.Coord{Q: 3, R: 2}, lattice.Coord{Q: 3, R: 2})
	if len(line) != 1 {
		t.Fatalf("expected 1 point, got %d", len(line))
	}
}

func TestHexLine_adjacent(t *testing.T) {
	t.Parallel()
	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 1, R: 0}
	line := lattice.HexLine(a, b)
	if len(line) != 2 {
		t.Fatalf("expected 2 points, got %d", len(line))
	}
	if line[0] != a || line[1] != b {
		t.Fatalf("expected [%v, %v], got %v", a, b, line)
	}
}

func TestHexLine_distance3(t *testing.T) {
	t.Parallel()
	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 3, R: 0}
	line := lattice.HexLine(a, b)
	// Distance 3 → 4 points.
	if len(line) != 4 {
		t.Fatalf("expected 4 points, got %d", len(line))
	}
	// Each consecutive pair should be at distance 1.
	for i := 1; i < len(line); i++ {
		d := line[i-1].Distance(line[i])
		if d != 1 {
			t.Fatalf("step %d-%d distance=%d, want 1", i-1, i, d)
		}
	}
	// Endpoints.
	if line[0] != a {
		t.Fatalf("first point %v, want %v", line[0], a)
	}
	if line[len(line)-1] != b {
		t.Fatalf("last point %v, want %v", line[len(line)-1], b)
	}
}

func TestHexLine_diagonal(t *testing.T) {
	t.Parallel()
	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 2, R: -2}
	line := lattice.HexLine(a, b)
	dist := a.Distance(b)
	if len(line) != dist+1 {
		t.Fatalf("expected %d points, got %d", dist+1, len(line))
	}
	for i := 1; i < len(line); i++ {
		d := line[i-1].Distance(line[i])
		if d != 1 {
			t.Fatalf("step %d-%d distance=%d, want 1", i-1, i, d)
		}
	}
}
