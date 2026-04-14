package lattice

import (
	"errors"
	"math"
)

// Coordinate bounds for Pack / Unpack (zigzag + 21-bit Morton limbs).
//
// Each packed axis uses 21 bits of zigzag payload (values 0..2^21-1). That
// corresponds to signed cube coordinates in [-2^20, 2^20-1] inclusive per axis.
//
// Not every pair (q,r) with both in that range is valid: we require the derived
// cube triple (q, r, s) with s = -q-r to satisfy the same per-axis bound. The
// tightest convenient axial box is |Q| ≤ MaxAxialAbs and |R| ≤ MaxAxialAbs with
// MaxAxialAbs = 2^18-1, which keeps |S| ≤ 2^19-2.
const (
	// MaxAxialAbs is the maximum absolute value for Q and R in Pack/Unpack.
	MaxAxialAbs = (1 << 18) - 1

	maxCubeZigzagBits = 21
	maxZigzagLimb     = (1 << maxCubeZigzagBits) - 1
)

// ErrCoordOutOfRange means the coordinate is outside the Pack-valid cube box.
var ErrCoordOutOfRange = errors.New("lattice: coordinate out of packed range")

// zigzag64 maps a signed int to an unsigned for Morton interleaving (branch form avoids
// signed-shift overflow at int64 min and unchecked int→uint casts).
func zigzag64(n int64) uint64 {
	if n >= 0 {
		return uint64(n) * 2
	}
	// n < 0: zigzag is 2|n|-1; k is in [0, MaxInt64] when n is in [MinInt64, -1].
	k := -(n + 1)
	if k < 0 {
		panic("lattice: zigzag64 internal error")
	}
	return uint64(k)*2 + 1
}

func unzigzag64(z uint64) int64 {
	if z&1 == 0 {
		return int64(z >> 1)
	}
	t := (z + 1) >> 1
	if t == 0 {
		return math.MinInt64
	}
	return -int64(t)
}

func inPackRangeCube(q, r, s int) bool {
	if q > MaxAxialAbs || q < -MaxAxialAbs {
		return false
	}
	if r > MaxAxialAbs || r < -MaxAxialAbs {
		return false
	}
	if s > MaxAxialAbs || s < -MaxAxialAbs {
		return false
	}
	return true
}

func zigzagFits21(z uint64) bool {
	return z <= maxZigzagLimb
}
