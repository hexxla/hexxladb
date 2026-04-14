package lattice

// cubeDirections matches Red Blob Games cube neighbor order for pointy-top hexes
// (used for ring perimeter walks). Index i is the offset added for neighbor i.
var cubeDirections = [6]Cube{
	{1, 0, -1},
	{1, -1, 0},
	{0, -1, 1},
	{-1, 0, 1},
	{-1, 1, 0},
	{0, 1, -1},
}

func cubeAdd(a, b Cube) Cube {
	return Cube{a.Q + b.Q, a.R + b.R, a.S + b.S}
}

func cubeScale(d Cube, k int) Cube {
	return Cube{d.Q * k, d.R * k, d.S * k}
}

func cubeNeighbor(c Cube, dir int) Cube {
	d := cubeDirections[dir]
	return Cube{c.Q + d.Q, c.R + d.R, c.S + d.S}
}

func cubeToCoord(c Cube) Coord {
	return Coord{Q: c.Q, R: c.R}
}

// Ring returns all cells at exactly hex distance k from center, in load_context order:
// start at the +q axial direction from center (cube +k·(1,0,-1)), then walk the ring
// counterclockwise using the Red Blob Games ring algorithm (six sides × k steps).
// For k == 0 the result is a single-element slice containing center.
func Ring(center Coord, k int) []Coord {
	if k < 0 {
		return nil
	}
	cb := center.Cube()
	if k == 0 {
		return []Coord{center}
	}
	out := make([]Coord, 0, 6*k)
	// Positive-q start: directions[0] * k in cube space.
	cur := cubeAdd(cb, cubeScale(cubeDirections[0], k))
	for i := range 6 {
		for range k {
			out = append(out, cubeToCoord(cur))
			cur = cubeNeighbor(cur, (i+2)%6)
		}
	}
	return out
}

// WalkRings appends ring 0 (center), then rings 1..maxR, each ring in Ring order
// (matches load_context: concentric rings outward, positive-q start within each ring).
// If maxR < 0, dst is unchanged; if maxR == 0, only center is appended.
func WalkRings(dst []Coord, center Coord, maxR int) []Coord {
	if maxR < 0 {
		return dst
	}
	dst = append(dst, center)
	for k := 1; k <= maxR; k++ {
		dst = append(dst, Ring(center, k)...)
	}
	return dst
}
