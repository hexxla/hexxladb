package index

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// FacetPrefix is the ASCII prefix for facet keys per HEXXLA_DB.md: facet/<packed>/<facet_id>.
const FacetPrefix = "facet/"

// MaxFacetID is the inclusive upper bound for facet_id (0..5).
const MaxFacetID = 5

// FacetKey returns facet/<packed>/<facet_id> with the same PackedCoord encoding as [CellKey].
func FacetKey(p lattice.PackedCoord, facetID byte) ([]byte, error) {
	if facetID > MaxFacetID {
		return nil, fmt.Errorf("%w: got %d", ErrFacetIDOutOfRange, facetID)
	}
	buf := make([]byte, 0, len(FacetPrefix)+PackedCoordKeyLen+1+1)
	buf = append(buf, FacetPrefix...)
	buf = appendPackedCoordBE(buf, p)
	buf = append(buf, '/', facetID)
	return buf, nil
}

// FacetRangeLower returns the smallest facet key for cell p (facet_id 0).
func FacetRangeLower(p lattice.PackedCoord) ([]byte, error) {
	return FacetKey(p, 0)
}

// FacetRangeUpper returns the largest facet key for cell p (facet_id 5) for inclusive AscendRange with lower.
func FacetRangeUpper(p lattice.PackedCoord) ([]byte, error) {
	return FacetKey(p, MaxFacetID)
}

// ParseFacetKey parses a key built by [FacetKey].
func ParseFacetKey(key []byte) (p lattice.PackedCoord, facetID byte, err error) {
	if !bytes.HasPrefix(key, []byte(FacetPrefix)) {
		return lattice.PackedCoord{}, 0, fmt.Errorf("index: not a facet key")
	}
	rest := key[len(FacetPrefix):]
	if len(rest) != PackedCoordKeyLen+1+1 {
		return lattice.PackedCoord{}, 0, fmt.Errorf("index: bad facet key length")
	}
	suf := rest[:PackedCoordKeyLen]
	var pc lattice.PackedCoord
	pc[1] = binary.BigEndian.Uint64(suf[0:8])
	pc[0] = binary.BigEndian.Uint64(suf[8:16])
	if rest[PackedCoordKeyLen] != '/' {
		return lattice.PackedCoord{}, 0, fmt.Errorf("index: bad facet separator")
	}
	facetID = rest[PackedCoordKeyLen+1]
	if facetID > MaxFacetID {
		return lattice.PackedCoord{}, 0, fmt.Errorf("%w: got %d", ErrFacetIDOutOfRange, facetID)
	}
	return pc, facetID, nil
}

// ErrFacetIDOutOfRange means facet_id is not in 0..5.
var ErrFacetIDOutOfRange = errors.New("index: facet id out of range (0..5)")
