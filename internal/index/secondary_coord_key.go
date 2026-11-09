package index

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// Tag and source keys share a length-prefixed identifier followed by a packed
// coordinate and an optional commit sequence. The family owns its size limit.
func secondaryCoordKey(prefix, id string, maxBytes int, errLong error, coord lattice.PackedCoord, seq *uint64) ([]byte, error) {
	capacity := len(prefix) + 2 + len(id) + 1 + PackedCoordKeyLen
	if seq != nil {
		capacity += VersionSuffixLen
	}
	key := append(make([]byte, 0, capacity), prefix...)
	key, err := appendLenPrefixedUTF8(key, id, maxBytes, errLong)
	if err != nil {
		return nil, err
	}
	key = appendPackedCoordBE(append(key, '/'), coord)
	if seq != nil {
		key = binary.BigEndian.AppendUint64(key, *seq)
	}
	return key, nil
}

func secondaryCoordBounds(prefix, id string, maxBytes int, errLong error, versioned bool) (from, to []byte, err error) {
	head, err := lenPrefixedIDPrefixAfterASCII(prefix, []byte(id), maxBytes, errLong)
	if err != nil {
		return nil, nil, err
	}
	width := PackedCoordKeyLen
	if versioned {
		width += VersionSuffixLen
	}
	from = make([]byte, len(head)+width)
	to = make([]byte, len(from))
	copy(from, head)
	copy(to, head)
	for i := len(head); i < len(to); i++ {
		to[i] = 0xff
	}
	return from, to, nil
}

func parseSecondaryCoordKey(key []byte, prefix string, maxBytes int) (string, lattice.PackedCoord, uint64, bool, error) {
	family := prefix[:len(prefix)-1]
	if !bytes.HasPrefix(key, []byte(prefix)) {
		return "", lattice.PackedCoord{}, 0, false, fmt.Errorf("index: not a %s key", family)
	}
	rest := key[len(prefix):]
	if len(rest) < 2 {
		return "", lattice.PackedCoord{}, 0, false, fmt.Errorf("index: %s key truncated", family)
	}
	n := int(binary.BigEndian.Uint16(rest))
	rest = rest[2:]
	if n > maxBytes || len(rest) < n+1+PackedCoordKeyLen {
		return "", lattice.PackedCoord{}, 0, false, fmt.Errorf("index: bad %s key layout", family)
	}
	if rest[n] != '/' {
		return "", lattice.PackedCoord{}, 0, false, fmt.Errorf("index: %s key separator", family)
	}
	id := string(rest[:n])
	tail := rest[n+1:]
	var seq uint64
	var versioned bool
	switch len(tail) {
	case PackedCoordKeyLen:
	case PackedCoordKeyLen + VersionSuffixLen:
		seq = binary.BigEndian.Uint64(tail[PackedCoordKeyLen:])
		versioned = true
	default:
		return "", lattice.PackedCoord{}, 0, false, fmt.Errorf("index: %s key packed len", family)
	}
	coord := lattice.PackedCoord{binary.BigEndian.Uint64(tail[8:]), binary.BigEndian.Uint64(tail)}
	return id, coord, seq, versioned, nil
}
