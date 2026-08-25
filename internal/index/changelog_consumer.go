package index

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
)

const (
	changelogConsumerPrefix       = "__meta/changelog-consumer/"
	changelogProjectionCheckpoint = "__meta/changelog-projection-checkpoint"

	// ChangelogConsumerMaxIDBytes bounds caller-controlled consumer identity keys.
	ChangelogConsumerMaxIDBytes = 128

	changelogConsumerValueVersion   = byte(1)
	changelogCheckpointValueVersion = byte(1)
	changelogConsumerValueSize      = 1 + 8 + 4
	changelogCheckpointValueSize    = 1 + 8 + 32 + 4
)

// ValidChangelogConsumerID reports whether id is a stable ASCII identifier.
// The first byte must be alphanumeric; subsequent bytes may also use '.', '_',
// ':', or '-'.
func ValidChangelogConsumerID(id string) bool {
	if id == "" || len(id) > ChangelogConsumerMaxIDBytes || !isConsumerIDAlphaNumeric(id[0]) {
		return false
	}
	for i := 1; i < len(id); i++ {
		if !isConsumerIDAlphaNumeric(id[i]) && id[i] != '.' && id[i] != '_' && id[i] != ':' && id[i] != '-' {
			return false
		}
	}
	return true
}

func isConsumerIDAlphaNumeric(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// ChangelogConsumerKey returns the authoritative metadata key for id.
func ChangelogConsumerKey(id string) []byte {
	return append([]byte(changelogConsumerPrefix), id...)
}

// ChangelogConsumerBounds returns the inclusive range containing cursor records.
func ChangelogConsumerBounds() (from, to []byte) {
	from = []byte(changelogConsumerPrefix)
	to = append(bytes.Clone(from), 0xff)
	return from, to
}

// ParseChangelogConsumerKey returns the validated identity suffix.
func ParseChangelogConsumerKey(key []byte) (string, bool) {
	id, ok := bytes.CutPrefix(key, []byte(changelogConsumerPrefix))
	if !ok || !ValidChangelogConsumerID(string(id)) {
		return "", false
	}
	return string(id), true
}

// EncodeChangelogConsumerCursor encodes one acknowledged sidecar sequence.
func EncodeChangelogConsumerCursor(seq uint64) []byte {
	value := make([]byte, changelogConsumerValueSize)
	value[0] = changelogConsumerValueVersion
	binary.BigEndian.PutUint64(value[1:9], seq)
	binary.BigEndian.PutUint32(value[9:], crc32.ChecksumIEEE(value[:9]))
	return value
}

// DecodeChangelogConsumerCursor validates and decodes one cursor value.
func DecodeChangelogConsumerCursor(value []byte) (uint64, bool) {
	if len(value) != changelogConsumerValueSize || value[0] != changelogConsumerValueVersion {
		return 0, false
	}
	if binary.BigEndian.Uint32(value[9:]) != crc32.ChecksumIEEE(value[:9]) {
		return 0, false
	}
	return binary.BigEndian.Uint64(value[1:9]), true
}

// ChangelogProjectionCheckpointKey stores the acknowledged logical sidecar head.
func ChangelogProjectionCheckpointKey() []byte {
	return []byte(changelogProjectionCheckpoint)
}

// EncodeChangelogProjectionCheckpoint binds seq to its rolling logical digest.
func EncodeChangelogProjectionCheckpoint(seq uint64, digest [32]byte) []byte {
	value := make([]byte, changelogCheckpointValueSize)
	value[0] = changelogCheckpointValueVersion
	binary.BigEndian.PutUint64(value[1:9], seq)
	copy(value[9:41], digest[:])
	binary.BigEndian.PutUint32(value[41:], crc32.ChecksumIEEE(value[:41]))
	return value
}

// DecodeChangelogProjectionCheckpoint validates and decodes the projected head.
func DecodeChangelogProjectionCheckpoint(value []byte) (seq uint64, digest [32]byte, ok bool) {
	if len(value) != changelogCheckpointValueSize || value[0] != changelogCheckpointValueVersion {
		return 0, digest, false
	}
	if binary.BigEndian.Uint32(value[41:]) != crc32.ChecksumIEEE(value[:41]) {
		return 0, digest, false
	}
	seq = binary.BigEndian.Uint64(value[1:9])
	copy(digest[:], value[9:41])
	return seq, digest, true
}
