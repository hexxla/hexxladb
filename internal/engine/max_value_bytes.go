package engine

const validMaxValueBytesText = "512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, or 1048576"

// validMaxValueBytes lists the accepted per-database value size limits.
// Values ≤ inlineThreshold(pageSize) fit in a single leaf entry; larger values
// spill to overflow pages automatically.
var validMaxValueBytes = [...]uint32{512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, 1048576}

// resolveMaxValueBytes validates opts.MaxValueBytes and returns the canonical
// on-disk value (0 = use default; non-zero = explicit limit to persist).
// Zero in opts means accept the default; the header stores 0 and Open converts to DefaultMaxValueBytes.
func resolveMaxValueBytes(opts *Options) (uint32, error) {
	if opts == nil || opts.MaxValueBytes == 0 {
		return 0, nil // zero → header stores 0 → Open uses DefaultMaxValueBytes
	}
	for _, v := range validMaxValueBytes {
		if opts.MaxValueBytes == v {
			return opts.MaxValueBytes, nil
		}
	}
	return 0, ErrInvalidMaxValueBytes
}

// MaxValueBytes returns the effective per-database maximum value size in bytes.
// This is the value read from the file header at Open (or DefaultMaxValueBytes if the header field was zero).
func (e *Engine) MaxValueBytes() uint32 {
	return e.maxValueBytes
}
