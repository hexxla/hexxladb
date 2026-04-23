package record

// Binary record envelope and payload versioning.
// See FORMAT.md for full layout.

const (
	headerSize = 4 + 2 + 4 // magic + format_version + payload_len

	// FormatVersionV1 is the only version implemented by this package.
	FormatVersionV1 = 1

	// SupportedFormatVersion is the highest format version this binary can decode.
	SupportedFormatVersion = FormatVersionV1

	// MaxPayload is the maximum allowed payload size after the header (DoS bound).
	MaxPayload = 16 << 20 // 16 MiB

	// MaxStringField is the maximum UTF-8 byte length for a single length-prefixed string field.
	MaxStringField = 1 << 20 // 1 MiB
)

// Four-byte ASCII magics per record family (distinct to catch cross-decode mistakes).
var (
	MagicCell  = [4]byte{'H', 'X', 'C', 'L'}
	MagicFacet = [4]byte{'H', 'X', 'F', 'C'}
	MagicEdge  = [4]byte{'H', 'X', 'E', 'D'}
	MagicSeam  = [4]byte{'H', 'X', 'S', 'M'}
)
