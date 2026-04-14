package index

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// EdgePrefix is the ASCII prefix for edge keys per HEXXLA_DB.md: edge/<from>/<to>/<type>.
const EdgePrefix = "edge/"

// maxRelationTypeBytes bounds relation type length in keys (DoS / btree key size).
const maxRelationTypeBytes = 16 << 20 // 16 MiB, aligned with record payload scale

// EdgeKey returns edge/<from>/<to>/<uint32be_len><utf8_relation_type>.
func EdgeKey(from, to lattice.PackedCoord, relationType string) ([]byte, error) {
	if relationType == "" {
		return nil, ErrEmptyRelationType
	}
	rt := []byte(relationType)
	if len(rt) > maxRelationTypeBytes {
		return nil, fmt.Errorf("index: relation type too long")
	}
	// len(rt) is bounded by maxRelationTypeBytes, so it fits in uint32.
	n := uint32(len(rt)) // #nosec G115
	buf := make([]byte, 0, len(EdgePrefix)+PackedCoordKeyLen+1+PackedCoordKeyLen+1+4+len(rt))
	buf = append(buf, EdgePrefix...)
	buf = appendPackedCoordBE(buf, from)
	buf = append(buf, '/')
	buf = appendPackedCoordBE(buf, to)
	buf = append(buf, '/')
	var lenBE [4]byte
	binary.BigEndian.PutUint32(lenBE[:], n)
	buf = append(buf, lenBE[:]...)
	buf = append(buf, rt...)
	return buf, nil
}

// EdgeFromPrefix returns the key prefix for all edges with the given from-cell (for AscendRange + HasPrefix).
func EdgeFromPrefix(from lattice.PackedCoord) []byte {
	buf := make([]byte, 0, len(EdgePrefix)+PackedCoordKeyLen+1)
	buf = append(buf, EdgePrefix...)
	buf = appendPackedCoordBE(buf, from)
	buf = append(buf, '/')
	return buf
}

// ParseEdgeKey parses a key built by [EdgeKey].
func ParseEdgeKey(key []byte) (from, to lattice.PackedCoord, relationType string, err error) {
	if !bytes.HasPrefix(key, []byte(EdgePrefix)) {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("index: not an edge key")
	}
	rest := key[len(EdgePrefix):]
	if len(rest) < PackedCoordKeyLen+1+PackedCoordKeyLen+1+4 {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("index: bad edge key (truncated)")
	}
	from, err = parsePackedCoordSegment(rest[:PackedCoordKeyLen])
	if err != nil {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", err
	}
	if rest[PackedCoordKeyLen] != '/' {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("index: bad edge separator after from")
	}
	rest = rest[PackedCoordKeyLen+1:]
	to, err = parsePackedCoordSegment(rest[:PackedCoordKeyLen])
	if err != nil {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", err
	}
	if rest[PackedCoordKeyLen] != '/' {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("index: bad edge separator after to")
	}
	rest = rest[PackedCoordKeyLen+1:]
	if len(rest) < 4 {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("index: bad edge key length field")
	}
	n := binary.BigEndian.Uint32(rest[0:4])
	rest = rest[4:]
	if uint64(len(rest)) != uint64(n) {
		return lattice.PackedCoord{}, lattice.PackedCoord{}, "", fmt.Errorf("index: edge relation length mismatch")
	}
	return from, to, string(rest[:n]), nil
}

// ErrEmptyRelationType is returned when relation type is required but empty.
var ErrEmptyRelationType = errors.New("index: empty relation type")
