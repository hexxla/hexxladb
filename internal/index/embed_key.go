package index

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// EmbedPrefix is the ASCII prefix for embedding vector keys per HEXXLA_DB.md storage layout.
const EmbedPrefix = "embed/"

// EmbedKey returns the storage key embed/<packed_coord> using the same Morton-friendly
// byte order as [CellKey].
func EmbedKey(p lattice.PackedCoord) []byte {
	buf := make([]byte, 0, len(EmbedPrefix)+PackedCoordKeyLen)
	buf = append(buf, EmbedPrefix...)
	buf = appendPackedCoordBE(buf, p)
	return buf
}

// EmbedKeyEnd returns the exclusive upper bound for an embed/ prefix scan.
func EmbedKeyEnd() []byte {
	end := []byte(EmbedPrefix)
	end[len(end)-1]++ // "embed/" → "embed0"
	return end
}

// ParseEmbedKey extracts the PackedCoord from a key returned by [EmbedKey].
func ParseEmbedKey(key []byte) (lattice.PackedCoord, error) {
	if !bytes.HasPrefix(key, []byte(EmbedPrefix)) {
		return lattice.PackedCoord{}, fmt.Errorf("index: not an embed key")
	}
	suf := key[len(EmbedPrefix):]
	if len(suf) != PackedCoordKeyLen {
		return lattice.PackedCoord{}, fmt.Errorf("index: bad embed key length")
	}
	var p lattice.PackedCoord
	p[1] = binary.BigEndian.Uint64(suf[0:8])
	p[0] = binary.BigEndian.Uint64(suf[8:16])
	return p, nil
}
