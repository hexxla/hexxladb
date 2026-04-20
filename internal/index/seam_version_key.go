package index

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/oklog/ulid/v2"
)

const seamVersionSep = '/'

// SeamKeyWithVersion returns seam/<ulid>/<commit_seq_be>.
func SeamKeyWithVersion(ulidStr string, commitSeq uint64) ([]byte, error) {
	if _, err := ulid.Parse(ulidStr); err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidSeamULID, err)
	}
	out := make([]byte, 0, len(SeamPrefix)+ULIDStringLen+1+VersionSuffixLen)
	out = append(out, SeamPrefix...)
	out = append(out, ulidStr...)
	out = append(out, seamVersionSep)
	var b [VersionSuffixLen]byte
	binary.BigEndian.PutUint64(b[:], commitSeq)
	out = append(out, b[:]...)
	return out, nil
}

// SeamVersionScanBounds returns [from, to] inclusive over all versions for one ULID.
func SeamVersionScanBounds(ulidStr string) (from, to []byte, err error) {
	from, err = SeamKeyWithVersion(ulidStr, 0)
	if err != nil {
		return nil, nil, err
	}
	to, err = SeamKeyWithVersion(ulidStr, math.MaxUint64)
	if err != nil {
		return nil, nil, err
	}
	return from, to, nil
}

// ParseSeamCommitSeq extracts commit sequence from seam/<ulid>/<seq> keys.
func ParseSeamCommitSeq(key []byte) (uint64, bool) {
	want := len(SeamPrefix) + ULIDStringLen + 1 + VersionSuffixLen
	if len(key) != want {
		return 0, false
	}
	if key[len(SeamPrefix)+ULIDStringLen] != seamVersionSep {
		return 0, false
	}
	return binary.BigEndian.Uint64(key[len(key)-VersionSuffixLen:]), true
}

// ParseSeamVersionKey parses seam/<ulid>/<seq>.
func ParseSeamVersionKey(key []byte) (ulidStr string, commitSeq uint64, err error) {
	if len(key) != len(SeamPrefix)+ULIDStringLen+1+VersionSuffixLen {
		return "", 0, errors.New("index: seam version key length")
	}
	if string(key[:len(SeamPrefix)]) != SeamPrefix {
		return "", 0, errors.New("index: seam version prefix")
	}
	ulidStr = string(key[len(SeamPrefix) : len(SeamPrefix)+ULIDStringLen])
	if _, parseErr := ulid.Parse(ulidStr); parseErr != nil {
		return "", 0, parseErr
	}
	if key[len(SeamPrefix)+ULIDStringLen] != seamVersionSep {
		return "", 0, errors.New("index: seam version separator")
	}
	commitSeq = binary.BigEndian.Uint64(key[len(key)-VersionSuffixLen:])
	return ulidStr, commitSeq, nil
}
