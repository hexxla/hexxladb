package index

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// SourcePrefix is the ASCII prefix for source/<source_id>/... keys per HEXXLA_DB.md.
const SourcePrefix = "source/"

// MaxSourceIDBytes is the maximum UTF-8 byte length for [record.ProvenanceWire.SourceID]
// in secondary index keys (keeps full keys within engine limits).
const MaxSourceIDBytes = 220

// SourceKey returns source/<u16be len><id bytes>/<packed_coord> for lexicographic scans by source.
func SourceKey(sourceID string, p lattice.PackedCoord) ([]byte, error) {
	id := []byte(sourceID)
	if len(id) > MaxSourceIDBytes {
		return nil, ErrSourceIDTooLong
	}
	if len(id) > 0xffff {
		return nil, ErrSourceIDTooLong
	}
	buf := make([]byte, 0, len(SourcePrefix)+2+len(id)+1+PackedCoordKeyLen)
	buf = append(buf, SourcePrefix...)
	var lenBE [2]byte
	binary.BigEndian.PutUint16(lenBE[:], uint16(len(id))) //nolint:gosec // G115 — len(id) ≤ MaxSourceIDBytes.
	buf = append(buf, lenBE[:]...)
	buf = append(buf, id...)
	buf = append(buf, '/')
	buf = appendPackedCoordBE(buf, p)
	return buf, nil
}

// SourceRangePrefix returns [from, to] inclusive byte bounds for AscendRange over all cells
// with the given source_id (any PackedCoord).
func SourceRangePrefix(sourceID string) (from, to []byte, err error) {
	id := []byte(sourceID)
	if len(id) > MaxSourceIDBytes {
		return nil, nil, ErrSourceIDTooLong
	}
	from = make([]byte, 0, len(SourcePrefix)+2+len(id)+1+PackedCoordKeyLen)
	from = append(from, SourcePrefix...)
	var lenBE [2]byte
	binary.BigEndian.PutUint16(lenBE[:], uint16(len(id))) //nolint:gosec // G115 — len(id) ≤ MaxSourceIDBytes.
	from = append(from, lenBE[:]...)
	from = append(from, id...)
	from = append(from, '/')
	// Lower bound: zero packed coord
	var z lattice.PackedCoord
	from = appendPackedCoordBE(from, z)
	// Upper bound: max packed coord (total order upper sentinel)
	to = make([]byte, 0, len(SourcePrefix)+2+len(id)+1+PackedCoordKeyLen)
	to = append(to, SourcePrefix...)
	to = append(to, lenBE[:]...)
	to = append(to, id...)
	to = append(to, '/')
	var maxP lattice.PackedCoord
	maxP[0] = ^uint64(0)
	maxP[1] = ^uint64(0)
	to = appendPackedCoordBE(to, maxP)
	return from, to, nil
}

// SourceKeyWithVersion appends MVCC [VersionSuffixLen] commit_seq (big-endian) to [SourceKey].
func SourceKeyWithVersion(sourceID string, p lattice.PackedCoord, commitSeq uint64) ([]byte, error) {
	base, err := SourceKey(sourceID, p)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(base)+VersionSuffixLen)
	out = append(out, base...)
	var suf [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(suf[:], commitSeq)
	return append(out, suf[:]...), nil
}

// SourceRangePrefixAllVersions widens [SourceRangePrefix] so AscendRange includes every MVCC suffix
// for that source_id.
func SourceRangePrefixAllVersions(sourceID string) (from, to []byte, err error) {
	from, to, err = SourceRangePrefix(sourceID)
	if err != nil {
		return nil, nil, err
	}
	from = append(append([]byte(nil), from...), make([]byte, VersionSuffixLen)...)
	to = append(append([]byte(nil), to...), bytes.Repeat([]byte{0xff}, VersionSuffixLen)...)
	return from, to, nil
}

// ParseSourceKey extracts source id and packed coord from a key built by [SourceKey] or [SourceKeyWithVersion].
func ParseSourceKey(key []byte) (sourceID string, p lattice.PackedCoord, err error) {
	if !bytes.HasPrefix(key, []byte(SourcePrefix)) {
		return "", lattice.PackedCoord{}, errors.New("index: not a source key")
	}
	rest := key[len(SourcePrefix):]
	if len(rest) < 2 {
		return "", lattice.PackedCoord{}, errors.New("index: source key truncated")
	}
	n := int(binary.BigEndian.Uint16(rest[0:2]))
	rest = rest[2:]
	if n > MaxSourceIDBytes || len(rest) < n+1+PackedCoordKeyLen {
		return "", lattice.PackedCoord{}, errors.New("index: bad source key layout")
	}
	id := string(rest[:n])
	rest = rest[n:]
	if rest[0] != '/' {
		return "", lattice.PackedCoord{}, errors.New("index: source key separator")
	}
	rest = rest[1:]
	switch len(rest) {
	case PackedCoordKeyLen:
	case PackedCoordKeyLen + VersionSuffixLen:
		rest = rest[:PackedCoordKeyLen]
	default:
		return "", lattice.PackedCoord{}, errors.New("index: source key packed len")
	}
	var pc lattice.PackedCoord
	pc[1] = binary.BigEndian.Uint64(rest[0:8])
	pc[0] = binary.BigEndian.Uint64(rest[8:16])
	return id, pc, nil
}

// ErrSourceIDTooLong means SourceID exceeds [MaxSourceIDBytes].
var ErrSourceIDTooLong = errors.New("index: source_id too long for secondary key")
