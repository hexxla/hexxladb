package index

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// VersionSuffixLen is the length of the big-endian commit_seq suffix on MVCC physical keys (MVCC_DESIGN §3 Option A).
const VersionSuffixLen = 8

// CellKeyWithVersion returns cell/<packed> || commit_seq (big-endian uint64).
func CellKeyWithVersion(p lattice.PackedCoord, commitSeq uint64) []byte {
	base := CellKey(p)
	out := make([]byte, 0, len(base)+VersionSuffixLen)
	out = append(out, base...)
	var suf [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(suf[:], commitSeq)
	return append(out, suf[:]...)
}

// ParseCellVersionKey parses a key built by [CellKeyWithVersion].
func ParseCellVersionKey(key []byte) (p lattice.PackedCoord, commitSeq uint64, err error) {
	want := len(CellPrefix) + PackedCoordKeyLen + VersionSuffixLen
	if len(key) != want {
		return lattice.PackedCoord{}, 0, fmt.Errorf("index: bad cell version key length")
	}
	p, err = ParseCellKey(key[:len(CellPrefix)+PackedCoordKeyLen])
	if err != nil {
		return lattice.PackedCoord{}, 0, err
	}
	return p, binary.BigEndian.Uint64(key[len(key)-VersionSuffixLen:]), nil
}

// CellVersionScanBounds returns [from, to] inclusive for AscendRange over all physical rows for logical cell p.
func CellVersionScanBounds(p lattice.PackedCoord) (from, to []byte) {
	return CellKeyWithVersion(p, 0), CellKeyWithVersion(p, math.MaxUint64)
}

// ParseCommitSeqFromCellKey extracts commit_seq from a key from [CellKeyWithVersion].
func ParseCommitSeqFromCellKey(key []byte) (commitSeq uint64, ok bool) {
	want := len(CellPrefix) + PackedCoordKeyLen + VersionSuffixLen
	if len(key) != want {
		return 0, false
	}
	if _, err := ParseCellKey(key[:len(CellPrefix)+PackedCoordKeyLen]); err != nil {
		return 0, false
	}
	return binary.BigEndian.Uint64(key[len(key)-VersionSuffixLen:]), true
}
