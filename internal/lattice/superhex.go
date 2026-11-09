package lattice

// SuperHexParent maps a fine coordinate to its parent in an aperture-7 hex
// hierarchy. Every parent owns exactly seven fine cells: the transformed center
// and its six immediate neighbors.
//
// The parent lattice basis vectors are (2,1) and (-1,3), whose determinant is
// seven. The child offset follows from the fine coordinate modulo seven.
func SuperHexParent(c Coord) Coord {
	// The coarse centers satisfy 3q+r ≡ 0 (mod 7). The center and its six
	// neighbors have distinct residues, so the residue selects one child offset.
	residue := (3*c.Q + c.R) % 7
	if residue < 0 {
		residue += 7
	}
	offset := superHexChildOffsets[residue]
	q, r := c.Q-offset.Q, c.R-offset.R
	return Coord{Q: (3*q + r) / 7, R: (-q + 2*r) / 7}
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

var superHexChildOffsets = [7]Coord{
	{},
	{R: 1},
	{Q: 1, R: -1},
	{Q: 1},
	{Q: -1},
	{Q: -1, R: 1},
	{R: -1},
}
