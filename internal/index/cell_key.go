package index

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// CellPrefix is the ASCII prefix for primary cell keys per HEXXLA_DB.md storage layout.
const CellPrefix = "cell/"

// PackedCoordKeyLen is the length of the encoded PackedCoord suffix (bytes).
const PackedCoordKeyLen = 16

// CellKey returns the full storage key cell/<packed> with Morton-friendly byte order:
// high word (Hi) then low word (Lo), each big-endian uint64, so [bytes.Compare] on
// full keys matches [lattice.PackedCoord.Compare].
func CellKey(p lattice.PackedCoord) []byte {
	buf := make([]byte, 0, len(CellPrefix)+PackedCoordKeyLen)
	buf = append(buf, CellPrefix...)
	buf = appendPackedCoordBE(buf, p)
	return buf
}

func appendPackedCoordBE(dst []byte, p lattice.PackedCoord) []byte {
	var tmp [PackedCoordKeyLen]byte
	binary.BigEndian.PutUint64(tmp[0:8], p[1])
	binary.BigEndian.PutUint64(tmp[8:16], p[0])
	return append(dst, tmp[:]...)
}

// ParseCellKey extracts the PackedCoord from a key returned by [CellKey]. Returns an
// error if the prefix or length is wrong.
func ParseCellKey(key []byte) (lattice.PackedCoord, error) {
	if !bytes.HasPrefix(key, []byte(CellPrefix)) {
		return lattice.PackedCoord{}, fmt.Errorf("index: not a cell key")
	}
	suf := key[len(CellPrefix):]
	if len(suf) != PackedCoordKeyLen {
		return lattice.PackedCoord{}, fmt.Errorf("index: bad cell key length")
	}
	var p lattice.PackedCoord
	p[1] = binary.BigEndian.Uint64(suf[0:8])
	p[0] = binary.BigEndian.Uint64(suf[8:16])
	return p, nil
}
