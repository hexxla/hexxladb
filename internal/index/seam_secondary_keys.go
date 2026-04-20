package index

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/oklog/ulid/v2"
)

// SeamTimePrefix is the ASCII prefix for seam-time/<bucket>/<ulid> keys (seam temporal secondary).
const SeamTimePrefix = "seam-time/"

// SeamSourcePrefix is the ASCII prefix for seam-source/<u16be len><source_id>/<ulid> keys.
const SeamSourcePrefix = "seam-source/"

// ULIDStringLen is the Crockford length of a canonical ULID string.
const ULIDStringLen = 26

// SeamTimeKey returns seam-time/<int64be bucket>/<ulid> for lexicographic scans by validity week bucket.
func SeamTimeKey(bucket int64, ulidStr string) ([]byte, error) {
	if len(ulidStr) != ULIDStringLen {
		return nil, errInvalidSeamULID
	}
	if _, err := ulid.Parse(ulidStr); err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidSeamULID, err)
	}
	buf := make([]byte, 0, len(SeamTimePrefix)+8+1+ULIDStringLen)
	buf = append(buf, SeamTimePrefix...)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(bucket)) //nolint:gosec // G115 — bucket stored as unsigned key segment.
	buf = append(buf, b[:]...)
	buf = append(buf, '/')
	buf = append(buf, ulidStr...)
	return buf, nil
}

// SeamTimeKeyWithVersion returns seam-time/<bucket>/<ulid>/<commit_seq_be>.
func SeamTimeKeyWithVersion(bucket int64, ulidStr string, commitSeq uint64) ([]byte, error) {
	base, err := SeamTimeKey(bucket, ulidStr)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(base)+1+VersionSuffixLen)
	out = append(out, base...)
	out = append(out, '/')
	var b [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(b[:], commitSeq)
	out = append(out, b[:]...)
	return out, nil
}

// SeamTimeRangePrefix returns inclusive [from, to] byte bounds for AscendRange over one week bucket (all seams).
func SeamTimeRangePrefix(bucket int64) (from, to []byte) {
	from = make([]byte, 0, len(SeamTimePrefix)+8+1+ULIDStringLen)
	from = append(from, SeamTimePrefix...)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(bucket)) //nolint:gosec // G115
	from = append(from, b[:]...)
	from = append(from, '/')
	from = append(from, bytes.Repeat([]byte{'0'}, ULIDStringLen)...)
	to = make([]byte, 0, len(SeamTimePrefix)+8+1+ULIDStringLen)
	to = append(to, SeamTimePrefix...)
	to = append(to, b[:]...)
	to = append(to, '/')
	to = append(to, bytes.Repeat([]byte{'Z'}, ULIDStringLen)...)
	return from, to
}

// SeamTimeRangePrefixAllVersions returns inclusive bounds for seam-time/<bucket>/<ulid>/<seq>.
func SeamTimeRangePrefixAllVersions(bucket int64) (from, to []byte) {
	from = make([]byte, 0, len(SeamTimePrefix)+8+1+ULIDStringLen+1+VersionSuffixLen)
	from = append(from, SeamTimePrefix...)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(bucket)) //nolint:gosec // G115
	from = append(from, b[:]...)
	from = append(from, '/')
	from = append(from, bytes.Repeat([]byte{'0'}, ULIDStringLen)...)
	from = append(from, '/')
	var v0 [VersionSuffixLen]byte
	from = append(from, v0[:]...)

	to = make([]byte, 0, len(from))
	to = append(to, SeamTimePrefix...)
	to = append(to, b[:]...)
	to = append(to, '/')
	to = append(to, bytes.Repeat([]byte{'Z'}, ULIDStringLen)...)
	to = append(to, '/')
	var vmax [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(vmax[:], math.MaxUint64)
	to = append(to, vmax[:]...)
	return from, to
}

