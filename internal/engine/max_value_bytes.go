package engine

// validMaxValueBytes lists the accepted per-database value size limits.
var validMaxValueBytes = [...]uint32{512, 1024, 2048, 4096, 8192, 16384}

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
