package lattice

// SuperHexParent maps a fine coordinate to its parent in an aperture-7 hex
// hierarchy. Every parent owns exactly seven fine cells: the transformed center
// and its six immediate neighbors.
//
// The parent lattice basis vectors are (2,1) and (-1,3), whose determinant is
// seven. Candidate selection uses exact hex distance and a lexicographic
// tie-break, avoiding floating-point rounding at cluster boundaries.
func SuperHexParent(c Coord) Coord {
	approxQ := floorDiv(3*c.Q+c.R, 7)
	approxR := floorDiv(-c.Q+2*c.R, 7)
	best := Coord{Q: approxQ, R: approxR}
	bestDistance := c.Distance(SuperHexCenter(best))

	for dq := -1; dq <= 1; dq++ {
		for dr := -1; dr <= 1; dr++ {
			candidate := Coord{Q: approxQ + dq, R: approxR + dr}
			distance := c.Distance(SuperHexCenter(candidate))
			if distance < bestDistance || distance == bestDistance && coordLess(candidate, best) {
				best = candidate
				bestDistance = distance
			}
		}
	}
	return best
}

// SuperHexParentAtLevel maps c through levels aperture-7 hierarchy levels.
// Levels <= 0 return c unchanged.
func SuperHexParentAtLevel(c Coord, levels int) Coord {
	for range max(levels, 0) {
		c = SuperHexParent(c)
	}
	return c
}

// SuperHexCenter returns the fine-grid center owned by parent at the next
// coarser aperture-7 level.
func SuperHexCenter(parent Coord) Coord {
	return Coord{
		Q: 2*parent.Q - parent.R,
		R: parent.Q + 3*parent.R,
	}
}

// SuperHexCenterAtLevel expands parent through levels aperture-7 transforms.
// Levels <= 0 return parent unchanged.
func SuperHexCenterAtLevel(parent Coord, levels int) Coord {
	for range max(levels, 0) {
		parent = SuperHexCenter(parent)
	}
	return parent
}

// SuperHexChildren returns the seven fine-grid children of parent in
// deterministic center-then-ring order.
func SuperHexChildren(parent Coord) []Coord {
	center := SuperHexCenter(parent)
	children := make([]Coord, 0, 7)
	children = append(children, center)
	return RingInto(children, center, 1)
}

func coordLess(a, b Coord) bool {
	return a.Q < b.Q || a.Q == b.Q && a.R < b.R
}
