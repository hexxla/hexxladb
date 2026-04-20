package index

import "encoding/binary"

const commitTimePrefix = "__meta/commit-time/"

// CommitTimeKey returns a key sorted by commit time and then commit sequence:
// __meta/commit-time/<int64be unix_nanos>/<uint64be commit_seq>.
func CommitTimeKey(unixNanos int64, commitSeq uint64) []byte {
	out := make([]byte, 0, len(commitTimePrefix)+8+8)
	out = append(out, commitTimePrefix...)
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], uint64(unixNanos)) //nolint:gosec // G115: persisted ordering key.
	out = append(out, t[:]...)
	var s [8]byte
	binary.BigEndian.PutUint64(s[:], commitSeq)
	out = append(out, s[:]...)
	return out
}

// CommitTimeScanBounds returns inclusive range for all commits at or before unixNanos.
func CommitTimeScanBounds(unixNanos int64) (from, to []byte) {
	from = CommitTimeKey(0, 0)
	to = CommitTimeKey(unixNanos, ^uint64(0))
	return from, to
}

// ParseCommitTimeKey extracts unix_nanos and commit_seq from [CommitTimeKey].
func ParseCommitTimeKey(key []byte) (unixNanos int64, commitSeq uint64, ok bool) {
	if len(key) != len(commitTimePrefix)+16 {
		return 0, 0, false
	}
	if string(key[:len(commitTimePrefix)]) != commitTimePrefix {
		return 0, 0, false
	}
	unixNanos = int64(binary.BigEndian.Uint64(key[len(commitTimePrefix) : len(commitTimePrefix)+8])) //nolint:gosec // G115
	commitSeq = binary.BigEndian.Uint64(key[len(commitTimePrefix)+8:])
	return unixNanos, commitSeq, true
}
