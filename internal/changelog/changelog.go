// Package changelog implements the append-only logical changefeed (Phase G).
// See docs/hexxladb/CHANGEFEED.md.
package changelog

import (
	"bytes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sort"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	// MaxInlinePayload is the max encoded record size stored inline in the log.
	MaxInlinePayload = 4096

	// maxBodyBytes caps a single on-disk frame (defensive; keys are already bounded by callers).
	maxBodyBytes = 128 << 20

	plaintextHeaderSize = 16
	encryptedHeaderSize = 48
	headerMagicV1       = "HXCHGv01"
	headerMagicV2       = "HXCHGv02"
	formatV1            = uint32(1)
	formatV2            = uint32(2)
	flagEncrypted       = uint32(1)
	headerTagSize       = 16
	noncePrefixSize     = 16
	minimumPlainBody    = 28
	maxSequence         = ^uint64(0)
	intentFormatV1      = byte(1)
	intentFixedSize     = 1 + 8 + 1 + 1 + 4 + 32 + 4

	// checkpointStride bounds the number of historical frames ReadSince must
	// decode before reaching its cursor. Checkpoints are rebuilt during the
	// mandatory open scan and are not persisted.
	checkpointStride = uint64(256)
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

var (
	// ErrCorrupt is returned when changelog framing, sequence order, CRC, or authentication fails.
	ErrCorrupt = errors.New("changelog: corrupt file")
	// ErrEncryptionRequired means an encrypted changelog was opened without its key.
	ErrEncryptionRequired = errors.New("changelog: encryption key required")
	// ErrPlaintext means encrypted mode encountered a legacy plaintext changelog.
	ErrPlaintext = errors.New("changelog: legacy plaintext file")
	// ErrEncryptionKeyMismatch means the encrypted changelog header did not authenticate.
	ErrEncryptionKeyMismatch = errors.New("changelog: encryption key mismatch")
)

// Entry is one mutation to append in a batch (same wall-clock time).
type Entry struct {
	Op      byte
	Key     []byte
	Encoded []byte
}

// Intent is the bounded durable representation of one logical change. It is stored in the
// authoritative database before the external changelog is projected. Key is stored in the
// primary outbox key rather than this value so Intent remains bounded by MaxValueBytes.
type Intent struct {
	WallUnixNs int64
	Op         byte
	Key        []byte
	Hash       [32]byte
	HashValid  bool
	Inline     []byte
	EncodedLen uint32
}

// checkpoint maps an applied sequence to the offset of the following frame.
// The initial checkpoint maps sequence zero to the first frame.
type checkpoint struct {
	seq    uint64
	offset int64
}

// Log is an open changelog file (append + read).
type Log struct {
	path        string
	f           *os.File
	sync        bool
	maxSeq      uint64
	checkpoints []checkpoint
	dataOffset  int64
	aead        cipher.AEAD
	noncePrefix [noncePrefixSize]byte
	header      [encryptedHeaderSize]byte
}

// Open opens or creates a plaintext changelog file, validates the header, and scans for maxSeq.
func Open(path string, syncWrites bool) (*Log, error) {
	return open(path, syncWrites, nil, false)
}

// OpenRecoverable is like [Open] but may remove an incomplete final frame. Callers must use it
// only when authoritative durable intents exist to reconstruct every unacknowledged tail entry.
func OpenRecoverable(path string, syncWrites bool) (*Log, error) {
	return open(path, syncWrites, nil, true)
}

// OpenEncrypted opens or creates an authenticated encrypted changelog file.
// key must contain 32 bytes of changelog-specific key material.
func OpenEncrypted(path string, syncWrites bool, key []byte) (*Log, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, errors.New("changelog: encryption key must be 32 bytes")
	}
	return open(path, syncWrites, key, false)
}

// OpenEncryptedRecoverable is the authenticated equivalent of [OpenRecoverable].
func OpenEncryptedRecoverable(path string, syncWrites bool, key []byte) (*Log, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, errors.New("changelog: encryption key must be 32 bytes")
	}
	return open(path, syncWrites, key, true)
}

