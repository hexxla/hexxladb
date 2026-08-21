package index

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// TimePrefix is the ASCII prefix for time/<valid_bucket>/... keys per HEXXLA_DB.md.
const TimePrefix = "time/"

// WeekNanos is the length of one UTC week in nanoseconds (7 × 24h).
const WeekNanos int64 = 7 * 24 * int64(3600) * 1_000_000_000

// TimeKey returns time/<int64be bucket>/<packed_coord> where bucket is a UTC week index
// derived from ValidFrom (see [WeekBucketFromValidity]).
func TimeKey(bucket int64, p lattice.PackedCoord) []byte {
	buf := make([]byte, 0, len(TimePrefix)+8+1+PackedCoordKeyLen)
	buf = append(buf, TimePrefix...)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(bucket)) //nolint:gosec // G115 — bucket stored as unsigned key segment.
	buf = append(buf, b[:]...)
	buf = append(buf, '/')
	return appendPackedCoordBE(buf, p)
}

// WeekBucketFromValidity returns the UTC week bucket index for v.ValidFrom (nanoseconds since epoch),
// or ok=false when ValidFrom is nil (no time/ index entry).
func WeekBucketFromValidity(v record.ValidityWire) (bucket int64, ok bool) {
	if v.ValidFrom == nil {
		return 0, false
	}
	nanos := *v.ValidFrom
	bucket = nanos / WeekNanos
	if nanos < 0 && nanos%WeekNanos != 0 {
		bucket--
	}
	return bucket, true
}

// TimeRangePrefix returns inclusive [from, to] byte bounds for [BTree.AscendRange] over one week bucket.
func TimeRangePrefix(bucket int64) (from, to []byte) {
	from = make([]byte, 0, len(TimePrefix)+8+1+PackedCoordKeyLen)
	from = append(from, TimePrefix...)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(bucket)) //nolint:gosec // G115 — bucket stored as unsigned key segment.
	from = append(from, b[:]...)
	from = append(from, '/')
	var z lattice.PackedCoord
	from = appendPackedCoordBE(from, z)
	to = make([]byte, 0, len(TimePrefix)+8+1+PackedCoordKeyLen)
	to = append(to, TimePrefix...)
	to = append(to, b[:]...)
	to = append(to, '/')
	var maxP lattice.PackedCoord
	maxP[0] = ^uint64(0)
	maxP[1] = ^uint64(0)
	to = appendPackedCoordBE(to, maxP)
	return from, to
}

// TimeKeyWithVersion appends MVCC [VersionSuffixLen] commit_seq to [TimeKey].
func TimeKeyWithVersion(bucket int64, p lattice.PackedCoord, commitSeq uint64) []byte {
	base := TimeKey(bucket, p)
	out := make([]byte, 0, len(base)+VersionSuffixLen)
	out = append(out, base...)
	var suf [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(suf[:], commitSeq)
	return append(out, suf[:]...)
}

// TimeRangePrefixAllVersions widens [TimeRangePrefix] to include every MVCC suffix in the bucket.
func TimeRangePrefixAllVersions(bucket int64) (from, to []byte) {
	fromB, toB := TimeRangePrefix(bucket)
	from = append(append([]byte(nil), fromB...), make([]byte, VersionSuffixLen)...)
	to = append(append([]byte(nil), toB...), bytes.Repeat([]byte{0xff}, VersionSuffixLen)...)
	return from, to
}

// ParseTimeKey extracts bucket and packed coord from a key built by [TimeKey] or [TimeKeyWithVersion].
func ParseTimeKey(key []byte) (bucket int64, p lattice.PackedCoord, err error) {
	if !bytes.HasPrefix(key, []byte(TimePrefix)) {
		return 0, lattice.PackedCoord{}, errors.New("index: not a time key")
	}
	rest := key[len(TimePrefix):]
	if len(rest) < 8+1+PackedCoordKeyLen {
		return 0, lattice.PackedCoord{}, errors.New("index: time key truncated")
	}
	bucket = int64(binary.BigEndian.Uint64(rest[0:8])) //nolint:gosec // G115 — key written by [TimeKey].
	if rest[8] != '/' {
		return 0, lattice.PackedCoord{}, errors.New("index: time key separator")
	}
	rest = rest[9:]
	switch len(rest) {
	case PackedCoordKeyLen:
	case PackedCoordKeyLen + VersionSuffixLen:
		rest = rest[:PackedCoordKeyLen]
	default:
		return 0, lattice.PackedCoord{}, errors.New("index: time key packed len")
	}
	var pc lattice.PackedCoord
	pc[1] = binary.BigEndian.Uint64(rest[0:8])
	pc[0] = binary.BigEndian.Uint64(rest[8:16])
	return bucket, pc, nil
}
