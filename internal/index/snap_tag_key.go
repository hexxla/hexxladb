package index

import "encoding/binary"

const (
	// SnapTagPrefix is the B+ tree key prefix for named snapshot tags.
	// Layout: __meta/snap-tag/<label>
	SnapTagPrefix = "__meta/snap-tag/"

	// SnapTagMaxLabelBytes is the maximum byte length of a snapshot tag label.
	SnapTagMaxLabelBytes = 200

	// snapTagValueLen is the fixed value length: uint64 big-endian commit_seq.
	snapTagValueLen = 8
)

// SnapTagKey returns the B+ tree key for a named snapshot tag.
func SnapTagKey(label string) []byte {
	out := make([]byte, 0, len(SnapTagPrefix)+len(label))
	out = append(out, SnapTagPrefix...)
	return append(out, label...)
}

// ParseSnapTagLabel extracts the label from a key built by [SnapTagKey].
// Returns ok=false if the key does not have the expected prefix.
func ParseSnapTagLabel(key []byte) (label string, ok bool) {
	if len(key) <= len(SnapTagPrefix) {
		return "", false
	}
	if string(key[:len(SnapTagPrefix)]) != SnapTagPrefix {
		return "", false
	}
	return string(key[len(SnapTagPrefix):]), true
}

// EncodeSnapTagValue encodes commitSeq as an 8-byte big-endian value.
func EncodeSnapTagValue(commitSeq uint64) []byte {
	var v [snapTagValueLen]byte
	binary.BigEndian.PutUint64(v[:], commitSeq)
	return v[:]
}

// DecodeSnapTagValue decodes a commit_seq from a value produced by [EncodeSnapTagValue].
func DecodeSnapTagValue(v []byte) (commitSeq uint64, ok bool) {
	if len(v) != snapTagValueLen {
		return 0, false
	}
	return binary.BigEndian.Uint64(v), true
}