func open(path string, syncWrites bool, key []byte, repairIncompleteTail bool) (*Log, error) {
	// #nosec G304 -- path is the caller-selected database companion path, matching hexxladb.Open.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	l := &Log{
		path: path,
		f:    f,
		sync: syncWrites,
	}
	if st.Size() == 0 {
		if err := l.writeHeader(key); err != nil {
			_ = f.Close()
			return nil, err
		}
		return l, nil
	}
	if err := l.readHeaderAndScan(key, repairIncompleteTail); err != nil {
		_ = f.Close()
		return nil, err
	}
	return l, nil
}

func (l *Log) writeHeader(key []byte) error {
	if len(key) == 0 {
		var h [plaintextHeaderSize]byte
		copy(h[:], headerMagicV1)
		binary.BigEndian.PutUint32(h[8:], formatV1)
		if _, err := l.f.WriteAt(h[:], 0); err != nil {
			return err
		}
		l.dataOffset = plaintextHeaderSize
		l.checkpoints = []checkpoint{{seq: 0, offset: l.dataOffset}}
	} else {
		copy(l.header[:], headerMagicV2)
		binary.BigEndian.PutUint32(l.header[8:12], formatV2)
		binary.BigEndian.PutUint32(l.header[12:16], flagEncrypted)
		if _, err := rand.Read(l.noncePrefix[:]); err != nil {
			return err
		}
		copy(l.header[16:32], l.noncePrefix[:])
		if err := l.configureEncryption(key); err != nil {
			return err
		}
		tag := deriveHeaderTag(key, l.noncePrefix, l.header[:32])
		copy(l.header[32:], tag[:])
		if _, err := l.f.WriteAt(l.header[:], 0); err != nil {
			return err
		}
		l.dataOffset = encryptedHeaderSize
		l.checkpoints = []checkpoint{{seq: 0, offset: l.dataOffset}}
	}
	if l.sync {
		return l.f.Sync()
	}
	return nil
}

func (l *Log) readHeaderAndScan(key []byte, repairIncompleteTail bool) error {
	var prefix [plaintextHeaderSize]byte
	if _, err := l.f.ReadAt(prefix[:], 0); err != nil {
		return fmt.Errorf("%w: truncated header: %v", ErrCorrupt, err)
	}
	if string(prefix[:8]) != headerMagicV1 && string(prefix[:8]) != headerMagicV2 {
		return fmt.Errorf("%w: bad magic", ErrCorrupt)
	}
	switch string(prefix[:8]) {
	case headerMagicV1:
		if binary.BigEndian.Uint32(prefix[8:12]) != formatV1 {
			return fmt.Errorf("%w: unknown plaintext format", ErrCorrupt)
		}
		if len(key) > 0 {
			return ErrPlaintext
		}
		l.dataOffset = plaintextHeaderSize
	case headerMagicV2:
		if len(key) == 0 {
			return ErrEncryptionRequired
		}
		return l.readEncryptedHeaderAndScan(key, repairIncompleteTail)
	}
	highSeq, checkpoints, err := l.scanMaxSeq(repairIncompleteTail)
	if err != nil {
		return err
	}
	l.maxSeq = highSeq
	l.checkpoints = checkpoints
	return nil
}

func (l *Log) readEncryptedHeaderAndScan(key []byte, repairIncompleteTail bool) error {
	if _, err := l.f.ReadAt(l.header[:], 0); err != nil {
		return fmt.Errorf("%w: truncated encrypted header: %v", ErrCorrupt, err)
	}
	if string(l.header[:8]) != headerMagicV2 || binary.BigEndian.Uint32(l.header[8:12]) != formatV2 {
		return fmt.Errorf("%w: unknown encrypted format", ErrCorrupt)
	}
	if binary.BigEndian.Uint32(l.header[12:16]) != flagEncrypted {
		return fmt.Errorf("%w: unknown encrypted flags", ErrCorrupt)
	}
	copy(l.noncePrefix[:], l.header[16:32])
	want := deriveHeaderTag(key, l.noncePrefix, l.header[:32])
	if !hmac.Equal(l.header[32:], want[:]) {
		return ErrEncryptionKeyMismatch
	}
	if err := l.configureEncryption(key); err != nil {
		return err
	}
	l.dataOffset = encryptedHeaderSize
	highSeq, checkpoints, err := l.scanMaxSeq(repairIncompleteTail)
	if err != nil {
		return err
	}
	l.maxSeq = highSeq
	l.checkpoints = checkpoints
	return nil
}

