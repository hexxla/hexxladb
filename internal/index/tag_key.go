package index

import (
	"errors"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// TagPrefix is the ASCII prefix for tag/<tag_text>/<packed_coord> secondary keys per HEXXLA_DB.md.
const TagPrefix = "tag/"

// MaxTagBytes is the maximum UTF-8 byte length for a single tag in secondary keys.
const MaxTagBytes = 220

// TagKey returns tag/<u16be len><utf8 tag>/<packed_coord>.
func TagKey(id string, p lattice.PackedCoord) ([]byte, error) {
	return secondaryCoordKey(TagPrefix, id, MaxTagBytes, ErrTagTooLong, p, nil)
}

// TagKeyWithVersion adds a commit sequence to a tag index key.
func TagKeyWithVersion(id string, p lattice.PackedCoord, seq uint64) ([]byte, error) {
	return secondaryCoordKey(TagPrefix, id, MaxTagBytes, ErrTagTooLong, p, &seq)
}

// TagRangePrefix returns inclusive coordinate bounds for a tag.
func TagRangePrefix(id string) (from, to []byte, err error) {
	return secondaryCoordBounds(TagPrefix, id, MaxTagBytes, ErrTagTooLong, false)
}

// TagRangePrefixAllVersions includes all commit sequences.
func TagRangePrefixAllVersions(id string) (from, to []byte, err error) {
	return secondaryCoordBounds(TagPrefix, id, MaxTagBytes, ErrTagTooLong, true)
}

// ParseTagKey extracts the indexed value and coordinate.
func ParseTagKey(key []byte) (string, lattice.PackedCoord, error) {
	id, coord, _, _, err := ParseTagKeyWithSeq(key)
	return id, coord, err
}

// ParseTagKeyWithSeq also returns the optional commit sequence.
func ParseTagKeyWithSeq(key []byte) (string, lattice.PackedCoord, uint64, bool, error) {
	return parseSecondaryCoordKey(key, TagPrefix, MaxTagBytes)
}

// ErrTagTooLong means a tag exceeds [MaxTagBytes].
var ErrTagTooLong = errors.New("index: tag too long for secondary key")

// TagFamilyScanBounds returns inclusive [from, to] for AscendRange over every physical
// tag secondary key (tag/<u16be len><utf8>/<packed_coord>[+MVCC suffix]).
//
// The upper bound is the ASCII string "tau": every valid tag index key begins with [TagPrefix],
// and lexicographically "tag/..." < "tau" because at index 2, 'g' < 'u'.
func TagFamilyScanBounds() (from, to []byte) {
	return []byte(TagPrefix), []byte("tau")
}