// ParseSeamTimeKey extracts bucket and ULID string from a key built by [SeamTimeKey].
func ParseSeamTimeKey(key []byte) (bucket int64, ulidStr string, err error) {
	if !bytes.HasPrefix(key, []byte(SeamTimePrefix)) {
		return 0, "", errors.New("index: not a seam-time key")
	}
	rest := key[len(SeamTimePrefix):]
	if len(rest) < 8+1+ULIDStringLen {
		return 0, "", errors.New("index: seam-time key truncated")
	}
	bucket = int64(binary.BigEndian.Uint64(rest[0:8])) //nolint:gosec // G115
	if rest[8] != '/' {
		return 0, "", errors.New("index: seam-time separator")
	}
	rest = rest[9:]
	if len(rest) != ULIDStringLen && len(rest) != ULIDStringLen+1+VersionSuffixLen {
		return 0, "", errors.New("index: seam-time ulid len")
	}
	if len(rest) == ULIDStringLen+1+VersionSuffixLen {
		if rest[ULIDStringLen] != '/' {
			return 0, "", errors.New("index: seam-time version separator")
		}
		rest = rest[:ULIDStringLen]
	}
	ulidStr = string(rest)
	if _, err := ulid.Parse(ulidStr); err != nil {
		return 0, "", err
	}
	return bucket, ulidStr, nil
}

// ParseSeamTimeCommitSeq extracts commit_seq from seam-time versioned keys.
func ParseSeamTimeCommitSeq(key []byte) (uint64, bool) {
	if len(key) != len(SeamTimePrefix)+8+1+ULIDStringLen+1+VersionSuffixLen {
		return 0, false
	}
	if key[len(key)-VersionSuffixLen-1] != '/' {
		return 0, false
	}
	return binary.BigEndian.Uint64(key[len(key)-VersionSuffixLen:]), true
}

// SeamSourceKey returns seam-source/<u16be len><id bytes>/<ulid> for lexicographic scans by source.
func SeamSourceKey(sourceID, ulidStr string) ([]byte, error) {
	if len(ulidStr) != ULIDStringLen {
		return nil, errInvalidSeamULID
	}
	if _, err := ulid.Parse(ulidStr); err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidSeamULID, err)
	}
	id := []byte(sourceID)
	if len(id) > MaxSourceIDBytes {
		return nil, ErrSourceIDTooLong
	}
	if len(id) > 0xffff {
		return nil, ErrSourceIDTooLong
	}
	buf := make([]byte, 0, len(SeamSourcePrefix)+2+len(id)+1+ULIDStringLen)
	buf = append(buf, SeamSourcePrefix...)
	var lenBE [2]byte
	binary.BigEndian.PutUint16(lenBE[:], uint16(len(id))) //nolint:gosec // G115
	buf = append(buf, lenBE[:]...)
	buf = append(buf, id...)
	buf = append(buf, '/')
	buf = append(buf, ulidStr...)
	return buf, nil
}

// SeamSourceKeyWithVersion returns seam-source/<len><source>/<ulid>/<commit_seq_be>.
func SeamSourceKeyWithVersion(sourceID, ulidStr string, commitSeq uint64) ([]byte, error) {
	base, err := SeamSourceKey(sourceID, ulidStr)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(base)+1+VersionSuffixLen)
	out = append(out, base...)
	out = append(out, '/')
	var b [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(b[:], commitSeq)
	out = append(out, b[:]...)
	return out, nil
}

// SeamSourceRangePrefix returns [from, to] inclusive byte bounds for AscendRange over all seams with sourceID.
func SeamSourceRangePrefix(sourceID string) (from, to []byte, err error) {
	id := []byte(sourceID)
	if len(id) > MaxSourceIDBytes {
		return nil, nil, ErrSourceIDTooLong
	}
	from = make([]byte, 0, len(SeamSourcePrefix)+2+len(id)+1+ULIDStringLen)
	from = append(from, SeamSourcePrefix...)
	var lenBE [2]byte
	binary.BigEndian.PutUint16(lenBE[:], uint16(len(id))) //nolint:gosec // G115
	from = append(from, lenBE[:]...)
	from = append(from, id...)
	from = append(from, '/')
	from = append(from, bytes.Repeat([]byte{'0'}, ULIDStringLen)...)
	to = make([]byte, 0, len(SeamSourcePrefix)+2+len(id)+1+ULIDStringLen)
	to = append(to, SeamSourcePrefix...)
	to = append(to, lenBE[:]...)
	to = append(to, id...)
	to = append(to, '/')
	to = append(to, bytes.Repeat([]byte{'Z'}, ULIDStringLen)...)
	return from, to, nil
}