func (l *Log) scanMaxSeq(repairIncompleteTail bool) (uint64, []checkpoint, error) {
	st, err := l.f.Stat()
	if err != nil {
		return 0, nil, err
	}
	off := l.dataOffset
	var highSeq uint64
	checkpoints := []checkpoint{{seq: 0, offset: l.dataOffset}}
	for off < st.Size() {
		if st.Size()-off < 4 {
			if repairIncompleteTail {
				return highSeq, checkpoints, l.truncateIncompleteTail(off)
			}
			return 0, nil, ErrCorrupt
		}
		var lenBuf [4]byte
		if _, err := l.f.ReadAt(lenBuf[:], off); err != nil {
			return 0, nil, err
		}
		n := int64(binary.BigEndian.Uint32(lenBuf[:]))
		if n < int64(l.minimumFrameBody()) || n > maxBodyBytes {
			return 0, nil, ErrCorrupt
		}
		if st.Size()-off < 4 || n > st.Size()-off-4 {
			if repairIncompleteTail {
				return highSeq, checkpoints, l.truncateIncompleteTail(off)
			}
			return 0, nil, ErrCorrupt
		}
		body := make([]byte, n)
		if _, err := l.f.ReadAt(body, off+4); err != nil {
			return 0, nil, err
		}
		rec, err := l.decodeFrame(body)
		if err != nil {
			return 0, nil, err
		}
		if rec.Seq != highSeq+1 {
			return 0, nil, fmt.Errorf("%w: non-contiguous sequence", ErrCorrupt)
		}
		highSeq = rec.Seq
		off += 4 + n
		if highSeq%checkpointStride == 0 {
			checkpoints = append(checkpoints, checkpoint{seq: highSeq, offset: off})
		}
	}
	return highSeq, checkpoints, nil
}

