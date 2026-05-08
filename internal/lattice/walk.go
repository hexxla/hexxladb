package lattice

// WalkRingsPacked generates coordinates for rings 0..maxR from center and packs
// each into a PackedCoord. Returns the packed coords in ring order (center first,
// then ring 1, ring 2, etc). Coords that fall outside the packable range are
// silently skipped. This avoids repeated Pack calls at the call site.
func WalkRingsPacked(center Coord, maxR int) []PackedCoord {
	if maxR < 0 {
		return nil
	}
	total := 3*maxR*maxR + 3*maxR + 1
	out := make([]PackedCoord, 0, total)
	buf := make([]Coord, 0, 6*maxR+1)

	for k := range maxR + 1 {
		buf = RingInto(buf[:0], center, k)
		for _, c := range buf {
			p, err := Pack(c)
			if err != nil {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}

// WalkRingsCoordPacked generates (Coord, PackedCoord) pairs for rings 0..maxR.
// Useful when the caller needs both the logical coordinate and packed key.
func WalkRingsCoordPacked(center Coord, maxR int) []CoordPacked {
	if maxR < 0 {
		return nil
	}
	total := 3*maxR*maxR + 3*maxR + 1
	out := make([]CoordPacked, 0, total)
	buf := make([]Coord, 0, 6*maxR+1)

	for k := range maxR + 1 {
		buf = RingInto(buf[:0], center, k)
		for _, c := range buf {
			p, err := Pack(c)
			if err != nil {
				continue
			}
			out = append(out, CoordPacked{Coord: c, Packed: p})
		}
	}
	return out
}

// CoordPacked pairs a logical coordinate with its packed representation.
type CoordPacked struct {
	Coord  Coord
	Packed PackedCoord
}
