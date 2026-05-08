package lattice

import "iter"

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

// RingInto appends the cells at exactly hex distance k from center into dst and
// returns the extended slice. Unlike [Ring], no new backing array is allocated when
// dst has sufficient capacity. Callers that loop over multiple rings should
// pre-allocate dst with capacity 3*maxR*maxR+3*maxR+1 and reuse it across iterations.
func RingInto(dst []Coord, center Coord, k int) []Coord {
	if k < 0 {
		return dst
	}
	cb := center.Cube()
	if k == 0 {
		return append(dst, center)
	}
	cur := cubeAdd(cb, cubeScale(cubeDirections[0], k))
	for i := range 6 {
		for range k {
			dst = append(dst, cubeToCoord(cur))
			cur = cubeNeighbor(cur, (i+2)%6)
		}
	}
	return dst
}

// SpiralRange appends all coordinates in rings [minR, maxR] from center into dst,
// in ascending ring order. The center cell is included when minR <= 0.
// Returns dst unchanged when maxR < 0 or minR > maxR.
func SpiralRange(dst []Coord, center Coord, minR, maxR int) []Coord {
	if maxR < 0 || minR > maxR {
		return dst
	}
	if minR <= 0 {
		dst = append(dst, center)
	}
	start := max(minR, 1)
	for k := start; k <= maxR; k++ {
		dst = RingInto(dst, center, k)
	}
	return dst
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

// RingSeq returns a lazy iterator over all cells at exactly hex distance k from center.
// Yields cells one at a time with no intermediate slice allocation.
// Callers may break early; no further work is done for unyielded cells.
func RingSeq(center Coord, k int) iter.Seq[Coord] {
	return func(yield func(Coord) bool) {
		if k < 0 {
			return
		}
		cb := center.Cube()
		if k == 0 {
			yield(center)
			return
		}
		cur := cubeAdd(cb, cubeScale(cubeDirections[0], k))
		for i := range 6 {
			for range k {
				if !yield(cubeToCoord(cur)) {
					return
				}
				cur = cubeNeighbor(cur, (i+2)%6)
			}
		}
	}
}

// WalkRingsSeq returns a lazy iterator over rings 0..maxR from center (center first,
// then outward ring by ring). Yields coords one at a time; callers may break early.
// No slice is allocated regardless of maxR.
func WalkRingsSeq(center Coord, maxR int) iter.Seq[Coord] {
	return func(yield func(Coord) bool) {
		if maxR < 0 {
			return
		}
		if !yield(center) {
			return
		}
		for k := 1; k <= maxR; k++ {
			for c := range RingSeq(center, k) {
				if !yield(c) {
					return
				}
			}
		}
	}
}

// SpiralRangeSeq returns a lazy iterator over rings [minR, maxR] from center.
// The center cell is included when minR <= 0. Callers may break early.
// No slice is allocated regardless of the range size.
func SpiralRangeSeq(center Coord, minR, maxR int) iter.Seq[Coord] {
	return func(yield func(Coord) bool) {
		if maxR < 0 || minR > maxR {
			return
		}
		if minR <= 0 {
			if !yield(center) {
				return
			}
		}
		start := max(minR, 1)
		for k := start; k <= maxR; k++ {
			for c := range RingSeq(center, k) {
				if !yield(c) {
					return
				}
			}
		}
	}
}
