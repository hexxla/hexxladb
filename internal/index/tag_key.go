package index

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// TagPrefix is the ASCII prefix for tag/<tag_text>/<packed_coord> secondary keys per HEXXLA_DB.md.
const TagPrefix = "tag/"

// MaxTagBytes is the maximum UTF-8 byte length for a single tag in secondary keys.
const MaxTagBytes = 220

// TagKey returns tag/<u16be len><utf8 tag>/<packed_coord>.
func TagKey(tag string, p lattice.PackedCoord) ([]byte, error) {
	buf := make([]byte, 0, len(TagPrefix)+2+len(tag)+1+PackedCoordKeyLen)
	buf = append(buf, TagPrefix...)
	var err error
	buf, err = appendLenPrefixedUTF8(buf, tag, MaxTagBytes, ErrTagTooLong)
	if err != nil {
		return nil, err
	}
	buf = append(buf, '/')
	return appendPackedCoordBE(buf, p), nil
}

// TagKeyWithVersion appends MVCC [VersionSuffixLen] commit_seq to [TagKey].
func TagKeyWithVersion(tag string, p lattice.PackedCoord, commitSeq uint64) ([]byte, error) {
	base, err := TagKey(tag, p)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(base)+VersionSuffixLen)
	out = append(out, base...)
	var suf [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(suf[:], commitSeq)
	return append(out, suf[:]...), nil
}

// TagRangePrefix returns inclusive [from, to] for AscendRange over all cells with this tag (any PackedCoord).
func TagRangePrefix(tag string) (from, to []byte, err error) {
	t := []byte(tag)
	if len(t) > MaxTagBytes {
		return nil, nil, ErrTagTooLong
	}
	from = make([]byte, 0, len(TagPrefix)+2+len(t)+1+PackedCoordKeyLen)
	from = append(from, TagPrefix...)
	var lenBE [2]byte
	binary.BigEndian.PutUint16(lenBE[:], uint16(len(t))) //nolint:gosec // len(t) ≤ MaxTagBytes (uint16 safe)
	from = append(from, lenBE[:]...)
	from = append(from, t...)
	from = append(from, '/')
	var z lattice.PackedCoord
	from = appendPackedCoordBE(from, z)
	to = make([]byte, 0, len(TagPrefix)+2+len(t)+1+PackedCoordKeyLen)
	to = append(to, TagPrefix...)
	to = append(to, lenBE[:]...)
	to = append(to, t...)
	to = append(to, '/')
	var maxP lattice.PackedCoord
	maxP[0] = ^uint64(0)
	maxP[1] = ^uint64(0)
	to = appendPackedCoordBE(to, maxP)
	return from, to, nil
}

// TagRangePrefixAllVersions widens [TagRangePrefix] for MVCC suffix scans.
func TagRangePrefixAllVersions(tag string) (from, to []byte, err error) {
	from, to, err = TagRangePrefix(tag)
	if err != nil {
		return nil, nil, err
	}
	from = append(append([]byte(nil), from...), make([]byte, VersionSuffixLen)...)
	to = append(append([]byte(nil), to...), bytes.Repeat([]byte{0xff}, VersionSuffixLen)...)
	return from, to, nil
}

// ParseTagKey extracts tag and packed coord from [TagKey] or [TagKeyWithVersion].
func ParseTagKey(key []byte) (tag string, p lattice.PackedCoord, err error) {
	if !bytes.HasPrefix(key, []byte(TagPrefix)) {
		return "", lattice.PackedCoord{}, errors.New("index: not a tag key")
	}
	rest := key[len(TagPrefix):]
	if len(rest) < 2 {
		return "", lattice.PackedCoord{}, errors.New("index: tag key truncated")
	}
	n := int(binary.BigEndian.Uint16(rest[0:2]))
	rest = rest[2:]
	if n > MaxTagBytes || len(rest) < n+1+PackedCoordKeyLen {
		return "", lattice.PackedCoord{}, errors.New("index: bad tag key layout")
	}
	tag = string(rest[:n])
	rest = rest[n:]
	if rest[0] != '/' {
		return "", lattice.PackedCoord{}, errors.New("index: tag key separator")
	}
	rest = rest[1:]
	switch len(rest) {
	case PackedCoordKeyLen:
	case PackedCoordKeyLen + VersionSuffixLen:
		rest = rest[:PackedCoordKeyLen]
	default:
		return "", lattice.PackedCoord{}, errors.New("index: tag key packed len")
	}
	var pc lattice.PackedCoord
	pc[1] = binary.BigEndian.Uint64(rest[0:8])
	pc[0] = binary.BigEndian.Uint64(rest[8:16])
	return tag, pc, nil
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
