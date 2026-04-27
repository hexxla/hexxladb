package index

import "github.com/hexxla/hexxladb/internal/lattice"

// HNSW keyspace prefixes.
const (
	HNSWMetaKey  = "hnsw/meta"
	HNSWEntryKey = "hnsw/entry"
	hnswNodePfx  = "hnsw/node/"
)

// HNSWNodeKey returns the storage key hnsw/node/<packed_coord>.
func HNSWNodeKey(p lattice.PackedCoord) []byte {
	buf := make([]byte, 0, len(hnswNodePfx)+PackedCoordKeyLen)
	buf = append(buf, hnswNodePfx...)
	buf = appendPackedCoordBE(buf, p)
	return buf
}