func (l *Log) truncateIncompleteTail(offset int64) error {
	if err := l.f.Truncate(offset); err != nil {
		return err
	}
	if _, err := l.f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	return l.f.Sync()
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

// Sync makes every successfully written changelog frame durable according to the file system.
func (l *Log) Sync() error {
	if l == nil || l.f == nil {
		return errors.New("changelog: closed")
	}
	return l.f.Sync()
}

// Append appends one record after a successful commit (assigns seq = maxSeq+1).
func (l *Log) Append(wallUnixNs int64, op byte, key, encoded []byte) error {
	if l == nil || l.f == nil {
		return errors.New("changelog: closed")
	}
	if len(key) > 65535 {
		return errors.New("changelog: key too long")
	}
	if l.maxSeq == maxSequence {
		return errors.New("changelog: sequence exhausted")
	}
	seq := l.maxSeq + 1
	inner, err := encodeInner(seq, wallUnixNs, op, key, encoded)
	if err != nil {
		return err
	}
	body, err := l.encodeFrame(seq, inner)
	if err != nil {
		return err
	}
	if len(body) > maxBodyBytes {
		return errors.New("changelog: record too large")
	}
	lenBuf := make([]byte, 4)
	// #nosec G115 -- body length is checked against maxBodyBytes above.
	binary.BigEndian.PutUint32(lenBuf, uint32(len(body))) //nolint:gosec // bounded by maxBodyBytes
	startOffset, err := l.f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := l.f.Write(lenBuf); err != nil {
		return err
	}
	if _, err := l.f.Write(body); err != nil {
		return err
	}
	l.maxSeq = seq
	l.addCheckpoint(seq, startOffset+int64(len(lenBuf))+int64(len(body)))
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
	if uint64(len(entries)) > maxSequence-l.maxSeq {
		return errors.New("changelog: sequence exhausted")
	}
	startSeq := l.maxSeq
	var buf bytes.Buffer
	var pendingCheckpoints []checkpoint
	for i := range entries {
		e := &entries[i]
		if len(e.Key) > 65535 {
			return errors.New("changelog: key too long")
		}
		seq := startSeq + uint64(i+1)
		inner, err := encodeInner(seq, wallUnixNs, e.Op, e.Key, e.Encoded)
		if err != nil {
			return err
		}
		body, err := l.encodeFrame(seq, inner)
		if err != nil {
			return err
		}
		var lenBuf [4]byte
		if len(body) > maxBodyBytes {
			return errors.New("changelog: record too large")
		}
		// #nosec G115 -- body length is checked against maxBodyBytes above.
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(body))) //nolint:gosec // bounded by maxBodyBytes
		if _, err := buf.Write(lenBuf[:]); err != nil {
			return err
		}
		if _, err := buf.Write(body); err != nil {
			return err
		}
		if seq%checkpointStride == 0 {
			pendingCheckpoints = append(pendingCheckpoints, checkpoint{seq: seq, offset: int64(buf.Len())})
		}
	}
	startOffset, err := l.f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := l.f.Write(buf.Bytes()); err != nil {
		return err
	}
	l.maxSeq = startSeq + uint64(len(entries))
	for i := range pendingCheckpoints {
		pendingCheckpoints[i].offset += startOffset
		l.checkpoints = append(l.checkpoints, pendingCheckpoints[i])
	}
	if l.sync {
		return l.f.Sync()
	}
	return nil
}

// AppendIntents appends a prepared durable commit projection. Each intent may carry a
// different timestamp so recovery can preserve the original commit time exactly.
func (l *Log) AppendIntents(intents []Intent) error {
	if l == nil || l.f == nil {
		return errors.New("changelog: closed")
	}
	if len(intents) == 0 {
		return nil
	}
	if uint64(len(intents)) > maxSequence-l.maxSeq {
		return errors.New("changelog: sequence exhausted")
	}
	startSeq := l.maxSeq
	var buf bytes.Buffer
	var pendingCheckpoints []checkpoint
	for i := range intents {
		intent := &intents[i]
		record := Record{
			Seq:        startSeq + uint64(i+1),
			WallUnixNs: intent.WallUnixNs,
			Op:         intent.Op,
			Key:        intent.Key,
			Hash:       intent.Hash,
			HashValid:  intent.HashValid,
			Inline:     intent.Inline,
			EncodedLen: intent.EncodedLen,
		}
		inner, err := encodeRecordInner(record)
		if err != nil {
			return err
		}
		body, err := l.encodeFrame(record.Seq, inner)
		if err != nil {
			return err
		}
		if len(body) > maxBodyBytes {
			return errors.New("changelog: record too large")
		}
		var lenBuf [4]byte
		// #nosec G115 -- body length is checked against maxBodyBytes above.
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(body))) //nolint:gosec // bounded by maxBodyBytes
		if _, err := buf.Write(lenBuf[:]); err != nil {
			return err
		}
		if _, err := buf.Write(body); err != nil {
			return err
		}
		if record.Seq%checkpointStride == 0 {
			pendingCheckpoints = append(pendingCheckpoints, checkpoint{seq: record.Seq, offset: int64(buf.Len())})
		}
	}
	startOffset, err := l.f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := l.f.Write(buf.Bytes()); err != nil {
		return err
	}
	l.maxSeq = startSeq + uint64(len(intents))
	for i := range pendingCheckpoints {
		pendingCheckpoints[i].offset += startOffset
		l.checkpoints = append(l.checkpoints, pendingCheckpoints[i])
	}
	if l.sync {
		return l.f.Sync()
	}
	return nil
}

