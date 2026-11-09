package lattice

import (
	"cmp"
	"context"
	"slices"
	"sort"
)

// FieldOfView computes visible coordinates from origin up to maxRadius.
// Opaque hexes block centre-to-centre sight lines, including boundary contact.
// The first blocking hex is visible. Results are deterministic and nearest-first.
// Returns nil if maxRadius < 0 or opaque is nil. Returns {origin} if maxRadius == 0.
func FieldOfView(origin Coord, maxRadius int, opaque func(Coord) bool) []Coord {
	visible, _ := FieldOfViewContext(context.Background(), origin, maxRadius, opaque)
	return visible
}

// FieldOfViewContext casts the angular shadows of regular hexagons using
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
func FieldOfViewContext(ctx context.Context, origin Coord, maxRadius int, opaque func(Coord) bool) ([]Coord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxRadius < 0 || opaque == nil {
		return nil, nil
	}
	if maxRadius == 0 {
		return []Coord{origin}, nil
	}
	numRays := 3 * maxRadius * (maxRadius + 1)
	rays := WalkRings(make([]Coord, 0, numRays+1), Coord{}, maxRadius)[1:]
	cells := make([]fovCell, numRays)
	done := ctx.Done()
	hasBlocker := false
	for i, offset := range rays {
		select {
		case <-done:
			return nil, ctx.Err()
		default:
		}
		blocked := opaque(Coord{Q: origin.Q + offset.Q, R: origin.R + offset.R})
		cells[i] = fovCell{offset: offset, blocked: blocked}
		hasBlocker = hasBlocker || blocked
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	visible := make([]Coord, 1, numRays+1)
	visible[0] = origin
	if !hasBlocker {
		for _, cell := range cells {
			visible = append(visible, Coord{Q: origin.Q + cell.offset.Q, R: origin.R + cell.offset.R})
		}
		sortFOVVisible(visible, origin)
		return visible, ctx.Err()
	}
	slices.SortFunc(cells, func(a, b fovCell) int { return compareFOVDirections(a.offset, b.offset) })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range cells {
		rays[i] = cells[i].offset
		cells[i].ray = i
	}
	slices.SortFunc(cells, func(a, b fovCell) int {
		return cmp.Compare(fovDistanceSquared(a.offset), fovDistanceSquared(b.offset))
	})
	shadows := make(fovShadows, len(rays)+1)
	for _, cell := range cells {
		select {
		case <-done:
			return nil, ctx.Err()
		default:
		}
		coord := Coord{Q: origin.Q + cell.offset.Q, R: origin.R + cell.offset.R}
		if !shadows.contains(cell.ray) {
			visible = append(visible, coord)
		}
		if cell.blocked {
			shadows.cast(cell.offset, rays)
		}
	}
	sortFOVVisible(visible, origin)
	return visible, ctx.Err()
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
