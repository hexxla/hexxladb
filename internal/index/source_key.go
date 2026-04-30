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
	buf := make([]byte, 0, len(SourcePrefix)+2+len(sourceID)+1+PackedCoordKeyLen)
	buf = append(buf, SourcePrefix...)
	var err error
	buf, err = appendLenPrefixedUTF8(buf, sourceID, MaxSourceIDBytes, ErrSourceIDTooLong)
	if err != nil {
		return nil, err
	}
	buf = append(buf, '/')
	buf = appendPackedCoordBE(buf, p)
	return buf, nil
}

// SourceRangePrefix returns [from, to] inclusive byte bounds for AscendRange over all cells
// with the given source_id (any PackedCoord).
func SourceRangePrefix(sourceID string) (from, to []byte, err error) {
	head, hErr := lenPrefixedIDPrefixAfterASCII(SourcePrefix, []byte(sourceID), MaxSourceIDBytes, ErrSourceIDTooLong)
	if hErr != nil {
		return nil, nil, hErr
	}
	// Lower bound: zero packed coord
	from = make([]byte, 0, len(head)+PackedCoordKeyLen)
	from = append(from, head...)
	var z lattice.PackedCoord
	from = appendPackedCoordBE(from, z)
	// Upper bound: max packed coord (total order upper sentinel)
	to = make([]byte, 0, len(head)+PackedCoordKeyLen)
	to = append(to, head...)
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

// ParseSourceKeyWithSeq parses k like [ParseSourceKey] and, when k includes an MVCC version suffix
// ([SourceKeyWithVersion]), sets hasSeq=true and fills commitSeq from that suffix.
func ParseSourceKeyWithSeq(k []byte) (sourceID string, p lattice.PackedCoord, commitSeq uint64, hasSeq bool, err error) {
	if !bytes.HasPrefix(k, []byte(SourcePrefix)) {
		return "", lattice.PackedCoord{}, 0, false, errors.New("index: not a source key")
	}
	rest := k[len(SourcePrefix):]
	if len(rest) < 2 {
		return "", lattice.PackedCoord{}, 0, false, errors.New("index: source key truncated")
	}
	n := int(binary.BigEndian.Uint16(rest[0:2]))
	rest = rest[2:]
	if n > MaxSourceIDBytes || len(rest) < n+1+PackedCoordKeyLen {
		return "", lattice.PackedCoord{}, 0, false, errors.New("index: bad source key layout")
	}
	id := string(rest[:n])
	rest = rest[n:]
	if len(rest) < 1 || rest[0] != '/' {
		return "", lattice.PackedCoord{}, 0, false, errors.New("index: source key separator")
	}
	rest = rest[1:]
	var pc lattice.PackedCoord
	switch len(rest) {
	case PackedCoordKeyLen:
	case PackedCoordKeyLen + VersionSuffixLen:
		hasSeq = true
		commitSeq = binary.BigEndian.Uint64(rest[PackedCoordKeyLen:])
		rest = rest[:PackedCoordKeyLen]
	default:
		return "", lattice.PackedCoord{}, 0, false, errors.New("index: source key packed len")
	}
	pc[1] = binary.BigEndian.Uint64(rest[0:8])
	pc[0] = binary.BigEndian.Uint64(rest[8:16])
	return id, pc, commitSeq, hasSeq, nil
}

// ErrSourceIDTooLong means SourceID exceeds [MaxSourceIDBytes].
var ErrSourceIDTooLong = errors.New("index: source_id too long for secondary key")
