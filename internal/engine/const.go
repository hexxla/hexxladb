package engine

import "fmt"

// DefaultPageSize is the page size for newly created databases (4 KiB).
const DefaultPageSize = 4 << 10

// AuthenticatedFormatVersion is the MVCC engine format with authenticated data pages.
const AuthenticatedFormatVersion uint32 = 3

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
	formatVersionV2  = uint32(2)                  // MVCC: commit_seq in header (see ENGINE_FORMAT.md).
	formatVersionV3  = AuthenticatedFormatVersion // MVCC plus authenticated data-page envelopes.

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

// HeaderAuthTagOffset is the byte offset of the authenticated-format header MAC.
const HeaderAuthTagOffset = 107

// HeaderAuthTagLen is the byte length of the authenticated-format header MAC.
const HeaderAuthTagLen = 32

// HeaderBTreeRootGenerationOffset is the rewrite generation expected for the root page.
const HeaderBTreeRootGenerationOffset = HeaderAuthTagOffset + HeaderAuthTagLen

// HeaderFreelistHeadOffset is the byte offset of the first authenticated
// freelist metadata page id. A zero id means all free ids fit in the header.
const HeaderFreelistHeadOffset = HeaderBTreeRootGenerationOffset + 8

// HeaderFreelistHeadGenerationOffset is the byte offset of the expected
// rewrite generation for HeaderFreelistHead.
const HeaderFreelistHeadGenerationOffset = HeaderFreelistHeadOffset + 8

// HeaderFreelistCountOffset is the byte offset of the total reusable-page count.
const HeaderFreelistCountOffset = HeaderFreelistHeadGenerationOffset + 8

// HeaderInlineFreelistOffset starts the authenticated inline free-id array.
const HeaderInlineFreelistOffset = HeaderFreelistCountOffset + 8

// HeaderInlineFreelistCapacity keeps small free sets entirely in page 0.
const HeaderInlineFreelistCapacity = 40

// AuthenticatedPageOverhead is generation(8) + XChaCha20 nonce(24) + tag(16).
const AuthenticatedPageOverhead = 48