// PrepareIntent computes the exact bounded payload metadata that both live projection and
// reopen recovery will append. maxValueBytes is the primary outbox value limit.
func PrepareIntent(wallUnixNs int64, entry Entry, maxValueBytes uint32) (Intent, error) {
	intent := Intent{WallUnixNs: wallUnixNs, Op: entry.Op, Key: bytes.Clone(entry.Key)}
	if len(entry.Encoded) > 0 {
		intent.Hash = sha256.Sum256(entry.Encoded)
		intent.HashValid = true
		if uint64(len(entry.Encoded)) > uint64(^uint32(0)) {
			return Intent{}, errors.New("changelog: encoded record too large")
		}
		// #nosec G115 -- entry length is rejected above when it exceeds uint32.
		intent.EncodedLen = uint32(len(entry.Encoded)) //nolint:gosec // bounded by the preceding check
		if len(entry.Encoded) <= MaxInlinePayload && intentFixedSize+len(entry.Encoded) <= int(maxValueBytes) {
			intent.Inline = bytes.Clone(entry.Encoded)
		}
	}
	if intentFixedSize+len(intent.Inline) > int(maxValueBytes) {
		return Intent{}, errors.New("changelog: MaxValueBytes too small for durable intent")
	}
	return intent, nil
}

// EncodeIntentValue encodes the outbox value. Intent.Key is deliberately excluded because it
// is stored in the ordered outbox key.
func EncodeIntentValue(intent Intent) ([]byte, error) {
	// #nosec G115 -- inline payloads are capped at MaxInlinePayload (4096 bytes).
	if len(intent.Inline) > 0 && uint32(len(intent.Inline)) != intent.EncodedLen { //nolint:gosec // inline max is 4096
		return nil, fmt.Errorf("%w: intent inline length mismatch", ErrCorrupt)
	}
	if !intent.HashValid && (intent.EncodedLen != 0 || len(intent.Inline) != 0) {
		return nil, fmt.Errorf("%w: intent payload metadata mismatch", ErrCorrupt)
	}
	flags := byte(0)
	if intent.HashValid {
		flags |= flagHash
	}
	if len(intent.Inline) > 0 {
		flags |= flagInline
	}
	buf := make([]byte, intentFixedSize+len(intent.Inline))
	buf[0] = intentFormatV1
	// #nosec G115 -- the wire format preserves signed int64 bits in a uint64 field.
	binary.BigEndian.PutUint64(buf[1:9], uint64(intent.WallUnixNs)) //nolint:gosec // signed instant stored as uint64 bits
	buf[9] = intent.Op
	buf[10] = flags
	binary.BigEndian.PutUint32(buf[11:15], intent.EncodedLen)
	copy(buf[15:47], intent.Hash[:])
	copy(buf[47:len(buf)-4], intent.Inline)
	binary.BigEndian.PutUint32(buf[len(buf)-4:], crc32.ChecksumIEEE(buf[:len(buf)-4]))
	return buf, nil
}

