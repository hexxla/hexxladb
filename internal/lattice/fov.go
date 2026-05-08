package lattice

import (
	"math"
	"slices"
)

// FieldOfView computes visible coordinates from origin up to maxRadius.
// Delegates to FieldOfViewShadowcast for accuracy and O(visible) performance.
//
// Opaque cells at the boundary ARE included (visible but blocking).
// Returns nil if maxRadius < 0 or opaque is nil. Returns {origin} if maxRadius == 0.
func FieldOfView(origin Coord, maxRadius int, opaque func(Coord) bool) []Coord {
	return FieldOfViewShadowcast(origin, maxRadius, opaque)
}

// FieldOfViewShadowcast computes visible coordinates using symmetric shadowcasting
// (Albert Ford 2021, hex adaptation). Six sextants cover the hex grid around origin.
// Within each sextant, shadow slopes track blocked angular ranges, scanning outward
// row by row. Each cell is visited at most once: O(visible cells).
//
// Guarantees full symmetry: if A sees B then B sees A (for floor tiles).
// Opaque cells at the boundary are included (the wall is seen but blocks behind it).
// Returns nil if maxRadius < 0 or opaque is nil. Returns {origin} if maxRadius == 0.
func FieldOfViewShadowcast(origin Coord, maxRadius int, opaque func(Coord) bool) []Coord {
	if maxRadius < 0 || opaque == nil {
		return nil
	}
	visible := make(map[Coord]struct{}, 1+3*maxRadius*(maxRadius+1))
	visible[origin] = struct{}{}
	if maxRadius == 0 {
		return []Coord{origin}
	}
	for s := range 6 {
		scanSextant(origin, maxRadius, s, opaque, visible)
	}
	out := make([]Coord, 0, len(visible))
	for c := range visible {
		out = append(out, c)
	}
	return out
}

// sextantBasis defines the (rowQ, rowR, colQ, colR) axial components for each
// of the 6 hex sextants. Each sextant covers exactly one side of each ring:
//   - row vector = cubeDirections[i] in axial (outward corner direction)
//   - col vector = ring walk step direction for side i (sweeps along ring side)
//
// Derived from the ring walk in ring.go: side i starts at cubeDirections[i]*d
// and walks d steps in axial direction (i+2)%6.
var sextantBasis = [6][4]int{
	{1, 0, 0, -1},  // side 0: out={1,0},  step={0,-1}
	{1, -1, -1, 0}, // side 1: out={1,-1}, step={-1,0}
	{0, -1, -1, 1}, // side 2: out={0,-1}, step={-1,1}
	{-1, 0, 0, 1},  // side 3: out={-1,0}, step={0,1}
	{-1, 1, 1, 0},  // side 4: out={-1,1}, step={1,0}
	{0, 1, 1, -1},  // side 5: out={0,1},  step={1,-1}
}

// fovRow holds the slope bounds and depth for a single shadowcasting scan row.
// Slopes are in [0, 1] across the face of one ring side.
type fovRow struct {
	depth      int
	startSlope frac // inclusive; tiles with slope > startSlope are in light
	endSlope   frac // inclusive; tiles with slope < endSlope are in light
}

// scanSextant performs the shadowcasting scan for one sextant using an explicit
// stack to avoid recursion depth limits at large maxRadius.
func scanSextant(origin Coord, maxRadius, sextant int, opaque func(Coord) bool, visible map[Coord]struct{}) {
	b := sextantBasis[sextant]
	rQ, rR, cQ, cR := b[0], b[1], b[2], b[3]

	stack := []fovRow{{depth: 1, startSlope: frac{0, 1}, endSlope: frac{1, 1}}}
	for len(stack) > 0 {
		row := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if row.depth > maxRadius {
			continue
		}
		stack = scanRow(origin, row, maxRadius, rQ, rR, cQ, cR, opaque, visible, stack)
	}
}

// scanRow processes one depth row within a sextant. Columns run 0..depth-1.
// Slopes are (col + 0.5) / depth for tile centres, boundaries at (col ± 0.5) / depth.
// Returns the updated stack with child rows to process.
func scanRow(origin Coord, row fovRow, maxRadius, rQ, rR, cQ, cR int, opaque func(Coord) bool, visible map[Coord]struct{}, stack []fovRow) []fovRow {
	// Ford's formula: min_col = roundTiesUp(depth * startSlope),
	//                 max_col = roundTiesDown(depth * endSlope).
	minCol := roundTiesUp(mulFrac(row.startSlope, row.depth))
	maxCol := roundTiesDown(mulFrac(row.endSlope, row.depth))
	if minCol < 0 {
		minCol = 0
	}
	if maxCol >= row.depth {
		maxCol = row.depth - 1
	}

	prevOpaque := false
	for col := minCol; col <= maxCol; col++ {
		c := Coord{
			Q: origin.Q + row.depth*rQ + col*cQ,
			R: origin.R + row.depth*rR + col*cR,
		}
		if origin.Distance(c) > maxRadius {
			continue
		}
		isOpaque := opaque(c)
		if isOpaque || isSymmetric(row, col) {
			visible[c] = struct{}{}
		}
		if prevOpaque && !isOpaque {
			row.startSlope = tileSlope(col, row.depth)
		}
		if !prevOpaque && isOpaque {
			stack = append(stack, fovRow{
				depth:      row.depth + 1,
				startSlope: row.startSlope,
				endSlope:   tileSlope(col, row.depth),
			})
		}
		prevOpaque = isOpaque
	}
	if !prevOpaque {
		stack = append(stack, fovRow{depth: row.depth + 1, startSlope: row.startSlope, endSlope: row.endSlope})
	}
	return stack
}

// isSymmetric returns true when the tile centre at col falls within [startSlope, endSlope].
// Tile centre slope = col / depth. Uses cross-multiplication for exact integer arithmetic.
// col/depth >= start.num/start.den  →  col*start.den >= start.num*depth
// col/depth <= end.num/end.den      →  col*end.den   <= end.num*depth
func isSymmetric(row fovRow, col int) bool {
	lhs := col*row.startSlope.den >= row.startSlope.num*row.depth
	rhs := col*row.endSlope.den <= row.endSlope.num*row.depth
	return lhs && rhs
}

// tileSlope returns the left-edge slope for a tile: (col - 0.5) / depth = (2*col-1)/(2*depth).
// Used for both start and end slope updates, matching Ford's single slope(tile) function.
func tileSlope(col, depth int) frac { return frac{2*col - 1, 2 * depth} }

// frac is an integer fraction num/den (den > 0).
type frac struct{ num, den int }

// mulFrac returns (f.num/f.den) * n as float64.
func mulFrac(f frac, n int) float64 { return float64(f.num*n) / float64(f.den) }

// roundTiesUp returns floor(n + 0.5).
func roundTiesUp(n float64) int { return int(math.Floor(n + 0.5)) }

// roundTiesDown returns ceil(n - 0.5).
func roundTiesDown(n float64) int { return int(math.Ceil(n - 0.5)) }

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
