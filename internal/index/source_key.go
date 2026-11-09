package index

import (
	"errors"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// SourcePrefix is the ASCII prefix for source/<source_id>/... keys per HEXXLA_DB.md.
const SourcePrefix = "source/"

// MaxSourceIDBytes is the maximum UTF-8 byte length for [record.ProvenanceWire.SourceID]
// in secondary index keys (keeps full keys within engine limits).
const MaxSourceIDBytes = 220

// SourceKey returns source/<u16be len><id bytes>/<packed_coord> for lexicographic scans by source.
func SourceKey(id string, p lattice.PackedCoord) ([]byte, error) {
	return secondaryCoordKey(SourcePrefix, id, MaxSourceIDBytes, ErrSourceIDTooLong, p, nil)
}

// SourceKeyWithVersion adds a commit sequence to a source ID index key.
func SourceKeyWithVersion(id string, p lattice.PackedCoord, seq uint64) ([]byte, error) {
	return secondaryCoordKey(SourcePrefix, id, MaxSourceIDBytes, ErrSourceIDTooLong, p, &seq)
}

// SourceRangePrefix returns inclusive coordinate bounds for a source ID.
func SourceRangePrefix(id string) (from, to []byte, err error) {
	return secondaryCoordBounds(SourcePrefix, id, MaxSourceIDBytes, ErrSourceIDTooLong, false)
}

// SourceRangePrefixAllVersions includes all commit sequences.
func SourceRangePrefixAllVersions(id string) (from, to []byte, err error) {
	return secondaryCoordBounds(SourcePrefix, id, MaxSourceIDBytes, ErrSourceIDTooLong, true)
}

// ParseSourceKey extracts the indexed value and coordinate.
func ParseSourceKey(key []byte) (string, lattice.PackedCoord, error) {
	id, coord, _, _, err := ParseSourceKeyWithSeq(key)
	return id, coord, err
}

// ParseSourceKeyWithSeq also returns the optional commit sequence.
func ParseSourceKeyWithSeq(key []byte) (string, lattice.PackedCoord, uint64, bool, error) {
	return parseSecondaryCoordKey(key, SourcePrefix, MaxSourceIDBytes)
}

// ErrSourceIDTooLong means SourceID exceeds [MaxSourceIDBytes].
var ErrSourceIDTooLong = errors.New("index: source_id too long for secondary key")