// DecodeIntentValue decodes one primary outbox value and attaches logicalKey from its key.
func DecodeIntentValue(logicalKey, value []byte) (Intent, error) {
	if len(value) < intentFixedSize || value[0] != intentFormatV1 {
		return Intent{}, ErrCorrupt
	}
	want := binary.BigEndian.Uint32(value[len(value)-4:])
	if crc32.ChecksumIEEE(value[:len(value)-4]) != want {
		return Intent{}, ErrCorrupt
	}
	flags := value[10]
	if flags & ^(flagHash|flagInline) != 0 || flags&flagInline != 0 && flags&flagHash == 0 {
		return Intent{}, ErrCorrupt
	}
	// #nosec G115 -- the wire format preserves signed int64 bits in a uint64 field.
	intent := Intent{
		WallUnixNs: int64(binary.BigEndian.Uint64(value[1:9])), //nolint:gosec // signed instant stored as uint64 bits
		Op:         value[9],
		Key:        bytes.Clone(logicalKey),
		EncodedLen: binary.BigEndian.Uint32(value[11:15]),
	}
	copy(intent.Hash[:], value[15:47])
	intent.HashValid = flags&flagHash != 0
	inline := value[47 : len(value)-4]
	if flags&flagInline != 0 {
		// #nosec G115 -- outbox values are bounded by MaxValueBytes (at most 1 MiB).
		if uint32(len(inline)) != intent.EncodedLen { //nolint:gosec // outbox values are capped by MaxValueBytes
			return Intent{}, ErrCorrupt
		}
		intent.Inline = bytes.Clone(inline)
	} else if len(inline) != 0 {
		return Intent{}, ErrCorrupt
	}
	if !intent.HashValid && intent.EncodedLen != 0 || intent.HashValid && intent.EncodedLen == 0 {
		return Intent{}, ErrCorrupt
	}
	return intent, nil
}

// ReadSince returns records with Seq > afterSeq, up to limit entries. It seeks
// to the nearest in-memory checkpoint, then scans forward.
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
	off := l.offsetAfter(afterSeq)
	if off < l.dataOffset || off > st.Size() {
		return nil, ErrCorrupt
	}
	for off < st.Size() && len(out) < limit {
		if st.Size()-off < 4 {
			return nil, ErrCorrupt
		}
		var lenBuf [4]byte
		if _, err := l.f.ReadAt(lenBuf[:], off); err != nil {
			return nil, err
		}
		n := int64(binary.BigEndian.Uint32(lenBuf[:]))
		if n < int64(l.minimumFrameBody()) || n > maxBodyBytes {
			return nil, ErrCorrupt
		}
		if st.Size()-off < 4 || n > st.Size()-off-4 {
			return nil, ErrCorrupt
		}
		body := make([]byte, n)
		if _, err := l.f.ReadAt(body, off+4); err != nil {
			return nil, err
		}
		rec, err := l.decodeFrame(body)
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

// CopyTo rewrites all logical records into an empty destination log, preserving sequences,
// timestamps, hashes, and inline payload policy while allowing a different frame encryption key.
func (l *Log) CopyTo(dst *Log) error {
	if l == nil || l.f == nil || dst == nil || dst.f == nil {
		return errors.New("changelog: closed")
	}
	if l == dst || l.path == dst.path {
		return errors.New("changelog: source and destination must differ")
	}
	if dst.maxSeq != 0 {
		return errors.New("changelog: destination is not empty")
	}
	const copyBatchSize = 256
	var cursor uint64
	for {
		records, err := l.ReadSince(cursor, copyBatchSize)
		if err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		if err := dst.appendRecords(records); err != nil {
			return err
		}
		cursor = records[len(records)-1].Seq
	}
}

func (l *Log) appendRecords(records []Record) error {
	if len(records) == 0 {
		return nil
	}
	if uint64(len(records)) > maxSequence-l.maxSeq {
		return errors.New("changelog: sequence exhausted")
	}
	startSeq := l.maxSeq
	var buf bytes.Buffer
	var pendingCheckpoints []checkpoint
	for i := range records {
		record := records[i]
		wantSeq := startSeq + uint64(i+1)
		if record.Seq != wantSeq {
			return fmt.Errorf("%w: non-contiguous copy sequence", ErrCorrupt)
		}
		inner, err := encodeRecordInner(record)
		if err != nil {
			return err
		}
		body, err := l.encodeFrame(record.Seq, inner)
		if err != nil {
			return err
		}
		var lenBuf [4]byte
		// #nosec G115 -- encodeFrame rejects bodies larger than maxBodyBytes.
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(body))) //nolint:gosec // body is bounded by encodeFrame
		if _, err := buf.Write(lenBuf[:]); err != nil {
			return err
		}
		if _, err := buf.Write(body); err != nil {
			return err
		}
		if record.Seq%checkpointStride == 0 {
			pendingCheckpoints = append(pendingCheckpoints, checkpoint{seq: record.Seq, offset: int64(buf.Len())})
		}
	}
	startOffset, err := l.f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := l.f.Write(buf.Bytes()); err != nil {
		return err
	}
	l.maxSeq = records[len(records)-1].Seq
	for i := range pendingCheckpoints {
		pendingCheckpoints[i].offset += startOffset
		l.checkpoints = append(l.checkpoints, pendingCheckpoints[i])
	}
	if l.sync {
		return l.f.Sync()
	}
	return nil
}

