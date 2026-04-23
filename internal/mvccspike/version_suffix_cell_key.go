// Package mvccspike holds Phase E1 MVCC storage experiments. Production MVCC reuses the same
// Option A key layout and [SelectVisible] via [github.com/hexxla/hexxladb/internal/index] and package hexxladb.
//
// This file implements the “version suffix on logical keys” layout for MVCC format v2: each
// physical btree key is [index.CellKey] for the logical cell plus an 8-byte big-endian commit_seq.
// Visibility picks the largest commit_seq ≤ read_seq (see [SelectVisible]).
package mvccspike

import (
	"encoding/binary"
	"math"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// VersionSuffixLen is the length of the commit_seq suffix in bytes.
const VersionSuffixLen = 8

// CellPhysicalKeyWithVersionSuffix returns cell/<packed> || commit_seq (big-endian uint64).
// Multiple commits for the same logical cell share the same prefix and differ only in the suffix.
func CellPhysicalKeyWithVersionSuffix(p lattice.PackedCoord, commitSeq uint64) []byte {
	base := index.CellKey(p)
	out := make([]byte, 0, len(base)+VersionSuffixLen)
	out = append(out, base...)
	var suf [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(suf[:], commitSeq)
	return append(out, suf[:]...)
}

// ParseCommitSeqFromPhysicalKey extracts commit_seq from a key produced by [CellPhysicalKeyWithVersionSuffix].
// ok is false if key is too short or does not start with cell/.
func ParseCommitSeqFromPhysicalKey(key []byte) (commitSeq uint64, ok bool) {
	if len(key) != len(index.CellPrefix)+index.PackedCoordKeyLen+VersionSuffixLen {
		return 0, false
	}
	if _, err := index.ParseCellKey(key[:len(index.CellPrefix)+index.PackedCoordKeyLen]); err != nil {
		return 0, false
	}
	return binary.BigEndian.Uint64(key[len(key)-VersionSuffixLen:]), true
}

// CellVersionSuffixScanBounds returns [from, to] inclusive for [hexxladb.Tx.AscendRange] over all
// physical rows for the logical cell p (every commit_seq from 0 through 2^64-1).
func CellVersionSuffixScanBounds(p lattice.PackedCoord) (from, to []byte) {
	return CellPhysicalKeyWithVersionSuffix(p, 0), CellPhysicalKeyWithVersionSuffix(p, math.MaxUint64)
}

// VersionKV is one stored row for visibility selection.
type VersionKV struct {
	CommitSeq uint64
	Value     []byte
}

// SelectVisible returns the value for max(commit_seq) ≤ readSeq among versions, or ok=false if none.
func SelectVisible(versions []VersionKV, readSeq uint64) (value []byte, commitSeq uint64, ok bool) {
	var best *VersionKV
	for i := range versions {
		v := &versions[i]
		if v.CommitSeq > readSeq {
			continue
		}
		if best == nil || v.CommitSeq > best.CommitSeq {
			best = v
		}
	}
	if best == nil {
		return nil, 0, false
	}
	return best.Value, best.CommitSeq, true
}
