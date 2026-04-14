package lattice

// PackedCoord is a 128-bit Morton order key for cube coordinates.
//
// Layout (v1 — locked for engine key stability; bump format if this changes):
//
//   - Word index [0] is Lo (bits 0..63 of the 128-bit value); [1] is Hi (bits 64..127).
//   - Total order for Compare: (Hi, Lo) as unsigned 128-bit big-endian style
//     (high word compared first).
//   - Hi is 0 for coordinates in the v1 world extent (reserved for future region prefix).
//   - Lo bits 0..62 hold a 63-bit Morton code; Lo bit 63 is 0 in v1.
//
// Morton construction: let q', r', s' be 21-bit zigzag encodings of cube (Q,R,S).
// Bits are interleaved in fixed cyclic order q'₀, r'₀, s'₀, q'₁, r'₁, s'₁, … for
// indices 0..20, producing 63 bits (21×3). Those occupy Lo bits 0..62.
//
// On-disk / byte order: when serializing the two uint64 words, use the same order
// as Go memory (Lo first, then Hi) if both are written little-endian; logical bit 0
// is the LSB of Lo.
//
// Overflow: Pack rejects coordinates outside the documented range with ErrCoordOutOfRange
// (no clamping). See MaxAxialAbs and bounds.go.
type PackedCoord [2]uint64

// Pack returns the Morton-packed key for c. Cube coordinates must lie in the
// valid packed range (see MaxAxialAbs).
func Pack(c Coord) (PackedCoord, error) {
	cb := c.Cube()
	q, r, s := int64(cb.Q), int64(cb.R), int64(cb.S)
	if !inPackRangeCube(cb.Q, cb.R, cb.S) {
		return PackedCoord{}, ErrCoordOutOfRange
	}
	qp := zigzag64(q)
	rp := zigzag64(r)
	sp := zigzag64(s)
	if !zigzagFits21(qp) || !zigzagFits21(rp) || !zigzagFits21(sp) {
		return PackedCoord{}, ErrCoordOutOfRange
	}
	lo := mortonPack63(qp, rp, sp)
	return PackedCoord{lo, 0}, nil
}

// Unpack decodes a v1 PackedCoord with Hi==0. Reserved Hi bits are rejected.
func Unpack(p PackedCoord) (Coord, error) {
	if p[1] != 0 {
		return Coord{}, ErrCoordOutOfRange
	}
	if p[0]>>63 != 0 {
		return Coord{}, ErrCoordOutOfRange
	}
	qp, rp, sp := mortonUnpack63(p[0])
	if !zigzagFits21(qp) || !zigzagFits21(rp) || !zigzagFits21(sp) {
		return Coord{}, ErrCoordOutOfRange
	}
	q := int(unzigzag64(qp))
	r := int(unzigzag64(rp))
	s := int(unzigzag64(sp))
	if q+r+s != 0 {
		return Coord{}, ErrCoordOutOfRange
	}
	if !inPackRangeCube(q, r, s) {
		return Coord{}, ErrCoordOutOfRange
	}
	return Coord{Q: q, R: r}, nil
}

// Compare returns -1 if p < q, 0 if equal, +1 if p > q (total order on 128-bit keys).
func (p PackedCoord) Compare(q PackedCoord) int {
	if p[1] < q[1] {
		return -1
	}
	if p[1] > q[1] {
		return 1
	}
	if p[0] < q[0] {
		return -1
	}
	if p[0] > q[0] {
		return 1
	}
	return 0
}

// Less is p < q in the same total order as Compare.
func (p PackedCoord) Less(q PackedCoord) bool {
	return p.Compare(q) < 0
}

func mortonPack63(qp, rp, sp uint64) uint64 {
	var m uint64
	for i := range 21 {
		m |= (qp >> i & 1) << (3 * i)
		m |= (rp >> i & 1) << (3*i + 1)
		m |= (sp >> i & 1) << (3*i + 2)
	}
	return m
}

func mortonUnpack63(m uint64) (qp, rp, sp uint64) {
	for i := range 21 {
		qp |= (m >> (3 * i) & 1) << i
		rp |= (m >> (3*i + 1) & 1) << i
		sp |= (m >> (3*i + 2) & 1) << i
	}
	return qp, rp, sp
}