func (l *Log) addCheckpoint(seq uint64, offset int64) {
	if seq%checkpointStride == 0 {
		l.checkpoints = append(l.checkpoints, checkpoint{seq: seq, offset: offset})
	}
}

func (l *Log) offsetAfter(afterSeq uint64) int64 {
	i := sort.Search(len(l.checkpoints), func(i int) bool {
		return l.checkpoints[i].seq > afterSeq
	})
	if i == 0 {
		return l.dataOffset
	}
	return l.checkpoints[i-1].offset
}

func deriveSubkey(master []byte, noncePrefix [noncePrefixSize]byte, label string) [32]byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(label))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(noncePrefix[:])
	sum := mac.Sum(nil)
	var key [32]byte
	copy(key[:], sum)
	clear(sum)
	return key
}

func deriveHeaderTag(master []byte, noncePrefix [noncePrefixSize]byte, headerPrefix []byte) [headerTagSize]byte {
	key := deriveSubkey(master, noncePrefix, "hexxladb-changelog-header-mac-v2")
	defer clear(key[:])
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(headerPrefix)
	sum := mac.Sum(nil)
	defer clear(sum)
	var tag [headerTagSize]byte
	copy(tag[:], sum)
	return tag
}

func (l *Log) configureEncryption(master []byte) error {
	key := deriveSubkey(master, l.noncePrefix, "hexxladb-changelog-frame-aead-v2")
	defer clear(key[:])
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return err
	}
	l.aead = aead
	return nil
}

func (l *Log) minimumFrameBody() int {
	if l.aead == nil {
		return minimumPlainBody
	}
	return 8 + minimumPlainBody + l.aead.Overhead()
}

func (l *Log) frameNonce(seq uint64) [chacha20poly1305.NonceSizeX]byte {
	var nonce [chacha20poly1305.NonceSizeX]byte
	copy(nonce[:noncePrefixSize], l.noncePrefix[:])
	binary.BigEndian.PutUint64(nonce[noncePrefixSize:], seq)
	return nonce
}

func (l *Log) frameAAD(seq uint64, bodyLen uint32) [encryptedHeaderSize + 4 + 8]byte {
	var aad [encryptedHeaderSize + 4 + 8]byte
	copy(aad[:encryptedHeaderSize], l.header[:])
	binary.BigEndian.PutUint32(aad[encryptedHeaderSize:encryptedHeaderSize+4], bodyLen)
	binary.BigEndian.PutUint64(aad[encryptedHeaderSize+4:], seq)
	return aad
}

func (l *Log) encodeFrame(seq uint64, inner []byte) ([]byte, error) {
	if l.aead == nil {
		return inner, nil
	}
	bodyLen := 8 + len(inner) + l.aead.Overhead()
	if bodyLen > maxBodyBytes {
		return nil, errors.New("changelog: record too large")
	}
	nonce := l.frameNonce(seq)
	// #nosec G115 -- bodyLen is checked against maxBodyBytes above.
	aad := l.frameAAD(seq, uint32(bodyLen)) //nolint:gosec // bodyLen is bounded by maxBodyBytes
	body := make([]byte, 8, bodyLen)
	binary.BigEndian.PutUint64(body, seq)
	body = l.aead.Seal(body, nonce[:], inner, aad[:])
	return body, nil
}

