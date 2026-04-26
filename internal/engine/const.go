package engine

// PageSize is the fixed on-disk page size for v1 (64 KiB).
const PageSize = 64 << 10

const (
	headerMagic      = "HEXXLADB"
	headerPrefixSize = 512
	formatVersionV1  = uint32(1)
	formatVersionV2  = uint32(2) // MVCC: commit_seq in header (see ENGINE_FORMAT.md).

	btreeNodeMagic = "HXBT"
)

// HeaderCommitSeqOffset is the byte offset of CommitSeq in the 512-byte file header (format v2).
const HeaderCommitSeqOffset = 60

// HeaderEncryptionKeyCheckOffset is the byte offset of the keyed verifier used for wrong-key detection.
const HeaderEncryptionKeyCheckOffset = 68

// HeaderEncryptionKeyCheckLen is the verifier byte length.
const HeaderEncryptionKeyCheckLen = 32

// HeaderMaxValueBytesOffset is the byte offset of the per-database max value size (uint32, big-endian).
// Zero on disk means the engine default (8192 bytes) is used.
const HeaderMaxValueBytesOffset = 100

// DefaultMaxValueBytes is the engine default maximum value size when Options.MaxValueBytes is zero.
const DefaultMaxValueBytes = 8192
