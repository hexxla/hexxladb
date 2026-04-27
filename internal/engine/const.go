package engine

import "fmt"

// DefaultPageSize is the page size for newly created databases (4 KiB).
const DefaultPageSize = 4 << 10

// LegacyPageSize is the page size used by databases created before
// configurable page size was introduced.
const LegacyPageSize = 64 << 10

// validPageSizes lists accepted page sizes (must be power of 2, ≥4 KiB).
var validPageSizes = [...]uint32{4096, 8192, 16384, 65536}

// IsValidPageSize reports whether ps is a supported page size.
func IsValidPageSize(ps uint32) bool {
	for _, v := range validPageSizes {
		if ps == v {
			return true
		}
	}
	return false
}

// resolvePageSize returns the effective page size from Options.
// Zero means DefaultPageSize; invalid values return an error.
func resolvePageSize(opts *Options) (uint32, error) {
	if opts == nil || opts.PageSize == 0 {
		return DefaultPageSize, nil
	}
	if !IsValidPageSize(opts.PageSize) {
		return 0, fmt.Errorf("%w: %d", ErrInvalidPageSize, opts.PageSize)
	}
	return opts.PageSize, nil
}

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

// HeaderEmbeddingDimOffset is the byte offset of the embedding vector dimension (uint16, big-endian).
// Zero means embeddings are disabled.
const HeaderEmbeddingDimOffset = 104

// HeaderEmbeddingMetricOffset is the byte offset of the distance metric (uint8).
const HeaderEmbeddingMetricOffset = 106
