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

func TestFieldOfViewShadowcast_symmetry(t *testing.T) {
	t.Parallel()
	origin := lattice.Coord{Q: 0, R: 0}
	// Scattered obstacles at various positions.
	obstacles := map[lattice.Coord]bool{
		{Q: 2, R: -1}: true,
		{Q: -1, R: 2}: true,
		{Q: 0, R: 3}:  true,
		{Q: 3, R: -3}: true,
	}
	opaque := func(c lattice.Coord) bool { return obstacles[c] }

	visible := lattice.FieldOfViewShadowcast(origin, 5, opaque)
	visSet := make(map[lattice.Coord]struct{}, len(visible))
	for _, c := range visible {
		visSet[c] = struct{}{}
	}

	// For every non-opaque visible cell A, A must also see origin.
	for _, a := range visible {
		if opaque(a) {
			continue
		}
		aVisible := lattice.FieldOfViewShadowcast(a, 5, opaque)
		aSet := make(map[lattice.Coord]struct{}, len(aVisible))
		for _, c := range aVisible {
			aSet[c] = struct{}{}
		}
		if _, ok := aSet[origin]; !ok {
			t.Fatalf("symmetry violated: origin sees %v but %v does not see origin", a, a)
		}
	}
}

func TestFieldOfViewShadowcast_openRegressionVsRaycast(t *testing.T) {
	t.Parallel()
	origin := lattice.Coord{Q: 0, R: 0}
	noOpaque := func(lattice.Coord) bool { return false }

	for r := 0; r <= 5; r++ {
		shadow := lattice.FieldOfViewShadowcast(origin, r, noOpaque)
		ray := lattice.FieldOfViewRaycast(origin, r, noOpaque)
		if len(shadow) != len(ray) {
			t.Fatalf("r=%d: shadowcast=%d raycasting=%d", r, len(shadow), len(ray))
		}
		raySet := make(map[lattice.Coord]struct{}, len(ray))
		for _, c := range ray {
			raySet[c] = struct{}{}
		}
		for _, c := range shadow {
			if _, ok := raySet[c]; !ok {
				t.Fatalf("r=%d: shadowcast cell %v missing from raycasting", r, c)
			}
		}
	}
}

func TestFieldOfViewShadowcast_pillarShadow(t *testing.T) {
	t.Parallel()
	origin := lattice.Coord{Q: 0, R: 0}
	pillar := lattice.Coord{Q: 2, R: 0}
	opaque := func(c lattice.Coord) bool { return c == pillar }

	visible := lattice.FieldOfViewShadowcast(origin, 5, opaque)
	visSet := make(map[lattice.Coord]struct{}, len(visible))
	for _, c := range visible {
		visSet[c] = struct{}{}
	}

	// Pillar itself must be visible.
	if _, ok := visSet[pillar]; !ok {
		t.Fatal("pillar should be visible")
	}
	// Cell directly behind pillar must be blocked.
	behind := lattice.Coord{Q: 3, R: 0}
	if _, ok := visSet[behind]; ok {
		t.Fatalf("cell %v directly behind pillar should be blocked", behind)
	}
	// Cells well off to the side should not be blocked.
	side := lattice.Coord{Q: 3, R: -2}
	if _, ok := visSet[side]; !ok {
		t.Fatalf("cell %v off to side should be visible", side)
	}
}

func TestFieldOfViewShadowcast_countOpen(t *testing.T) {
	t.Parallel()
	origin := lattice.Coord{Q: 0, R: 0}
	noOpaque := func(lattice.Coord) bool { return false }
	for r := 0; r <= 8; r++ {
		got := lattice.FieldOfViewShadowcast(origin, r, noOpaque)
		want := 1 + 3*r*(r+1)
		if len(got) != want {
			t.Fatalf("r=%d: got %d cells, want %d (ball formula)", r, len(got), want)
		}
	}
}

func BenchmarkFieldOfViewShadowcast(b *testing.B) {
	origin := lattice.Coord{Q: 0, R: 0}
	noOpaque := func(lattice.Coord) bool { return false }
	b.Run("r10", func(b *testing.B) {
		for b.Loop() {
			lattice.FieldOfViewShadowcast(origin, 10, noOpaque)
		}
	})
	b.Run("r20", func(b *testing.B) {
		for b.Loop() {
			lattice.FieldOfViewShadowcast(origin, 20, noOpaque)
		}
	})
	b.Run("r50", func(b *testing.B) {
		for b.Loop() {
			lattice.FieldOfViewShadowcast(origin, 50, noOpaque)
		}
	})
}

func BenchmarkFieldOfViewRaycast(b *testing.B) {
	origin := lattice.Coord{Q: 0, R: 0}
	noOpaque := func(lattice.Coord) bool { return false }
	b.Run("r10", func(b *testing.B) {
		for b.Loop() {
			lattice.FieldOfViewRaycast(origin, 10, noOpaque)
		}
	})
	b.Run("r20", func(b *testing.B) {
		for b.Loop() {
			lattice.FieldOfViewRaycast(origin, 20, noOpaque)
		}
	})
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
