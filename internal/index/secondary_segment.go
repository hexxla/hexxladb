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
