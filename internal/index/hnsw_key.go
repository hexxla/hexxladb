package index

import (
	"bytes"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// HNSW keyspace prefixes.
const (
	HNSWMetaKey    = "hnsw/meta"
	HNSWEntryKey   = "hnsw/entry"
	HNSWStateKey   = "hnsw/state"
	HNSWNodePrefix = "hnsw/node/"
)

// HNSWNodeKey returns the storage key hnsw/node/<packed_coord>.
func HNSWNodeKey(p lattice.PackedCoord) []byte {
	buf := make([]byte, 0, len(HNSWNodePrefix)+PackedCoordKeyLen)
	buf = append(buf, HNSWNodePrefix...)
	buf = appendPackedCoordBE(buf, p)
	return buf
}

// ParseHNSWNodeKey returns the coordinate encoded by [HNSWNodeKey].
func ParseHNSWNodeKey(key []byte) (lattice.PackedCoord, error) {
	suffix, ok := bytes.CutPrefix(key, []byte(HNSWNodePrefix))
	if !ok || len(suffix) != PackedCoordKeyLen {
		return lattice.PackedCoord{}, fmt.Errorf("index: invalid HNSW node key")
	}
	return parsePackedCoordSegment(suffix)
}

// HNSWNodeKeyEnd returns the inclusive upper bound of the HNSW node keyspace.
func HNSWNodeKeyEnd() []byte {
	end := make([]byte, len(HNSWNodePrefix)+PackedCoordKeyLen)
	copy(end, HNSWNodePrefix)
	for i := len(HNSWNodePrefix); i < len(end); i++ {
		end[i] = 0xff
	}
	return end
}
