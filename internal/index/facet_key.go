package index

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

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

// FacetKeyWithVersion appends MVCC [VersionSuffixLen] commit_seq to [FacetKey].
func FacetKeyWithVersion(p lattice.PackedCoord, facetID byte, commitSeq uint64) ([]byte, error) {
	base, err := FacetKey(p, facetID)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(base)+VersionSuffixLen)
	out = append(out, base...)
	var suf [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(suf[:], commitSeq)
	return append(out, suf[:]...), nil
}

// FacetVersionScanBounds returns [from, to] inclusive for AscendRange over all MVCC rows for (p, facetID).
func FacetVersionScanBounds(p lattice.PackedCoord, facetID byte) (from, to []byte, err error) {
	from, err = FacetKeyWithVersion(p, facetID, 0)
	if err != nil {
		return nil, nil, err
	}
	to, err = FacetKeyWithVersion(p, facetID, math.MaxUint64)
	if err != nil {
		return nil, nil, err
	}
	return from, to, nil
}

// FacetCellAllVersionsRange returns [from, to] inclusive across facet_id 0..[MaxFacetID] and all commit_seq.
func FacetCellAllVersionsRange(p lattice.PackedCoord) (from, to []byte, err error) {
	from, err = FacetKeyWithVersion(p, 0, 0)
	if err != nil {
		return nil, nil, err
	}
	to, err = FacetKeyWithVersion(p, MaxFacetID, math.MaxUint64)
	if err != nil {
		return nil, nil, err
	}
	return from, to, nil
}

// ParseFacetKey parses a key built by [FacetKey] or [FacetKeyWithVersion].
func ParseFacetKey(key []byte) (p lattice.PackedCoord, facetID byte, err error) {
	if !bytes.HasPrefix(key, []byte(FacetPrefix)) {
		return lattice.PackedCoord{}, 0, fmt.Errorf("index: not a facet key")
	}
	rest := key[len(FacetPrefix):]
	if len(rest) != PackedCoordKeyLen+1+1 && len(rest) != PackedCoordKeyLen+1+1+VersionSuffixLen {
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

// ParseFacetCommitSeq returns the MVCC suffix commit_seq for keys from [FacetKeyWithVersion], or 0 for [FacetKey].
func ParseFacetCommitSeq(key []byte) (commitSeq uint64, ok bool) {
	_, _, err := ParseFacetKey(key)
	if err != nil {
		return 0, false
	}
	switch len(key) - len(FacetPrefix) {
	case PackedCoordKeyLen + 1 + 1:
		return 0, true
	case PackedCoordKeyLen + 1 + 1 + VersionSuffixLen:
		return binary.BigEndian.Uint64(key[len(key)-VersionSuffixLen:]), true
	default:
		return 0, false
	}
}

// ErrFacetIDOutOfRange means facet_id is not in 0..5.
var ErrFacetIDOutOfRange = errors.New("index: facet id out of range (0..5)")