func (l *Log) decodeFrame(body []byte) (Record, error) {
	if l.aead == nil {
		return decodeInner(body)
	}
	var record Record
	if len(body) < l.minimumFrameBody() {
		return record, ErrCorrupt
	}
	seq := binary.BigEndian.Uint64(body[:8])
	if seq == 0 {
		return record, ErrCorrupt
	}
	nonce := l.frameNonce(seq)
	// #nosec G115 -- callers cap frame bodies at maxBodyBytes before decode.
	aad := l.frameAAD(seq, uint32(len(body))) //nolint:gosec // frame length is capped before decode
	inner, err := l.aead.Open(nil, nonce[:], body[8:], aad[:])
	if err != nil {
		return record, fmt.Errorf("%w: frame authentication failed", ErrCorrupt)
	}
	record, err = decodeInner(inner)
	if err != nil {
		return record, err
	}
	if record.Seq != seq {
		return Record{}, fmt.Errorf("%w: frame sequence mismatch", ErrCorrupt)
	}
	return record, nil
}

func encodeInner(seq uint64, wallUnixNs int64, op byte, key, encoded []byte) ([]byte, error) {
	record := Record{Seq: seq, WallUnixNs: wallUnixNs, Op: op, Key: key}
	if len(encoded) > 0 {
		record.Hash = sha256.Sum256(encoded)
		record.HashValid = true
		// #nosec G115 -- encoded records originate from values bounded by the database MaxValueBytes contract.
		record.EncodedLen = uint32(len(encoded)) //nolint:gosec // bounded by the database MaxValueBytes contract
		if len(encoded) <= MaxInlinePayload {
			record.Inline = encoded
		}
	}
	return encodeRecordInner(record)
}

func encodeRecordInner(record Record) ([]byte, error) {
	if len(record.Key) > 65535 {
		return nil, errors.New("changelog: key too long")
	}
	// #nosec G115 -- inline payloads are capped at MaxInlinePayload (4096 bytes).
	if len(record.Inline) > 0 && uint32(len(record.Inline)) != record.EncodedLen { //nolint:gosec // inline max is 4096
		return nil, fmt.Errorf("%w: inline length mismatch", ErrCorrupt)
	}
	if !record.HashValid && (record.EncodedLen != 0 || len(record.Inline) != 0) {
		return nil, fmt.Errorf("%w: payload metadata mismatch", ErrCorrupt)
	}
	var flags byte
	if record.HashValid {
		flags |= flagHash
	}
	if len(record.Inline) > 0 {
		flags |= flagInline
	}
	// inner: seq wall op keyLen key flags [hash] encLen inline crc32
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, record.Seq)
	_ = binary.Write(buf, binary.BigEndian, record.WallUnixNs)
	_ = buf.WriteByte(record.Op)
	// #nosec G115 -- key length is rejected above when it exceeds uint16.
	_ = binary.Write(buf, binary.BigEndian, uint16(len(record.Key))) //nolint:gosec // len(key) <= 65535
	if _, err := buf.Write(record.Key); err != nil {
		return nil, err
	}
	_ = buf.WriteByte(flags)
	if flags&flagHash != 0 {
		if _, err := buf.Write(record.Hash[:]); err != nil {
			return nil, err
		}
	}
	_ = binary.Write(buf, binary.BigEndian, record.EncodedLen)
	if len(record.Inline) > 0 {
		if _, err := buf.Write(record.Inline); err != nil {
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
	// #nosec G115 -- the wire format preserves signed int64 bits in a uint64 field.
	r.WallUnixNs = int64(binary.BigEndian.Uint64(body[off:])) //nolint:gosec // signed instant stored as uint64 bits
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
	if flags & ^(flagHash|flagInline) != 0 {
		return r, ErrCorrupt
	}
	if flags&flagInline != 0 && flags&flagHash == 0 {
		return r, ErrCorrupt
	}
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
	if flags&flagHash == 0 && r.EncodedLen != 0 {
		return r, ErrCorrupt
	}
	if flags&flagHash != 0 && r.EncodedLen == 0 {
		return r, ErrCorrupt
	}
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
