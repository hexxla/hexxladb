// Package changelog implements the append-only logical changefeed (Phase G).
// See docs/hexxladb/CHANGEFEED.md.
package changelog

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

const (
	// MaxInlinePayload is the max encoded record size stored inline in the log.
	MaxInlinePayload = 4096

	// maxBodyBytes caps a single on-disk frame (defensive; keys are already bounded by callers).
	maxBodyBytes = 128 << 20

	headerSize  = 16
	headerMagic = "HXCHGv01"
	formatVer   = uint32(1)
)

// Operation codes (must stay stable for consumers).
// UpdateFacet is recorded as OpPutFacet; LinkCells as OpPutEdge; MarkConflict as OpPutSeam.
// ResolveSeam uses the same seam write path as PutSeam but is logged as OpResolveSeam.
const (
	OpPutCell     = byte(1)
	OpPutSeam     = byte(2)
	OpResolveSeam = byte(3)
	OpPutFacet    = byte(4)
	OpPutEdge     = byte(5)
	OpDeleteCell  = byte(6)
)

const (
	flagHash   = byte(1)
	flagInline = byte(2)
)

// Record is one decoded changefeed entry.
type Record struct {
	Seq        uint64
	WallUnixNs int64
	Op         byte
	Key        []byte
	Hash       [32]byte
	HashValid  bool
	Inline     []byte
	EncodedLen uint32 // logical encoded record size (for hash-only large rows)
}

// ErrCorrupt is returned when the changelog file is truncated or CRC fails.
var ErrCorrupt = errors.New("changelog: corrupt file")

// Entry is one mutation to append in a batch (same wall-clock time).
type Entry struct {
	Op      byte
	Key     []byte
	Encoded []byte
}

// Log is an open changelog file (append + read).
type Log struct {
	path   string
	f      *os.File
	sync   bool
	maxSeq uint64
}

// Open opens or creates a changelog file, validates the header, and scans for maxSeq.
func Open(path string, syncWrites bool) (*Log, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // path is DB companion file from Open
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	l := &Log{path: path, f: f, sync: syncWrites}
	if st.Size() == 0 {
		if err := l.writeHeader(); err != nil {
			_ = f.Close()
			return nil, err
		}
		return l, nil
	}
	if err := l.readHeaderAndScan(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return l, nil
}

func (l *Log) writeHeader() error {
	var h [headerSize]byte
	copy(h[:], headerMagic)
	binary.BigEndian.PutUint32(h[8:], formatVer)
	if _, err := l.f.WriteAt(h[:], 0); err != nil {
		return err
	}
	if l.sync {
		return l.f.Sync()
	}
	return nil
}

func (l *Log) readHeaderAndScan() error {
	var h [headerSize]byte
	if _, err := l.f.ReadAt(h[:], 0); err != nil {
		return err
	}
	if string(h[:8]) != headerMagic {
		return fmt.Errorf("%w: bad magic", ErrCorrupt)
	}
	if binary.BigEndian.Uint32(h[8:]) != formatVer {
		return fmt.Errorf("%w: unknown format", ErrCorrupt)
	}
	highSeq, err := scanMaxSeq(l.f, headerSize)
	if err != nil {
		return err
	}
	l.maxSeq = highSeq
	return nil
}

func scanMaxSeq(f *os.File, start int64) (uint64, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	off := start
	var highSeq uint64
	for off < st.Size() {
		var lenBuf [4]byte
		if _, err := f.ReadAt(lenBuf[:], off); err != nil {
			return 0, err
		}
		n := int64(binary.BigEndian.Uint32(lenBuf[:]))
		if n < 28 { // minimum frame (empty key, no payload hash path still needs encLen+crc)
			return 0, ErrCorrupt
		}
		if off+4+n > st.Size() {
			return 0, ErrCorrupt
		}
		body := make([]byte, n)
		if _, err := f.ReadAt(body, off+4); err != nil {
			return 0, err
		}
		rec, err := decodeInner(body)
		if err != nil {
			return 0, err
		}
		if rec.Seq > highSeq {
			highSeq = rec.Seq
		}
		off += 4 + n
	}
	return highSeq, nil
}

// Close releases the file handle.
func (l *Log) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// Path returns the filesystem path.
func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// MaxSeq returns the highest sequence number present (0 if empty).
func (l *Log) MaxSeq() uint64 {
	if l == nil {
		return 0
	}
	return l.maxSeq
}

// Append appends one record after a successful commit (assigns seq = maxSeq+1).
func (l *Log) Append(wallUnixNs int64, op byte, key, encoded []byte) error {
	if l == nil || l.f == nil {
		return errors.New("changelog: closed")
	}
	if len(key) > 65535 {
		return errors.New("changelog: key too long")
	}
	l.maxSeq++
	seq := l.maxSeq
	body, err := encodeInner(seq, wallUnixNs, op, key, encoded)
	if err != nil {
		l.maxSeq--
		return err
	}
	if len(body) > maxBodyBytes {
		return errors.New("changelog: record too large")
	}
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(body))) //nolint:gosec // bounded by maxBodyBytes
	if _, err := l.f.Seek(0, io.SeekEnd); err != nil {
		l.maxSeq--
		return err
	}
	if _, err := l.f.Write(lenBuf); err != nil {
		l.maxSeq--
		return err
	}
	if _, err := l.f.Write(body); err != nil {
		l.maxSeq--
		return err
	}
	if l.sync {
		return l.f.Sync()
	}
	return nil
}

