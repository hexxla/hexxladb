package lattice

import "fmt"

// CoarsenCoord maps a fine-grid coordinate to its parent at a coarser resolution.
// factor is the subdivision factor (must be ≥ 2). Each coarse cell covers
// factor² fine cells in the parallelogram [factor*Q, factor*Q+factor) ×
// [factor*R, factor*R+factor).
//
// Uses floor division so negative coordinates map correctly.
func CoarsenCoord(c Coord, factor int) (Coord, error) {
	if factor < 2 {
		return Coord{}, fmt.Errorf("lattice: CoarsenCoord factor must be >= 2, got %d", factor)
	}
	return Coord{
		Q: floorDiv(c.Q, factor),
		R: floorDiv(c.R, factor),
	}, nil
}

// RefineCoord returns the factor² fine-grid children of a coarse coordinate.
// These are all coordinates in the parallelogram [factor*Q .. factor*Q+factor-1] ×
// [factor*R .. factor*R+factor-1].
func RefineCoord(coarse Coord, factor int) ([]Coord, error) {
	if factor < 2 {
		return nil, fmt.Errorf("lattice: RefineCoord factor must be >= 2, got %d", factor)
	}
	baseQ := coarse.Q * factor
	baseR := coarse.R * factor
	out := make([]Coord, 0, factor*factor)
	for dq := range factor {
		for dr := range factor {
			out = append(out, Coord{Q: baseQ + dq, R: baseR + dr})
		}
	}
	return out, nil
}

// CoarsenMulti maps c through n levels of coarsening (each by factor).
// Equivalent to calling CoarsenCoord n times in sequence.
func CoarsenMulti(c Coord, factor, levels int) (Coord, error) {
	if factor < 2 {
		return Coord{}, fmt.Errorf("lattice: CoarsenMulti factor must be >= 2, got %d", factor)
	}
	if levels < 1 {
		return Coord{}, fmt.Errorf("lattice: CoarsenMulti levels must be >= 1, got %d", levels)
	}
	cur := c
	for range levels {
		cur = Coord{
			Q: floorDiv(cur.Q, factor),
			R: floorDiv(cur.R, factor),
		}
	}
	return cur, nil
}

// floorDiv performs integer floor division (rounds toward negative infinity),
// unlike Go's built-in integer division which truncates toward zero.
func floorDiv(a, b int) int {
	q := a / b
	if (a^b) < 0 && q*b != a {
		q--
	}
	return q
}