// SeamSourceRangePrefixAllVersions returns inclusive bounds for all seam-source versions.
func SeamSourceRangePrefixAllVersions(sourceID string) (from, to []byte, err error) {
	id := []byte(sourceID)
	if len(id) > MaxSourceIDBytes {
		return nil, nil, ErrSourceIDTooLong
	}
	from = make([]byte, 0, len(SeamSourcePrefix)+2+len(id)+1+ULIDStringLen+1+VersionSuffixLen)
	from = append(from, SeamSourcePrefix...)
	var lenBE [2]byte
	binary.BigEndian.PutUint16(lenBE[:], uint16(len(id))) //nolint:gosec // G115
	from = append(from, lenBE[:]...)
	from = append(from, id...)
	from = append(from, '/')
	from = append(from, bytes.Repeat([]byte{'0'}, ULIDStringLen)...)
	from = append(from, '/')
	var v0 [VersionSuffixLen]byte
	from = append(from, v0[:]...)

	to = make([]byte, 0, len(from))
	to = append(to, SeamSourcePrefix...)
	to = append(to, lenBE[:]...)
	to = append(to, id...)
	to = append(to, '/')
	to = append(to, bytes.Repeat([]byte{'Z'}, ULIDStringLen)...)
	to = append(to, '/')
	var vmax [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(vmax[:], math.MaxUint64)
	to = append(to, vmax[:]...)
	return from, to, nil
}

// ParseSeamSourceKey extracts source id and ULID from a key built by [SeamSourceKey].
func ParseSeamSourceKey(key []byte) (sourceID, ulidStr string, err error) {
	if !bytes.HasPrefix(key, []byte(SeamSourcePrefix)) {
		return "", "", errors.New("index: not a seam-source key")
	}
	rest := key[len(SeamSourcePrefix):]
	if len(rest) < 2 {
		return "", "", errors.New("index: seam-source key truncated")
	}
	n := int(binary.BigEndian.Uint16(rest[0:2]))
	rest = rest[2:]
	if n > MaxSourceIDBytes || len(rest) < n+1+ULIDStringLen {
		return "", "", errors.New("index: bad seam-source layout")
	}
	sourceID = string(rest[:n])
	rest = rest[n:]
	if rest[0] != '/' {
		return "", "", errors.New("index: seam-source separator")
	}
	rest = rest[1:]
	if len(rest) != ULIDStringLen && len(rest) != ULIDStringLen+1+VersionSuffixLen {
		return "", "", errors.New("index: seam-source ulid len")
	}
	if len(rest) == ULIDStringLen+1+VersionSuffixLen {
		if rest[ULIDStringLen] != '/' {
			return "", "", errors.New("index: seam-source version separator")
		}
		rest = rest[:ULIDStringLen]
	}
	ulidStr = string(rest)
	if _, err := ulid.Parse(ulidStr); err != nil {
		return "", "", err
	}
	return sourceID, ulidStr, nil
}

// ParseSeamSourceCommitSeq extracts commit_seq from seam-source versioned keys.
func ParseSeamSourceCommitSeq(key []byte) (uint64, bool) {
	if len(key) < VersionSuffixLen+1 {
		return 0, false
	}
	if key[len(key)-VersionSuffixLen-1] != '/' {
		return 0, false
	}
	return binary.BigEndian.Uint64(key[len(key)-VersionSuffixLen:]), true
}

var errInvalidSeamULID = errors.New("index: invalid seam ulid for secondary key")
