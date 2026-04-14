package hexxladb

import "github.com/hexxla/hexxladb/internal/lattice"

// Coord is an axial hex coordinate (q, r); cube s = -q - r. See [Coord.Cube].
type Coord = lattice.Coord

// Cube holds cube coordinates with q + r + s == 0.
type Cube = lattice.Cube

// PackedCoord is a 128-bit Morton-order key; see internal/lattice/PACKED_COORD.md.
type PackedCoord = lattice.PackedCoord

// MaxAxialAbs is the maximum absolute Q and R allowed by [Pack].
const MaxAxialAbs = lattice.MaxAxialAbs

// ErrCoordOutOfRange means coordinates are outside the range accepted by [Pack].
var ErrCoordOutOfRange = lattice.ErrCoordOutOfRange

// Pack encodes an axial coordinate to a Morton [PackedCoord].
func Pack(c Coord) (PackedCoord, error) { return lattice.Pack(c) }

// Unpack decodes a v1 [PackedCoord] with reserved high word zero.
func Unpack(p PackedCoord) (Coord, error) { return lattice.Unpack(p) }

// Ring returns all cells at hex distance k from center in load_context order.
func Ring(center Coord, k int) []Coord { return lattice.Ring(center, k) }

// WalkRings appends center, then rings 1..maxR, each in [Ring] order.
func WalkRings(dst []Coord, center Coord, maxR int) []Coord {
	return lattice.WalkRings(dst, center, maxR)
}
