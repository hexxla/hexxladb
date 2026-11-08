package engine

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

// WAL record: seq(8) + page_id(8) + crc32(4) + payload(pageSize).
const walRecordOverhead = 8 + 8 + 4
const walRecordMACSize = 32

func walRecordSize(pageSize int) int {
	return walRecordOverhead + pageSize
}

func encodeWALRecord(seq, pageID uint64, payload []byte, pageSize int) []byte {
	return encodeWALRecordWithMAC(seq, pageID, payload, [32]byte{}, false, pageSize)
}

func encodeWALRecordWithMAC(seq, pageID uint64, payload []byte, macKey [32]byte, useMAC bool, pageSize int) []byte {
	if len(payload) != pageSize {
		panic("engine: wal payload size mismatch")
	}
	size := walRecordSize(pageSize)
	if useMAC {
		size += walRecordMACSize
	}
	out := make([]byte, size)
	binary.BigEndian.PutUint64(out[0:8], seq)
	binary.BigEndian.PutUint64(out[8:16], pageID)
	binary.BigEndian.PutUint32(out[16:20], crc32.ChecksumIEEE(payload))
	copy(out[20:], payload)
	if useMAC {
		tag := walMAC(out[:16], payload, macKey)
		copy(out[20+pageSize:], tag[:])
	}
	return out
}

// parseAndReplayWAL scans walData and applies records with seq > lastApplied to db.
// Returns the highest seq value seen in the file (for header update).
func parseAndReplayWAL(walData []byte, lastApplied uint64, apply func(seq, pageID uint64, payload []byte) error, pageSize int) (maxSeq uint64, err error) {
	return parseAndReplayWALWithMAC(walData, lastApplied, apply, [32]byte{}, false, pageSize)
}

func parseAndReplayWALWithMAC(
	walData []byte,
	lastApplied uint64,
	apply func(seq, pageID uint64, payload []byte) error,
	macKey [32]byte,
	useMAC bool,
	pageSize int,
) (maxSeq uint64, err error) {
	return scanWAL(bytes.NewReader(walData), lastApplied, apply, macKey, useMAC, pageSize)
}

// scanWAL reuses a record-sized buffer. apply must consume the payload before returning.
func scanWAL(
	reader io.Reader, lastApplied uint64, apply func(seq, pageID uint64, payload []byte) error,
	macKey [32]byte, useMAC bool, pageSize int,
) (uint64, error) {
	recSize := walRecordSize(pageSize)
	if useMAC {
		recSize += walRecordMACSize
	}
	chunk := make([]byte, recSize)
	var maxSeq uint64
	for {
		if _, err := io.ReadFull(reader, chunk); err != nil {
			switch {
			case errors.Is(err, io.EOF):
				return maxSeq, nil
			case errors.Is(err, io.ErrUnexpectedEOF):
				return 0, ErrCorruptWAL
			default:
				return 0, err
			}
		}
		seq := binary.BigEndian.Uint64(chunk[0:8])
		pageID := binary.BigEndian.Uint64(chunk[8:16])
		wantCRC := binary.BigEndian.Uint32(chunk[16:20])
		payload := chunk[20:]
		if useMAC {
			tag := payload[pageSize:]
			payload = payload[:pageSize]
			wantTag := walMAC(chunk[:16], payload, macKey)
			if !hmac.Equal(tag, wantTag[:]) {
				return 0, ErrCorruptWAL
			}
		}
		if crc32.ChecksumIEEE(payload) != wantCRC {
			return 0, ErrCorruptWAL
		}
		if seq > maxSeq {
			maxSeq = seq
		}
		if seq <= lastApplied {
			continue
		}
		if err := apply(seq, pageID, payload); err != nil {
			return 0, err
		}
	}
}

func walMAC(prefix, payload []byte, key [32]byte) [walRecordMACSize]byte {
	m := hmac.New(sha256.New, key[:])
	_, _ = m.Write(prefix)
	_, _ = m.Write(payload)
	sum := m.Sum(nil)
	var out [walRecordMACSize]byte
	copy(out[:], sum)
	return out
}
