package index

import (
	"bytes"
	"encoding/binary"
)

const changelogOutboxPrefix = "__meta/changelog-outbox/"

const changelogHeadKey = "__meta/changelog-head"

// ChangelogHeadKey stores the last durable outbox commit identifier for format-v1 databases.
func ChangelogHeadKey() []byte { return []byte(changelogHeadKey) }

// ChangelogOutboxKey orders durable changefeed intents by commit and mutation ordinal.
// The logical mutation key is retained in the key suffix so the bounded outbox value can
// always fit within the database's configured MaxValueBytes.
func ChangelogOutboxKey(commitSeq uint64, ordinal uint32, logicalKey []byte) []byte {
	out := make([]byte, 0, len(changelogOutboxPrefix)+8+4+len(logicalKey))
	out = append(out, changelogOutboxPrefix...)
	var commit [8]byte
	binary.BigEndian.PutUint64(commit[:], commitSeq)
	out = append(out, commit[:]...)
	var index [4]byte
	binary.BigEndian.PutUint32(index[:], ordinal)
	out = append(out, index[:]...)
	return append(out, logicalKey...)
}

// ChangelogOutboxBounds returns the inclusive B+ tree range for durable changefeed intents.
func ChangelogOutboxBounds() (from, to []byte) {
	from = []byte(changelogOutboxPrefix)
	to = append(bytes.Clone(from), 0xff)
	return from, to
}

// ParseChangelogOutboxKey decodes a key produced by [ChangelogOutboxKey].
func ParseChangelogOutboxKey(key []byte) (commitSeq uint64, ordinal uint32, logicalKey []byte, ok bool) {
	if len(key) < len(changelogOutboxPrefix)+12 || !bytes.HasPrefix(key, []byte(changelogOutboxPrefix)) {
		return 0, 0, nil, false
	}
	off := len(changelogOutboxPrefix)
	commitSeq = binary.BigEndian.Uint64(key[off : off+8])
	ordinal = binary.BigEndian.Uint32(key[off+8 : off+12])
	logicalKey = bytes.Clone(key[off+12:])
	return commitSeq, ordinal, logicalKey, true
}
