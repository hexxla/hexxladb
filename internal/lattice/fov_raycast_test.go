package lattice

import (
	"maps"
	"math"
	"slices"
)

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
	return slices.Collect(maps.Keys(visible))
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
