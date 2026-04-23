package index

import (
	"encoding/binary"
)

// appendLenPrefixedUTF8 appends uint16-big-endian length plus UTF-8 segment bytes for s.
// Secondary keys such as source/ and tag/ share this framing; see docs/hexxladb/HEXXLA_DB.md.
//
// Returns tooLong when len(s) in UTF-8 exceeds maxBytes or cannot be represented in uint16.
func appendLenPrefixedUTF8(dst []byte, s string, maxBytes int, tooLong error) ([]byte, error) {
	id := []byte(s)
	if len(id) > maxBytes {
		return dst, tooLong
	}
	if len(id) > 0xffff {
		return dst, tooLong
	}
	var lenBE [2]byte
	binary.BigEndian.PutUint16(lenBE[:], uint16(len(id))) //nolint:gosec // bounded by checks above.
	dst = append(dst, lenBE[:]...)
	dst = append(dst, id...)
	return dst, nil
}

// lenPrefixedIDPrefixAfterASCII returns asciiPrefix + u16be(len(id)) + id + '/'.
// It is the shared head of [SourceRangePrefix] and [TagRangePrefix] before min/max [lattice.PackedCoord] sentinels.
func lenPrefixedIDPrefixAfterASCII(asciiPrefix string, id []byte, maxBytes int, errLong error) ([]byte, error) {
	if len(id) > maxBytes {
		return nil, errLong
	}
	if len(id) > 0xffff {
		return nil, errLong
	}
	b := make([]byte, 0, len(asciiPrefix)+2+len(id)+1)
	b = append(b, asciiPrefix...)
	var lenBE [2]byte
	binary.BigEndian.PutUint16(lenBE[:], uint16(len(id))) //nolint:gosec // len(id) bounded above
	b = append(b, lenBE[:]...)
	b = append(b, id...)
	b = append(b, '/')
	return b, nil
}
