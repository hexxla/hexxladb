package lattice

import (
	"cmp"
	"math"
	"slices"
	"sort"
)

// FieldOfView computes visible coordinates from origin up to maxRadius.
// Opaque hexes block centre-to-centre sight lines, including boundary contact.
// The first blocking hex is visible. Results are deterministic and nearest-first.
// Returns nil if maxRadius < 0 or opaque is nil. Returns {origin} if maxRadius == 0.
func FieldOfView(origin Coord, maxRadius int, opaque func(Coord) bool) []Coord {
	return FieldOfViewShadowcast(origin, maxRadius, opaque)
}

// FieldOfViewShadowcast casts the angular shadows of regular hexagons using
// exact integer directions. A floor tile is visible when the segment joining
// its centre to origin does not intersect another opaque hex, including edges.
// This geometric rule is reciprocal and invariant under hex rotations.
//
// Tiles are processed by squared Euclidean distance. A line crossing from one
// hex's Voronoi region into the next moves to a centre farther from origin, so
// every possible blocker is processed before its target. A range-add, point-
// query tree tracks which candidate directions have entered a shadow.
//
// Work is O(R² log R) and memory O(R²) for radius R. The opacity callback is
// evaluated once for each non-origin coordinate in the disk and must be stable
// throughout the call. Opaque origins do not block their own outgoing sight.
func FieldOfViewShadowcast(origin Coord, maxRadius int, opaque func(Coord) bool) []Coord {
	if maxRadius < 0 || opaque == nil {
		return nil
	}
	if maxRadius == 0 {
		return []Coord{origin}
	}
	rays := WalkRings(nil, Coord{}, maxRadius)[1:]
	cells := make([]fovCell, len(rays))
	hasBlocker := false
	for i, offset := range rays {
		blocked := opaque(Coord{Q: origin.Q + offset.Q, R: origin.R + offset.R})
		cells[i] = fovCell{offset: offset, blocked: blocked}
		hasBlocker = hasBlocker || blocked
	}
	if !hasBlocker {
		visible := make([]Coord, 1, len(rays)+1)
		visible[0] = origin
		for _, offset := range rays {
			visible = append(visible, Coord{Q: origin.Q + offset.Q, R: origin.R + offset.R})
		}
		sortFOVVisible(visible, origin)
		return visible
	}
	slices.SortFunc(cells, func(a, b fovCell) int { return compareFOVDirections(a.offset, b.offset) })
	for i := range cells {
		rays[i] = cells[i].offset
		cells[i].ray = i
	}
	slices.SortFunc(cells, func(a, b fovCell) int {
		return cmp.Compare(fovDistanceSquared(a.offset), fovDistanceSquared(b.offset))
	})
	shadows := make(fovShadows, len(rays)+1)
	visible := make([]Coord, 1, len(rays)+1)
	visible[0] = origin
	for _, cell := range cells {
		coord := Coord{Q: origin.Q + cell.offset.Q, R: origin.R + cell.offset.R}
		if !shadows.contains(cell.ray) {
			visible = append(visible, coord)
		}
		if cell.blocked {
			shadows.cast(cell.offset, rays)
		}
	}
	sortFOVVisible(visible, origin)
	return visible
}

func sortFOVVisible(visible []Coord, origin Coord) {
	slices.SortFunc(visible, func(a, b Coord) int {
		return cmp.Or(cmp.Compare(origin.Distance(a), origin.Distance(b)), cmp.Compare(a.Q, b.Q), cmp.Compare(a.R, b.R))
	})
}

type fovCell struct {
	offset  Coord
	ray     int
	blocked bool
}

func fovDistanceSquared(c Coord) int {
	return c.Q*c.Q + c.Q*c.R + c.R*c.R
}

// compareFOVDirections orders nonzero axial vectors counterclockwise from +Q.
// The axial-to-Cartesian transformation preserves the sign of cross products.
func compareFOVDirections(a, b Coord) int {
	aLower := a.R < 0 || a.R == 0 && a.Q < 0
	bLower := b.R < 0 || b.R == 0 && b.Q < 0
	if aLower != bLower {
		if aLower {
			return 1
		}
		return -1
	}
	return cmp.Compare(0, fovCross(a, b))
}

func fovCross(a, b Coord) int {
	return a.Q*b.R - a.R*b.Q
}

