package index

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// SeamByCellsPrefix is the ASCII prefix for seam-by-cells secondary keys per HEXXLA_DB.md.
const SeamByCellsPrefix = "seam-by-cells/"

// ULIDKeyLen is the Crockford ULID string length in bytes (ASCII).
const ULIDKeyLen = 26

// minULIDKeySuffix is the lexicographically smallest 26-byte ULID key suffix (all '0').
var minULIDKeySuffix = bytes.Repeat([]byte{'0'}, ULIDKeyLen)

// maxULIDKeySuffix is a suffix that sorts after any valid Crockford ULID string (26 × 0xFF).
var maxULIDKeySuffix = bytes.Repeat([]byte{0xff}, ULIDKeyLen)

// SeamByCellsKey returns seam-by-cells/<packed_lo>/<packed_hi>/<ulid> with the same 16-byte
// big-endian PackedCoord segments as [CellKey] (Hi word then Lo word per [appendPackedCoordBE]).
func SeamByCellsKey(lo, hi lattice.PackedCoord, ulidStr string) ([]byte, error) {
	if _, err := ulid.Parse(ulidStr); err != nil {
		return nil, fmt.Errorf("%w: %w", record.ErrInvalidULID, err)
	}
	if len(ulidStr) != ULIDKeyLen {
		return nil, fmt.Errorf("%w: bad ulid length", record.ErrInvalidULID)
	}
	out := make([]byte, 0, len(SeamByCellsPrefix)+2*(PackedCoordKeyLen+1)+ULIDKeyLen)
	out = append(out, SeamByCellsPrefix...)
	out = appendPackedCoordBE(out, lo)
	out = append(out, '/')
	out = appendPackedCoordBE(out, hi)
	out = append(out, '/')
	out = append(out, ulidStr...)
	return out, nil
}

// ParseSeamByCellsKey parses a key built by [SeamByCellsKey]. The ULID must be valid Crockford.
func ParseSeamByCellsKey(key []byte) (lo, hi lattice.PackedCoord, ulidStr string, err error) {
	if !bytes.HasPrefix(key, []byte(SeamByCellsPrefix)) {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("index: not a seam-by-cells key")
	}
	rest := key[len(SeamByCellsPrefix):]
	if len(rest) != PackedCoordKeyLen+1+PackedCoordKeyLen+1+ULIDKeyLen {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("index: bad seam-by-cells key length")
	}
	lo, err = parsePackedCoordSegment(rest[:PackedCoordKeyLen])
	if err != nil {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", err
	}
	if rest[PackedCoordKeyLen] != '/' {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("index: bad seam-by-cells separator")
	}
	rest = rest[PackedCoordKeyLen+1:]
	hi, err = parsePackedCoordSegment(rest[:PackedCoordKeyLen])
	if err != nil {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", err
	}
	if rest[PackedCoordKeyLen] != '/' {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("index: bad seam-by-cells separator")
	}
	ulidStr = string(rest[PackedCoordKeyLen+1:])
	if _, err := ulid.Parse(ulidStr); err != nil {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("%w: %w", record.ErrInvalidULID, err)
	}
	return lo, hi, ulidStr, nil
}

func parsePackedCoordSegment(seg []byte) (lattice.PackedCoord, error) {
	if len(seg) != PackedCoordKeyLen {
		return lattice.PackedCoord{}, fmt.Errorf("index: bad packed segment length")
	}
	var p lattice.PackedCoord
	p[1] = binaryUint64BE(seg[0:8])
	p[0] = binaryUint64BE(seg[8:16])
	return p, nil
}

func binaryUint64BE(b []byte) uint64 {
	_ = b[7]
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}

// SeamByCellsScanUpperBound returns an upper bound for AscendRange over all seam-by-cells keys
// (lexicographic): every key starting with [SeamByCellsPrefix] and valid layout is strictly less.
func SeamByCellsScanUpperBound() []byte {
	return []byte("seam-by-cells0")
}

// seamByCellsKeyRaw builds seam-by-cells/<lo>/<hi>/<26-byte suffix> without validating ULID
// (for inclusive AscendRange endpoints).
func seamByCellsKeyRaw(lo, hi lattice.PackedCoord, ulidSuffix []byte) []byte {
	if len(ulidSuffix) != ULIDKeyLen {
		panic("index: ulid suffix must be 26 bytes")
	}
	out := make([]byte, 0, len(SeamByCellsPrefix)+2*(PackedCoordKeyLen+1)+ULIDKeyLen)
	out = append(out, SeamByCellsPrefix...)
	out = appendPackedCoordBE(out, lo)
	out = append(out, '/')
	out = appendPackedCoordBE(out, hi)
	out = append(out, '/')
	out = append(out, ulidSuffix...)
	return out
}

// SeamByCellsRangeLoFixed returns inclusive [from, to] for AscendRange over all secondary keys
// whose first packed segment equals lo and second segment is any hi >= lo (canonical seam keys
// with that fixed lo). Uses min/max ULID suffixes for bounds.
func SeamByCellsRangeLoFixed(lo lattice.PackedCoord) (from, to []byte) {
	from = seamByCellsKeyRaw(lo, lo, minULIDKeySuffix)
	maxHi := maxPackedKeyBytes()
	to = seamByCellsKeyRaw(lo, maxHi, maxULIDKeySuffix)
	return from, to
}

// SeamByCellsRangeHiFixedLoLess returns inclusive [from, to] for keys with second packed segment
// hi, first segment lo < hi. If there is no lo' < hi in 128-bit key space (hi is zero), ok is false.
func SeamByCellsRangeHiFixedLoLess(hi lattice.PackedCoord) (from, to []byte, ok bool) {
	hiBytes := packedCoordToKeyBytes(hi)
	pred, ok := packedKeyBytesDec(hiBytes)
	if !ok {
		return nil, nil, false
	}
	loMax, ok := bytesToPackedCoord(pred)
	if !ok {
		return nil, nil, false
	}
	// All canonical pairs with hi fixed and lo < hi: lo ranges from 0 to pred(hi).
	minLo := lattice.PackedCoord{}
	from = seamByCellsKeyRaw(minLo, hi, minULIDKeySuffix)
	to = seamByCellsKeyRaw(loMax, hi, maxULIDKeySuffix)
	return from, to, true
}

func packedCoordToKeyBytes(p lattice.PackedCoord) []byte {
	b := make([]byte, PackedCoordKeyLen)
	copy(b, appendPackedCoordBE(nil, p))
	return b
}

func bytesToPackedCoord(b []byte) (lattice.PackedCoord, bool) {
	if len(b) != PackedCoordKeyLen {
		return lattice.PackedCoord{}, false
	}
	var p lattice.PackedCoord
	p[1] = binaryUint64BE(b[0:8])
	p[0] = binaryUint64BE(b[8:16])
	return p, true
}

func maxPackedKeyBytes() lattice.PackedCoord {
	return lattice.PackedCoord{^uint64(0), ^uint64(0)}
}

func packedKeyBytesDec(packed []byte) ([]byte, bool) {
	out := append([]byte(nil), packed...)
	for i := range slices.Backward(out) {
		if out[i] != 0 {
			out[i]--
			return out, true
		}
		out[i] = 0xff
	}
	return nil, false
}
