package engine

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// Compression envelope stored in leaf value (or before overflow):
//
//	[0]      0xFE (compression magic — distinct from overflow 0xFF and record 'H'=0x48)
//	[1..4]   uint32 big-endian — uncompressed length (for pre-allocation)
//	[5..]    DEFLATE-compressed payload
//
// Total overhead: 5 bytes + DEFLATE stream.

const (
	compressMagic      = byte(0xFE)
	compressHeaderSize = 1 + 4 // magic + uncompressedLen
	compressMinInput   = 64    // values shorter than this skip compression
)

// flateWriterPool reuses *flate.Writer instances (~256 KiB each).
var flateWriterPool = sync.Pool{
	New: func() any {
		w, _ := flate.NewWriter(nil, flate.BestSpeed)
		return w
	},
}

// isCompressedValue reports whether val starts with the compression magic.
func isCompressedValue(val []byte) bool {
	return len(val) > compressHeaderSize && val[0] == compressMagic
}

// compressValue compresses val using DEFLATE. Returns the original val unchanged
// if the input is too short or compression didn't shrink it.
//
// Exception: a raw value that would itself be misread as a compression envelope
// (starts with compressMagic and is long enough for isCompressedValue) MUST be
// wrapped in the envelope even when compression does not shrink it, otherwise
// decompressValue would try to inflate raw bytes on read and corrupt the value.
func compressValue(val []byte) []byte {
	mustWrap := isCompressedValue(val)

	if len(val) < compressMinInput && !mustWrap {
		return val
	}

	var buf bytes.Buffer
	buf.Grow(compressHeaderSize + len(val))

	// Write envelope header.
	buf.WriteByte(compressMagic)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(val))) //nolint:gosec // len(val) bounded by maxValueBytes (uint32)
	buf.Write(lenBuf[:])

	// Compress payload.
	w := flateWriterPool.Get().(*flate.Writer)
	w.Reset(&buf)
	if _, err := w.Write(val); err != nil {
		_ = w.Close()
		flateWriterPool.Put(w)
		return val // compression failed (in-memory writer); store raw
	}
	if err := w.Close(); err != nil {
		flateWriterPool.Put(w)
		return val // compression failed; store raw
	}
	flateWriterPool.Put(w)

	// Only use compressed form if it's actually smaller, unless the raw value
	// must be wrapped to avoid being misread as a compression envelope.
	if buf.Len() >= len(val) && !mustWrap {
		return val
	}
	return buf.Bytes()
}

// decompressValue decompresses a compressed value envelope. Returns an error
// if the envelope is malformed or decompression fails.
func decompressValue(val []byte) ([]byte, error) {
	if len(val) <= compressHeaderSize {
		return nil, fmt.Errorf("%w: compressed value too short", ErrCorruptTree)
	}
	uncompLen := binary.BigEndian.Uint32(val[1:5])
	r := flate.NewReader(bytes.NewReader(val[compressHeaderSize:]))
	defer func() { _ = r.Close() }()

	out, err := io.ReadAll(io.LimitReader(r, int64(uncompLen)+1))
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	if uint32(len(out)) != uncompLen { //nolint:gosec // len bounded by uncompLen
		return nil, fmt.Errorf("%w: decompressed length mismatch: got %d want %d", ErrCorruptTree, len(out), uncompLen)
	}
	return out, nil
}