// AppendBatch appends multiple records in one write (and one Sync when sync mode).
func (l *Log) AppendBatch(wallUnixNs int64, entries []Entry) error {
	if l == nil || l.f == nil {
		return errors.New("changelog: closed")
	}
	if len(entries) == 0 {
		return nil
	}
	startSeq := l.maxSeq
	var buf bytes.Buffer
	for i := range entries {
		e := &entries[i]
		if len(e.Key) > 65535 {
			return errors.New("changelog: key too long")
		}
		seq := startSeq + uint64(i+1)
		body, err := encodeInner(seq, wallUnixNs, e.Op, e.Key, e.Encoded)
		if err != nil {
			return err
		}
		var lenBuf [4]byte
		if len(body) > maxBodyBytes {
			return errors.New("changelog: record too large")
		}
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(body))) //nolint:gosec // bounded by maxBodyBytes
		if _, err := buf.Write(lenBuf[:]); err != nil {
			return err
		}
		if _, err := buf.Write(body); err != nil {
			return err
		}
	}
	if _, err := l.f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	if _, err := l.f.Write(buf.Bytes()); err != nil {
		return err
	}
	l.maxSeq = startSeq + uint64(len(entries))
	if l.sync {
		return l.f.Sync()
	}
	return nil
}

// ReadSince returns records with Seq > afterSeq, up to limit entries, scanning from the start of the log.
func (l *Log) ReadSince(afterSeq uint64, limit int) ([]Record, error) {
	if l == nil || l.f == nil {
		return nil, errors.New("changelog: closed")
	}
	if limit <= 0 {
		return nil, nil
	}
	st, err := l.f.Stat()
	if err != nil {
		return nil, err
	}
	var out []Record
	off := int64(headerSize)
	for off < st.Size() && len(out) < limit {
		var lenBuf [4]byte
		if _, err := l.f.ReadAt(lenBuf[:], off); err != nil {
			return nil, err
		}
		n := int64(binary.BigEndian.Uint32(lenBuf[:]))
		if n < 28 || off+4+n > st.Size() {
			return nil, ErrCorrupt
		}
		body := make([]byte, n)
		if _, err := l.f.ReadAt(body, off+4); err != nil {
			return nil, err
		}
		rec, err := decodeInner(body)
		if err != nil {
			return nil, err
		}
		if rec.Seq > afterSeq {
			out = append(out, rec)
		}
		off += 4 + n
	}
	return out, nil
}

func encodeInner(seq uint64, wallUnixNs int64, op byte, key, encoded []byte) ([]byte, error) {
	var flags byte
	var hash [32]byte
	if len(encoded) > 0 {
		flags |= flagHash
		h := sha256.Sum256(encoded)
		copy(hash[:], h[:])
	}
	var inline []byte
	var encLen uint32
	if len(encoded) > 0 {
		encLen = uint32(len(encoded)) //nolint:gosec // practical max far below MaxUint32
		if len(encoded) <= MaxInlinePayload {
			flags |= flagInline
			inline = encoded
		}
	}
	// inner: seq wall op keyLen key flags [hash] encLen inline crc32
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, seq)
	_ = binary.Write(buf, binary.BigEndian, wallUnixNs)
	_ = buf.WriteByte(op)
	_ = binary.Write(buf, binary.BigEndian, uint16(len(key))) //nolint:gosec // len(key) <= 65535
	if _, err := buf.Write(key); err != nil {
		return nil, err
	}
	_ = buf.WriteByte(flags)
	if flags&flagHash != 0 {
		if _, err := buf.Write(hash[:]); err != nil {
			return nil, err
		}
	}
	_ = binary.Write(buf, binary.BigEndian, encLen)
	if len(inline) > 0 {
		if _, err := buf.Write(inline); err != nil {
			return nil, err
		}
	}
	sum := crc32.ChecksumIEEE(buf.Bytes())
	_ = binary.Write(buf, binary.BigEndian, sum)
	return buf.Bytes(), nil
}

func decodeInner(body []byte) (Record, error) {
	var r Record
	if len(body) < 8+8+1+2+1+4+4 {
		return r, ErrCorrupt
	}
	off := 0
	r.Seq = binary.BigEndian.Uint64(body[off:])
	off += 8
	r.WallUnixNs = int64(binary.BigEndian.Uint64(body[off:])) //nolint:gosec // wire format stores signed instant as uint64 bits
	off += 8
	r.Op = body[off]
	off++
	klen := int(binary.BigEndian.Uint16(body[off:]))
	off += 2
	if klen < 0 || off+klen > len(body) {
		return r, ErrCorrupt
	}
	r.Key = append([]byte(nil), body[off:off+klen]...)
	off += klen
	if off >= len(body) {
		return r, ErrCorrupt
	}
	flags := body[off]
	off++
	if flags&flagHash != 0 {
		if off+32 > len(body) {
			return r, ErrCorrupt
		}
		copy(r.Hash[:], body[off:off+32])
		r.HashValid = true
		off += 32
	}
	if off+4 > len(body) {
		return r, ErrCorrupt
	}
	r.EncodedLen = binary.BigEndian.Uint32(body[off:])
	off += 4
	inlineLen := 0
	if flags&flagInline != 0 {
		inlineLen = int(r.EncodedLen)
		if off+inlineLen+4 > len(body) {
			return r, ErrCorrupt
		}
		r.Inline = append([]byte(nil), body[off:off+inlineLen]...)
		off += inlineLen
	}
	// crc32 last 4
	if off+4 != len(body) {
		return r, ErrCorrupt
	}
	want := binary.BigEndian.Uint32(body[off:])
	got := crc32.ChecksumIEEE(body[:off])
	if want != got {
		return r, ErrCorrupt
	}
	return r, nil
}