// Hex vertices are represented in axial coordinates scaled by three. These
// are intersections of the perpendicular bisectors to adjacent hex centres.
var fovHexVertices = [...]Coord{{Q: 1, R: 1}, {Q: 2, R: -1}, {Q: 1, R: -2}, {Q: -1, R: -1}, {Q: -2, R: 1}, {Q: -1, R: 2}}

// fovShadows is a Fenwick tree over a difference array of shadow counts.
// Adding [start, end) changes two entries; a prefix sum tests one direction.
type fovShadows []int

func (shadows fovShadows) add(position, delta int) {
	for i := position + 1; i < len(shadows); i += i & -i {
		shadows[i] += delta
	}
}

func (shadows fovShadows) contains(position int) bool {
	count := 0
	for i := position + 1; i > 0; i -= i & -i {
		count += shadows[i]
	}
	return count > 0
}

func (shadows fovShadows) cast(offset Coord, rays []Coord) {
	left, right := offset, offset
	for _, vertex := range fovHexVertices {
		direction := Coord{Q: 3*offset.Q + vertex.Q, R: 3*offset.R + vertex.R}
		if fovCross(left, direction) < 0 {
			left = direction
		}
		if fovCross(right, direction) > 0 {
			right = direction
		}
	}
	start, _ := slices.BinarySearchFunc(rays, left, compareFOVDirections)
	end := sort.Search(len(rays), func(i int) bool { return compareFOVDirections(rays[i], right) > 0 })
	shadows.add(start, 1)
	shadows.add(end, -1)
	if compareFOVDirections(left, right) > 0 {
		shadows.add(0, 1)
	}
}

// FieldOfViewRaycast is the original raycasting implementation, retained for
// regression comparison in tests. Not used in production paths.
func FieldOfViewRaycast(origin Coord, maxRadius int, opaque func(Coord) bool) []Coord {
	candidates := WalkRings(nil, origin, maxRadius)
	visible := make(map[Coord]struct{}, len(candidates))
	visible[origin] = struct{}{}
	for _, target := range candidates {
		if target == origin {
			continue
		}
		if checkLOS(origin, target, opaque) {
			visible[target] = struct{}{}
		}
	}
	out := make([]Coord, 0, len(visible))
	for c := range visible {
		out = append(out, c)
	}
	return out
}

// checkLOS returns true if target is visible from origin using raycasting.
func checkLOS(origin, target Coord, opaque func(Coord) bool) bool {
	line := HexLine(origin, target)
	return !slices.ContainsFunc(line[1:len(line)-1], opaque)
}

// HexLine returns the hex coordinates on a straight line from a to b using
// cube-coordinate linear interpolation and rounding (Red Blob Games algorithm).
// The result includes both endpoints.
func HexLine(a, b Coord) []Coord {
	dist := a.Distance(b)
	if dist == 0 {
		return []Coord{a}
	}

	ac, bc := a.Cube(), b.Cube()
	out := make([]Coord, 0, dist+1)
	for i := range dist + 1 {
		t := float64(i) / float64(dist)
		out = append(out, cubeRound(cubeLerp(ac, bc, t)))
	}
	return out
}

// cubeLerp interpolates between two cube coordinates at parameter t ∈ [0, 1].
func cubeLerp(a, b Cube, t float64) fractionalCube {
	return fractionalCube{
		q: float64(a.Q) + (float64(b.Q)-float64(a.Q))*t,
		r: float64(a.R) + (float64(b.R)-float64(a.R))*t,
		s: float64(a.S) + (float64(b.S)-float64(a.S))*t,
	}
}

// fractionalCube holds floating-point cube coordinates for interpolation.
type fractionalCube struct{ q, r, s float64 }

// cubeRound converts a fractional cube coordinate to the nearest integer hex
// by rounding each component and adjusting the one with the largest delta to
// enforce q+r+s=0. (Red Blob Games "Rounding to nearest hex" algorithm.)
func cubeRound(f fractionalCube) Coord {
	q := int(math.Round(f.q))
	r := int(math.Round(f.r))
	s := int(math.Round(f.s))

	qDiff := math.Abs(float64(q) - f.q)
	rDiff := math.Abs(float64(r) - f.r)
	sDiff := math.Abs(float64(s) - f.s)

	switch {
	case qDiff > rDiff && qDiff > sDiff:
		q = -r - s
	case rDiff > sDiff:
		r = -q - s
	default:
		// s has largest diff (or tie): s = -q - r (implicit, not stored in axial)
	}

	return Coord{Q: q, R: r}
}
